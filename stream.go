// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"math"
	"math/big"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/peltonapp/rustyon-go/internal/fields"
)

// The streaming decoder reads straight from the source text into Go values.
// It shares all scanning with the tree parser (parser.classify) and follows
// the decoding rules of the tree decoder (decoder.decode) for every kind of
// value, so that both accept the same inputs and produce the same results.
//
// Targets that need a [Value] (empty interfaces, Value, enum registry types,
// Unmarshaler and TextUnmarshaler implementations) fall back to parsing and
// decoding the tree for that subtree only.
//
// The streaming decoder reports failures with a bare sentinel error. The
// exported entry points repeat a failed decode with the tree decoder, which
// produces the documented error types with positions and paths. Errors are
// therefore identical to the ones of the tree decoder by construction.

// errStream marks any failure of the streaming decoder.
var errStream = errors.New("rustyon: stream decode failed")

// typePlan caches what the streaming decoder needs to know about a Go type.
type typePlan struct {
	t     reflect.Type
	kind  reflect.Kind
	tree  bool // always decode through the tree
	bytes bool // slice whose element is a plain uint8 type

	anyIface bool // an interface without methods
	temps    sync.Pool
	elem     atomic.Pointer[typePlan]
	key      atomic.Pointer[typePlan]
	st       atomic.Pointer[structPlan]
}

type structPlan struct {
	info  *fields.Info
	plans []atomic.Pointer[typePlan]
}

var planCache sync.Map // reflect.Type to *typePlan

func planOf(t reflect.Type) *typePlan {
	if pl, ok := planCache.Load(t); ok {
		return pl.(*typePlan)
	}
	fl := flagsOf(t)
	k := t.Kind()
	pl := &typePlan{t: t, kind: k}
	pl.anyIface = k == reflect.Interface && t.NumMethod() == 0
	pl.tree = t == valueType || fl.value || (k == reflect.Interface && !pl.anyIface) || t == bigIntType
	if k != reflect.Pointer && (fl.unmarshaler || fl.textUnmarshaler) {
		pl.tree = true
	}
	switch k {
	case reflect.Chan, reflect.Func, reflect.Complex64, reflect.Complex128, reflect.UnsafePointer:
		pl.tree = true
	case reflect.Slice:
		pl.bytes = t.Elem().Kind() == reflect.Uint8 && !hasCustomUnmarshaler(t.Elem())
	}
	actual, _ := planCache.LoadOrStore(t, pl)
	return actual.(*typePlan)
}

func (pl *typePlan) elemPlan() *typePlan {
	if e := pl.elem.Load(); e != nil {
		return e
	}
	e := planOf(pl.t.Elem())
	pl.elem.Store(e)
	return e
}

func (pl *typePlan) keyPlan() *typePlan {
	if e := pl.key.Load(); e != nil {
		return e
	}
	e := planOf(pl.t.Key())
	pl.key.Store(e)
	return e
}

func (pl *typePlan) structPlan() *structPlan {
	if sp := pl.st.Load(); sp != nil {
		return sp
	}
	info := fields.For(pl.t)
	sp := &structPlan{info: info, plans: make([]atomic.Pointer[typePlan], len(info.Fields))}
	actual := pl.st.Load()
	if actual == nil {
		pl.st.Store(sp)
		actual = sp
	}
	return actual
}

func (sp *structPlan) fieldPlan(i int) *typePlan {
	if pl := sp.plans[i].Load(); pl != nil {
		return pl
	}
	pl := planOf(sp.info.Fields[i].Type)
	sp.plans[i].Store(pl)
	return pl
}

func fieldValue(rv reflect.Value, f *fields.Field) reflect.Value {
	if len(f.Index) == 1 {
		return rv.Field(f.Index[0])
	}
	return fields.ByIndex(rv, f.Index, true)
}

type streamer struct {
	p         *parser
	d         *decoder
	regActive bool
}

func newStreamer(p *parser, opts *decodeOptions) *streamer {
	reg := opts.registry
	if reg == nil {
		reg = DefaultRegistry
	}
	state := reg.load()
	return &streamer{
		p:         p,
		d:         &decoder{opts: opts, reg: state},
		regActive: len(state.units) > 0 || len(state.variants) > 0,
	}
}

// regTree reports whether the registry gives t a meaning that only the tree
// decoder implements.
func (s *streamer) regTree(t reflect.Type) bool {
	if s.d.reg.unitEnum(t) != nil {
		return true
	}
	_, ok := s.d.reg.variantName(t, nil)
	return ok
}

// streamInto decodes the next value into the pointer target v. A nil error
// means success; any other error only says that the tree decoder has to
// produce the real result.
func (s *streamer) streamInto(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errStream
	}
	elem := rv.Elem()
	return s.value(elem, planOf(elem.Type()))
}

// unmarshal is the streaming implementation of Unmarshal.
func unmarshalStream(data []byte, v any, opts *decodeOptions) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errStream
	}
	p, err := newParser(data)
	if err != nil {
		return err
	}
	exts, err := p.parseAttributes()
	if err != nil {
		return err
	}
	o := *opts
	o.exts |= exts
	s := newStreamer(p, &o)
	if err := s.streamInto(v); err != nil {
		return err
	}
	if err := p.skipWS(); err != nil {
		return err
	}
	if p.cur < len(p.src) {
		return errStream
	}
	return nil
}

// decodeStream decodes the next value of a Decoder.
func (dec *Decoder) decodeStream(v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return errStream
	}
	return newStreamer(dec.p, &dec.opts).streamInto(v)
}

func (s *streamer) value(rv reflect.Value, pl *typePlan) error {
	p := s.p
	var h header
	if err := p.classify(&h); err != nil {
		return err
	}
	if pl == nil {
		return s.skip(&h)
	}
	return s.store(&h, rv, pl)
}

// guardedValue is value inside one unit of the recursion budget.
func (s *streamer) guardedValue(rv reflect.Value, pl *typePlan) error {
	p := s.p
	if p.limit == 0 {
		return p.errRecursion()
	}
	p.limit--
	err := s.value(rv, pl)
	p.limit++
	return err
}

// viaTree builds the tree for the rest of the value described by h and
// decodes it with the tree decoder.
func (s *streamer) viaTree(h *header, rv reflect.Value) error {
	v, err := s.p.build(h)
	if err != nil {
		return err
	}
	return s.d.decode(v, rv)
}

func (s *streamer) store(h *header, rv reflect.Value, pl *typePlan) error {
	if pl.tree || h.kind == hRange {
		return s.viaTree(h, rv)
	}
	if pl.anyIface {
		if s.regActive && s.d.reg.enum(pl.t) != nil {
			return s.viaTree(h, rv)
		}
		return s.storeAny(h, rv)
	}
	t := pl.t
	if pl.kind == reflect.Pointer {
		switch h.kind {
		case hNone:
			rv.SetZero()
			return nil
		case hSome:
			if rv.IsNil() {
				rv.Set(reflect.New(t.Elem()))
			}
			return s.someBody(rv.Elem(), pl.elemPlan())
		}
		if rv.IsNil() {
			rv.Set(reflect.New(t.Elem()))
		}
		return s.store(h, rv.Elem(), pl.elemPlan())
	}
	if s.regActive && s.regTree(t) {
		return s.viaTree(h, rv)
	}

	switch pl.kind {
	case reflect.Bool:
		if h.kind != hBool {
			return errStream
		}
		rv.SetBool(h.b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		switch h.kind {
		case hNum:
			m := h.num.mag
			switch {
			case h.num.isFloat, m.hi != 0:
				return errStream
			case !h.num.neg && m.lo <= 1<<63-1:
				n = int64(m.lo)
			case h.num.neg && m.lo <= 1<<63:
				n = -int64(m.lo)
			default:
				return errStream
			}
		case hChar:
			n = int64(h.r)
		default:
			return errStream
		}
		if rv.OverflowInt(n) {
			return errStream
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		var n uint64
		switch h.kind {
		case hNum:
			m := h.num.mag
			if h.num.isFloat || m.hi != 0 || (h.num.neg && m.lo != 0) {
				return errStream
			}
			n = m.lo
		case hChar:
			n = uint64(h.r)
		default:
			return errStream
		}
		if rv.OverflowUint(n) {
			return errStream
		}
		rv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if h.kind != hNum {
			return errStream
		}
		var f float64
		if h.num.isFloat {
			f = h.num.f
		} else {
			f = intValue(h.num.neg, h.num.mag).Float64()
		}
		if pl.kind == reflect.Float32 && !math.IsInf(f, 0) && rv.OverflowFloat(f) {
			return errStream
		}
		rv.SetFloat(f)
	case reflect.String:
		switch h.kind {
		case hString, hIdent:
			rv.SetString(strings.Clone(h.s))
		case hChar:
			rv.SetString(string(h.r))
		default:
			return errStream
		}
	case reflect.Slice:
		return s.storeSlice(h, rv, pl)
	case reflect.Array:
		return s.storeArray(h, rv, pl)
	case reflect.Map:
		return s.storeMap(h, rv, pl)
	case reflect.Struct:
		return s.storeStruct(h, rv, pl)
	default:
		return errStream
	}
	return nil
}

// storeAny decodes the value described by h into an empty interface the way
// decoder.native converts a tree.
func (s *streamer) storeAny(h *header, rv reflect.Value) error {
	x, err := s.native(h)
	if err != nil {
		return err
	}
	if x == nil {
		rv.SetZero()
	} else {
		rv.Set(reflect.ValueOf(x))
	}
	return nil
}

func (s *streamer) guardedNative() (any, error) {
	p := s.p
	if p.limit == 0 {
		return nil, p.errRecursion()
	}
	p.limit--
	var h header
	err := p.classify(&h)
	var x any
	if err == nil {
		x, err = s.native(&h)
	}
	p.limit++
	return x, err
}

// native mirrors decoder.native: it converts the value described by h into
// plain Go values.
func (s *streamer) native(h *header) (any, error) {
	switch h.kind {
	case hBool:
		return h.b, nil
	case hNone, hUnit:
		return nil, nil
	case hSome:
		p := s.p
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if p.consumeChar('(') {
			if err := p.skipWS(); err != nil {
				return nil, err
			}
			p.newtypeVariant = p.exts&UnwrapVariantNewtypes != 0
			x, err := s.guardedNative()
			if err != nil {
				return nil, err
			}
			p.newtypeVariant = false
			if _, err := p.comma(); err != nil {
				return nil, err
			}
			if !p.consumeChar(')') {
				return nil, errStream
			}
			return x, nil
		}
		if p.exts&ImplicitSome != 0 {
			return s.guardedNative()
		}
		return nil, errStream
	case hNum:
		if h.num.isFloat {
			return h.num.f, nil
		}
		i := intValue(h.num.neg, h.num.mag)
		if n, ok := i.Int64(); ok {
			return n, nil
		}
		if n, ok := i.Uint64(); ok {
			return n, nil
		}
		return i.b, nil
	case hRange:
		v, err := s.p.build(h)
		if err != nil {
			return nil, err
		}
		return s.d.native(v)
	case hIdent, hString:
		return strings.Clone(h.s), nil
	case hChar:
		return h.r, nil
	case hBytes:
		return h.bytes, nil
	case hList, hTuple:
		sq, err := s.open(h)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			x, err := s.guardedNative()
			if err != nil {
				return nil, err
			}
			out = append(out, x)
			if err := s.comma(&sq); err != nil {
				return nil, err
			}
		}
		return out, s.close(&sq)
	case hStruct:
		sq, err := s.open(h)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			name, err := s.fieldHead()
			if err != nil {
				return nil, err
			}
			x, err := s.guardedNative()
			if err != nil {
				return nil, err
			}
			out[strings.Clone(name)] = x
			if err := s.comma(&sq); err != nil {
				return nil, err
			}
		}
		return out, s.close(&sq)
	case hMap:
		sq, err := s.open(h)
		if err != nil {
			return nil, err
		}
		out := map[any]any{}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			k, err := s.guardedNative()
			if err != nil {
				return nil, err
			}
			if k != nil && !reflect.TypeOf(k).Comparable() {
				return nil, errStream
			}
			if b, isBig := k.(*big.Int); isBig {
				k = b.String()
			}
			if err := s.colon(); err != nil {
				return nil, err
			}
			x, err := s.guardedNative()
			if err != nil {
				return nil, err
			}
			out[k] = x
			if err := s.comma(&sq); err != nil {
				return nil, err
			}
		}
		return out, s.close(&sq)
	}
	return nil, errStream
}

// someBody decodes the payload of Some after the Some keyword into rv, or
// skips it when pl is nil.
func (s *streamer) someBody(rv reflect.Value, pl *typePlan) error {
	p := s.p
	if err := p.skipWS(); err != nil {
		return err
	}
	if p.consumeChar('(') {
		if err := p.skipWS(); err != nil {
			return err
		}
		p.newtypeVariant = p.exts&UnwrapVariantNewtypes != 0
		if err := s.guardedValue(rv, pl); err != nil {
			return err
		}
		p.newtypeVariant = false
		if _, err := p.comma(); err != nil {
			return err
		}
		if !p.consumeChar(')') {
			return errStream
		}
		return nil
	}
	if p.exts&ImplicitSome != 0 {
		return s.guardedValue(rv, pl)
	}
	return errStream
}

// seq tracks the position inside a comma separated collection.
type seq struct {
	term     byte
	old      bool
	hadComma bool
}

// open consumes the opening of the collection described by h and enters one
// level of nesting.
func (s *streamer) open(h *header) (seq, error) {
	p := s.p
	sq := seq{hadComma: true}
	switch h.kind {
	case hList:
		p.newtypeVariant = false
		p.cur++
		sq.term = ']'
	case hMap:
		p.newtypeVariant = false
		p.cur++
		sq.term = '}'
	default:
		if !h.old && !p.consumeChar('(') {
			return sq, errStream
		}
		sq.old = h.old
		p.newtypeVariant = false
		sq.term = ')'
	}
	return sq, p.enter()
}

func (s *streamer) next(sq *seq) (bool, error) { return s.p.hasElement(sq.hadComma, sq.term) }

func (s *streamer) comma(sq *seq) error {
	var err error
	sq.hadComma, err = s.p.comma()
	return err
}

func (s *streamer) close(sq *seq) error {
	p := s.p
	p.leave()
	if err := p.skipWS(); err != nil {
		return err
	}
	if sq.old || p.consumeChar(sq.term) {
		return nil
	}
	return errStream
}

// fieldHead parses "name:" of a struct field.
func (s *streamer) fieldHead() (string, error) {
	p := s.p
	if p.limit == 0 {
		return "", p.errRecursion()
	}
	name, err := p.identifier()
	if err != nil {
		return "", err
	}
	if err := p.skipWS(); err != nil {
		return "", err
	}
	if !p.consumeChar(':') {
		return "", errStream
	}
	return name, p.skipWS()
}

// colon parses the separator between a map key and its value.
func (s *streamer) colon() error {
	p := s.p
	if err := p.skipWS(); err != nil {
		return err
	}
	if !p.consumeChar(':') {
		return errStream
	}
	return p.skipWS()
}

// skip parses and discards the value described by h.
func (s *streamer) skip(h *header) error {
	switch h.kind {
	case hSome:
		return s.someBody(reflect.Value{}, nil)
	case hRange:
		_, err := s.p.build(h)
		return err
	case hList, hTuple:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if err := s.guardedValue(reflect.Value{}, nil); err != nil {
				return err
			}
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		return s.close(&sq)
	case hStruct:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if _, err := s.fieldHead(); err != nil {
				return err
			}
			if err := s.guardedValue(reflect.Value{}, nil); err != nil {
				return err
			}
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		return s.close(&sq)
	case hMap:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if err := s.guardedValue(reflect.Value{}, nil); err != nil {
				return err
			}
			if err := s.colon(); err != nil {
				return err
			}
			if err := s.guardedValue(reflect.Value{}, nil); err != nil {
				return err
			}
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		return s.close(&sq)
	}
	return nil
}

// tupleCount counts the elements of the tuple described by h, up to two,
// and leaves the position where it was.
func (s *streamer) tupleCount(h *header) (int, error) {
	p := s.p
	cur, nv, limit := p.cur, p.newtypeVariant, p.limit
	sq, err := s.open(h)
	if err != nil {
		return 0, err
	}
	n := 0
	for n < 2 {
		ok, err := s.next(&sq)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		if err := s.guardedValue(reflect.Value{}, nil); err != nil {
			return 0, err
		}
		if err := s.comma(&sq); err != nil {
			return 0, err
		}
		n++
	}
	p.cur, p.newtypeVariant, p.limit = cur, nv, limit
	return n, nil
}

func (s *streamer) storeSlice(h *header, rv reflect.Value, pl *typePlan) error {
	t := pl.t
	if pl.bytes && (!s.regActive || s.d.reg.unitEnum(t.Elem()) == nil) {
		switch h.kind {
		case hBytes:
			rv.SetBytes(h.bytes)
			return nil
		case hString:
			rv.SetBytes([]byte(h.s))
			return nil
		}
	}
	switch h.kind {
	case hUnit:
		rv.Set(reflect.MakeSlice(t, 0, 0))
		return nil
	case hList, hTuple:
	case hBytes:
		return s.viaTree(h, rv)
	default:
		return errStream
	}
	sq, err := s.open(h)
	if err != nil {
		return err
	}
	ep := pl.elemPlan()
	rv.SetZero()
	for {
		ok, err := s.next(&sq)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		n := rv.Len()
		if n == rv.Cap() {
			if n == 0 {
				// Small lists are common: avoid the first few doublings.
				rv.Grow(4)
			} else {
				rv.Grow(1)
			}
		}
		rv.SetLen(n + 1)
		if err := s.guardedValue(rv.Index(n), ep); err != nil {
			return err
		}
		if err := s.comma(&sq); err != nil {
			return err
		}
	}
	if err := s.close(&sq); err != nil {
		return err
	}
	if rv.IsNil() {
		rv.Set(reflect.MakeSlice(t, 0, 0))
	}
	return nil
}

func (s *streamer) storeArray(h *header, rv reflect.Value, pl *typePlan) error {
	size := pl.t.Len()
	switch h.kind {
	case hUnit:
		if size != 0 {
			return errStream
		}
		return nil
	case hList, hTuple:
	case hBytes:
		return s.viaTree(h, rv)
	default:
		return errStream
	}
	sq, err := s.open(h)
	if err != nil {
		return err
	}
	ep := pl.elemPlan()
	i := 0
	for {
		ok, err := s.next(&sq)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if i >= size {
			return errStream
		}
		if err := s.guardedValue(rv.Index(i), ep); err != nil {
			return err
		}
		i++
		if err := s.comma(&sq); err != nil {
			return err
		}
	}
	if err := s.close(&sq); err != nil {
		return err
	}
	if i != size {
		return errStream
	}
	return nil
}

func (s *streamer) storeMap(h *header, rv reflect.Value, pl *typePlan) error {
	t := pl.t
	switch h.kind {
	case hMap, hStruct, hUnit:
	default:
		return errStream
	}
	if rv.IsNil() {
		rv.Set(reflect.MakeMap(t))
	}
	if h.kind == hUnit {
		return nil
	}
	sq, err := s.open(h)
	if err != nil {
		return err
	}
	kp, ep := pl.keyPlan(), pl.elemPlan()
	plainKey := kp.kind == reflect.String && !kp.tree && !s.regActive
	maybeIncomparable := kp.kind == reflect.Interface || kp.kind == reflect.Struct || kp.kind == reflect.Array
	var tmp *mapTemps
	if x := pl.temps.Get(); x != nil {
		tmp = x.(*mapTemps)
	} else {
		tmp = &mapTemps{k: reflect.New(t.Key()).Elem(), ev: reflect.New(t.Elem()).Elem()}
	}
	k, ev := tmp.k, tmp.ev
	for {
		ok, err := s.next(&sq)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		k.SetZero()
		if h.kind == hMap {
			if err := s.guardedValue(k, kp); err != nil {
				return err
			}
			if err := s.colon(); err != nil {
				return err
			}
		} else {
			name, err := s.fieldHead()
			if err != nil {
				return err
			}
			if plainKey {
				k.SetString(strings.Clone(name))
			} else if err := s.d.decode(String(strings.Clone(name)), k); err != nil {
				return err
			}
		}
		if maybeIncomparable && !k.Comparable() {
			return errStream
		}
		ev.SetZero()
		if err := s.guardedValue(ev, ep); err != nil {
			return err
		}
		rv.SetMapIndex(k, ev)
		if err := s.comma(&sq); err != nil {
			return err
		}
	}
	if err := s.close(&sq); err != nil {
		return err
	}
	k.SetZero()
	ev.SetZero()
	pl.temps.Put(tmp)
	return nil
}

// mapTemps are the scratch key and element values used while decoding a map.
// SetMapIndex copies them, so they can be reused for every entry.
type mapTemps struct{ k, ev reflect.Value }

func (s *streamer) storeStruct(h *header, rv reflect.Value, pl *typePlan) error {
	if s.d.opts.exts&ExplicitStructNames != 0 {
		switch h.kind {
		case hUnit:
			return errStream
		case hStruct, hTuple:
			if h.s == "" {
				return errStream
			}
		}
	}
	sp := pl.structPlan()
	si := sp.info

	// unwrap_newtypes: a single field tuple struct written as its inner value.
	if si.IsNewtype() {
		single := false
		if h.kind == hTuple {
			n, err := s.tupleCount(h)
			if err != nil {
				return err
			}
			single = n == 1
		}
		if !single {
			return s.store(h, fieldValue(rv, &si.Fields[0]), sp.fieldPlan(0))
		}
	}

	switch h.kind {
	case hStruct:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		nextField := 0
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			name, err := s.fieldHead()
			if err != nil {
				return err
			}
			if err := s.structField(rv, sp, name, &nextField); err != nil {
				return err
			}
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		return s.close(&sq)
	case hTuple:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		i := 0
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			if i >= len(si.Fields) {
				return errStream
			}
			if err := s.guardedValue(fieldValue(rv, &si.Fields[i]), sp.fieldPlan(i)); err != nil {
				return err
			}
			i++
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		if err := s.close(&sq); err != nil {
			return err
		}
		if i != len(si.Fields) {
			return errStream
		}
		return nil
	case hMap:
		sq, err := s.open(h)
		if err != nil {
			return err
		}
		nextField := 0
		for {
			ok, err := s.next(&sq)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
			name, err := s.mapKeyName()
			if err != nil {
				return err
			}
			if err := s.structField(rv, sp, name, &nextField); err != nil {
				return err
			}
			if err := s.comma(&sq); err != nil {
				return err
			}
		}
		return s.close(&sq)
	case hUnit, hIdent:
		if len(si.Fields) == 0 {
			return nil
		}
	}
	return errStream
}

// mapKeyName parses a map key that must be a string or an identifier and
// the colon after it.
func (s *streamer) mapKeyName() (string, error) {
	p := s.p
	if p.limit == 0 {
		return "", p.errRecursion()
	}
	p.limit--
	var kh header
	err := p.classify(&kh)
	p.limit++
	if err != nil {
		return "", err
	}
	if kh.kind != hString && kh.kind != hIdent {
		return "", errStream
	}
	return kh.s, s.colon()
}

// structField decodes the value of the field called name, or skips it.
func (s *streamer) structField(rv reflect.Value, sp *structPlan, name string, nextField *int) error {
	si := sp.info
	idx := -1
	if n := *nextField; n < len(si.Fields) && si.Fields[n].Name == name {
		idx = n
	} else if i, ok := si.LookupIndex(name); ok {
		idx = i
	}
	if idx < 0 {
		if s.d.opts.disallowUnknown {
			return errStream
		}
		return s.guardedValue(reflect.Value{}, nil)
	}
	*nextField = idx + 1
	return s.guardedValue(fieldValue(rv, &si.Fields[idx]), sp.fieldPlan(idx))
}

// Unmarshal and Decoder entry points.

func unmarshalTree(data []byte, v any, opts *decodeOptions) error {
	doc, err := ParseDocument(data)
	if err != nil {
		return err
	}
	o := *opts
	o.exts |= doc.Extensions
	return decodeInto(doc.Value, v, &o)
}

// decodeNext decodes the next value of the Decoder, trying the streaming
// decoder first. When that fails it repeats the work with the tree decoder
// to produce the precise error.
func (dec *Decoder) decodeNext(v any) error {
	p := dec.p
	cur, nv, limit := p.cur, p.newtypeVariant, p.limit
	if err := dec.decodeStream(v); err == nil {
		return nil
	}
	p.cur, p.newtypeVariant, p.limit = cur, nv, limit
	val, err := p.parseValue()
	if err != nil {
		dec.err = p.wrap(err)
		return dec.err
	}
	return decodeInto(val, v, &dec.opts)
}

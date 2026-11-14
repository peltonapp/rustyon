// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"encoding"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/peltonapp/rustyon-go/internal/fields"
	"github.com/peltonapp/rustyon-go/internal/syntax"
)

// Unmarshaler is implemented by types that can unmarshal a RON value
// themselves. UnmarshalRON receives the value re-encoded as compact RON.
type Unmarshaler interface {
	UnmarshalRON([]byte) error
}

var (
	unmarshalerType     = reflect.TypeFor[Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// Unmarshal parses the RON document in data and stores the result in the
// value pointed to by v. It is the inverse of [Marshal] and accepts the
// following in addition to the mapping described there:
//
//   - Struct fields match by name exactly, then case-insensitively. Unknown
//     fields are ignored unless [Decoder.DisallowUnknownFields] is used.
//     Struct names in the input are not checked.
//   - Structs also decode from tuples (by position) and from maps with
//     string keys.
//   - Pointers accept None, Some(v) and a plain v (implicit_some).
//   - Tuple structs with a single field accept the plain inner value
//     (unwrap_newtypes).
//   - Strings accept chars and identifiers, floats accept integers.
//
// Decoding into an empty interface produces nil for () and None, bool,
// int64 (or uint64, or *big.Int when needed), float64, string, rune for
// chars, []byte, []any for lists and tuples, map[any]any for maps,
// map[string]any for structs and string for identifiers. Decode into a
// [Value] to keep the full structure including names.
func Unmarshal(data []byte, v any) error {
	doc, err := ParseDocument(data)
	if err != nil {
		return err
	}
	return decodeInto(doc.Value, v, &decodeOptions{})
}

// FromValue stores a parsed [Value] in the Go value pointed to by target,
// following the rules of [Unmarshal].
func FromValue(v Value, target any) error {
	return decodeInto(v, target, &decodeOptions{})
}

type decodeOptions struct {
	disallowUnknown bool
	registry        *Registry
}

func decodeInto(val Value, v any, opts *decodeOptions) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return &InvalidUnmarshalError{Type: reflect.TypeOf(v)}
	}
	reg := opts.registry
	if reg == nil {
		reg = DefaultRegistry
	}
	d := &decoder{opts: opts, reg: reg.load()}
	return d.decode(val, rv.Elem())
}

// A Decoder reads and decodes RON values from an input stream. The stream
// can hold several values separated by whitespace or comments; each call to
// [Decoder.Decode] reads the next one. Extension attributes are accepted
// before the first value.
type Decoder struct {
	r       io.Reader
	p       *parser
	err     error
	started bool
	opts    decodeOptions
}

// NewDecoder returns a new decoder that reads from r. The decoder reads all
// of r on the first call to Decode.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// DisallowUnknownFields makes Decode return an [UnknownFieldError] when the
// input contains a struct field that has no matching Go field.
func (dec *Decoder) DisallowUnknownFields() { dec.opts.disallowUnknown = true }

// SetRegistry sets the enum registry. A nil registry selects
// [DefaultRegistry].
func (dec *Decoder) SetRegistry(r *Registry) { dec.opts.registry = r }

// More reports whether there is another value to decode.
func (dec *Decoder) More() bool {
	if err := dec.init(); err != nil {
		return true // let Decode report the error
	}
	return dec.p.tok.Kind != syntax.EOF
}

func (dec *Decoder) init() error {
	if dec.started {
		return dec.err
	}
	dec.started = true
	data, err := io.ReadAll(dec.r)
	if err != nil {
		dec.err = err
		return err
	}
	dec.p, dec.err = newParser(data)
	if dec.err != nil {
		return dec.err
	}
	for dec.p.tok.Kind == syntax.Hash {
		if _, err := dec.p.parseAttribute(); err != nil {
			dec.err = err
			return err
		}
	}
	return nil
}

// Decode reads the next RON value from the input and stores it in v. It
// returns [io.EOF] when there are no more values.
func (dec *Decoder) Decode(v any) error {
	if err := dec.init(); err != nil {
		return err
	}
	if dec.p.tok.Kind == syntax.EOF {
		return io.EOF
	}
	val, err := dec.p.parseValue()
	if err != nil {
		dec.err = err
		return err
	}
	return decodeInto(val, v, &dec.opts)
}

type decoder struct {
	opts *decodeOptions
	reg  *registryState
	path []pathElem
}

// pathElem is one step of the path to the value being decoded. It is
// rendered only when an error is reported.
type pathElem struct {
	kind  uint8 // pathField, pathIndex, pathKey or pathVariant
	name  string
	index int
	key   Value
}

const (
	pathField = iota
	pathIndex
	pathKey
	pathVariant
)

func (d *decoder) pathString() string {
	var sb strings.Builder
	for _, p := range d.path {
		switch p.kind {
		case pathField:
			if sb.Len() > 0 {
				sb.WriteByte('.')
			}
			sb.WriteString(p.name)
		case pathIndex:
			sb.WriteString("[" + strconv.Itoa(p.index) + "]")
		case pathKey:
			if p.key == nil {
				sb.WriteString("{key}")
			} else {
				sb.WriteString("[" + keyString(p.key) + "]")
			}
		case pathVariant:
			sb.WriteString("(" + p.name + ")")
		}
	}
	return sb.String()
}

func (d *decoder) push(e pathElem)       { d.path = append(d.path, e) }
func (d *decoder) pushField(name string) { d.push(pathElem{kind: pathField, name: name}) }
func (d *decoder) pushIndex(i int)       { d.push(pathElem{kind: pathIndex, index: i}) }
func (d *decoder) pushKey(k Value)       { d.push(pathElem{kind: pathKey, key: k}) }
func (d *decoder) pushVariant(n string)  { d.push(pathElem{kind: pathVariant, name: n}) }
func (d *decoder) pop()                  { d.path = d.path[:len(d.path)-1] }

func describeValue(v Value) string {
	switch v := v.(type) {
	case Ident:
		return "identifier " + string(v)
	case Tuple:
		if v.Name != "" {
			return "tuple " + v.Name
		}
	case Struct:
		if v.Name != "" {
			return "struct " + v.Name
		}
	case Option:
		if v.IsNone() {
			return "None"
		}
		return "Some"
	}
	return v.Kind().String()
}

func (d *decoder) typeError(v Value, t reflect.Type, msg string) error {
	return &UnmarshalTypeError{Value: describeValue(v), Type: t, Path: d.pathString(), Msg: msg}
}

func (d *decoder) decode(v Value, rv reflect.Value) error {
	t := rv.Type()

	// Value targets receive the tree itself.
	if t == valueType {
		rv.Set(reflect.ValueOf(v))
		return nil
	}
	// Fast paths for common scalar targets that cannot have custom methods.
	if t.PkgPath() == "" {
		switch t.Kind() {
		case reflect.String:
			if s, ok := v.(String); ok {
				rv.SetString(string(s))
				return nil
			}
		case reflect.Int, reflect.Int64:
			if i, ok := v.(Int); ok && i.b == nil {
				rv.SetInt(i.small)
				return nil
			}
		case reflect.Float64:
			if f, ok := v.(Float); ok {
				rv.SetFloat(float64(f))
				return nil
			}
		case reflect.Bool:
			if b, ok := v.(Bool); ok {
				rv.SetBool(bool(b))
				return nil
			}
		}
	}

	f := flagsOf(t)
	if f.value && reflect.TypeOf(v) == t {
		rv.Set(reflect.ValueOf(v))
		return nil
	}

	if t.Kind() == reflect.Pointer {
		if o, ok := v.(Option); ok {
			if o.IsNone() {
				rv.SetZero()
				return nil
			}
			v = o.Value
		}
		if rv.IsNil() {
			rv.Set(reflect.New(t.Elem()))
		}
		return d.decode(v, rv.Elem())
	}

	if rv.CanAddr() && (f.unmarshaler || f.textUnmarshaler) {
		pv := rv.Addr()
		if f.unmarshaler {
			b, err := printDocument(v, &encodeOptions{})
			if err != nil {
				return err
			}
			return pv.Interface().(Unmarshaler).UnmarshalRON(b)
		}
		if t != bigIntType {
			if d.reg.unitEnum(t) == nil {
				var text string
				switch v := v.(type) {
				case String:
					text = string(v)
				case Ident:
					text = string(v)
				default:
					return d.typeError(v, t, "expected a string")
				}
				return pv.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(text))
			}
		}
	}

	if t.Kind() == reflect.Interface {
		return d.decodeInterface(v, rv)
	}
	if info := d.reg.unitEnum(t); info != nil {
		var name string
		switch v := v.(type) {
		case Ident:
			name = string(v)
		case String:
			name = string(v)
		default:
			return d.typeError(v, t, "expected a unit variant")
		}
		val, ok := info.byName[name]
		if !ok {
			return d.typeError(v, t, "unknown variant "+strconv.Quote(name))
		}
		rv.Set(val)
		return nil
	}
	if name, ok := d.reg.variantName(t, nil); ok {
		return d.decodeVariantBody(v, name, rv)
	}
	if t == bigIntType {
		i, ok := v.(Int)
		if !ok {
			return d.typeError(v, t, "")
		}
		rv.Set(reflect.ValueOf(i.Big()).Elem())
		return nil
	}

	switch t.Kind() {
	case reflect.Bool:
		b, ok := v.(Bool)
		if !ok {
			return d.typeError(v, t, "")
		}
		rv.SetBool(bool(b))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		switch v := v.(type) {
		case Int:
			x, ok := v.Int64()
			if !ok {
				return d.typeError(v, t, "integer "+v.String()+" overflows")
			}
			n = x
		case Char:
			n = int64(v)
		default:
			return d.typeError(v, t, "")
		}
		if rv.OverflowInt(n) {
			return d.typeError(v, t, "integer "+strconv.FormatInt(n, 10)+" overflows")
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		i, ok := v.(Int)
		if !ok {
			return d.typeError(v, t, "")
		}
		n, ok := i.Uint64()
		if !ok || rv.OverflowUint(n) {
			return d.typeError(v, t, "integer "+i.String()+" overflows")
		}
		rv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		var f float64
		switch v := v.(type) {
		case Float:
			f = float64(v)
		case Int:
			f = v.Float64()
		default:
			return d.typeError(v, t, "")
		}
		if t.Kind() == reflect.Float32 && !math.IsInf(f, 0) && rv.OverflowFloat(f) {
			return d.typeError(v, t, "float overflows")
		}
		rv.SetFloat(f)
	case reflect.String:
		switch v := v.(type) {
		case String:
			rv.SetString(string(v))
		case Char:
			rv.SetString(string(rune(v)))
		case Ident:
			rv.SetString(string(v))
		default:
			return d.typeError(v, t, "")
		}
	case reflect.Slice:
		return d.decodeSlice(v, rv)
	case reflect.Array:
		return d.decodeArray(v, rv)
	case reflect.Map:
		return d.decodeMap(v, rv)
	case reflect.Struct:
		return d.decodeStruct(v, rv)
	default:
		return d.typeError(v, t, "unsupported Go type")
	}
	return nil
}

func (d *decoder) decodeInterface(v Value, rv reflect.Value) error {
	t := rv.Type()
	if o, ok := v.(Option); ok && o.IsNone() {
		rv.SetZero()
		return nil
	}
	if info := d.reg.enum(t); info != nil {
		var name string
		switch v := v.(type) {
		case Ident:
			name = string(v)
		case Tuple:
			name = v.Name
		case Struct:
			name = v.Name
		case String:
			name = string(v)
		case Option:
			return d.decodeInterface(v.Value, rv)
		}
		if name == "" {
			return d.typeError(v, t, "expected an enum variant")
		}
		vt, ok := info.byName[name]
		if !ok {
			return d.typeError(v, t, "unknown variant "+strconv.Quote(name))
		}
		target := reflect.New(vt).Elem()
		if err := d.decodeVariantBody(v, name, target); err != nil {
			return err
		}
		rv.Set(target)
		return nil
	}
	if t.NumMethod() != 0 {
		return d.typeError(v, t, "interface type is not a registered enum")
	}
	native, err := d.native(v)
	if err != nil {
		return err
	}
	if native == nil {
		rv.SetZero()
	} else {
		rv.Set(reflect.ValueOf(native))
	}
	return nil
}

// decodeVariantBody decodes the payload of an enum variant into rv, which
// holds the variant's Go type.
func (d *decoder) decodeVariantBody(v Value, name string, rv reflect.Value) error {
	t := rv.Type()
	if t.Kind() == reflect.Pointer {
		rv.Set(reflect.New(t.Elem()))
		return d.decodeVariantBody(v, name, rv.Elem())
	}
	d.pushVariant(name)
	defer d.pop()
	if t.Kind() == reflect.Struct && !hasCustomUnmarshaler(t) {
		si := fields.For(t)
		switch v := v.(type) {
		case Ident, String:
			return nil // unit variant, all fields zero
		case Tuple:
			if len(si.Fields) == 0 && len(v.Values) == 0 {
				return nil
			}
			// A newtype variant around a struct: Name((a: 1)).
			if !si.Tuple && len(v.Values) == 1 {
				if _, ok := v.Values[0].(Struct); ok {
					return d.decodeStruct(v.Values[0], rv)
				}
			}
			return d.decodeStruct(Tuple{Values: v.Values}, rv)
		case Struct:
			return d.decodeStruct(Struct{Fields: v.Fields}, rv)
		}
		return d.typeError(v, t, "")
	}
	switch v := v.(type) {
	case Tuple:
		if len(v.Values) == 1 {
			return d.decodePlain(v.Values[0], rv)
		}
		return d.decodePlain(Tuple{Values: v.Values}, rv)
	case Struct:
		return d.decodePlain(Struct{Fields: v.Fields}, rv)
	case Ident:
		return nil
	}
	return d.typeError(v, t, "expected an enum variant")
}

// decodePlain decodes into rv ignoring rv's own variant registration.
func (d *decoder) decodePlain(v Value, rv reflect.Value) error {
	tmp := reflect.New(rv.Type()).Elem()
	saved := d.reg
	d.reg = saved.without(rv.Type())
	err := d.decode(v, tmp)
	d.reg = saved
	if err != nil {
		return err
	}
	rv.Set(tmp)
	return nil
}

func hasCustomUnmarshaler(t reflect.Type) bool {
	f := flagsOf(t)
	return f.unmarshaler || f.textUnmarshaler
}

func (d *decoder) elements(v Value) ([]Value, bool) {
	switch v := v.(type) {
	case List:
		return v, true
	case Tuple:
		return v.Values, true
	case Unit:
		return nil, true
	case Bytes:
		vals := make([]Value, len(v))
		for i, b := range v {
			vals[i] = NewUint(uint64(b))
		}
		return vals, true
	case String:
		// Allow lists of chars to come from strings.
		return nil, false
	}
	return nil, false
}

func (d *decoder) decodeSlice(v Value, rv reflect.Value) error {
	t := rv.Type()
	if t.Elem().Kind() == reflect.Uint8 && !hasCustomUnmarshaler(t.Elem()) && d.reg.unitEnum(t.Elem()) == nil {
		switch b := v.(type) {
		case Bytes:
			rv.SetBytes(bytes.Clone(b))
			return nil
		case String:
			rv.SetBytes([]byte(b))
			return nil
		}
	}
	elems, ok := d.elements(v)
	if !ok {
		return d.typeError(v, t, "")
	}
	s := reflect.MakeSlice(t, len(elems), len(elems))
	for i, e := range elems {
		d.pushIndex(i)
		err := d.decode(e, s.Index(i))
		d.pop()
		if err != nil {
			return err
		}
	}
	rv.Set(s)
	return nil
}

func (d *decoder) decodeArray(v Value, rv reflect.Value) error {
	t := rv.Type()
	elems, ok := d.elements(v)
	if !ok {
		return d.typeError(v, t, "")
	}
	if len(elems) != t.Len() {
		return d.typeError(v, t, fmt.Sprintf("expected %d elements, found %d", t.Len(), len(elems)))
	}
	for i, e := range elems {
		d.pushIndex(i)
		err := d.decode(e, rv.Index(i))
		d.pop()
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *decoder) decodeMap(v Value, rv reflect.Value) error {
	t := rv.Type()
	var entries []MapEntry
	switch v := v.(type) {
	case Map:
		entries = v
	case Struct:
		for _, f := range v.Fields {
			entries = append(entries, MapEntry{Key: String(f.Name), Value: f.Value})
		}
	case Unit:
	default:
		return d.typeError(v, t, "")
	}
	if rv.IsNil() {
		rv.Set(reflect.MakeMapWithSize(t, len(entries)))
	}
	for _, e := range entries {
		k := reflect.New(t.Key()).Elem()
		d.pushKey(nil)
		err := d.decode(e.Key, k)
		d.pop()
		if err != nil {
			return err
		}
		ev := reflect.New(t.Elem()).Elem()
		d.pushKey(e.Key)
		err = d.decode(e.Value, ev)
		d.pop()
		if err != nil {
			return err
		}
		rv.SetMapIndex(k, ev)
	}
	return nil
}

func keyString(k Value) string {
	p := &printer{}
	p.value(k)
	s := p.sb.String()
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}

func (d *decoder) decodeStruct(v Value, rv reflect.Value) error {
	t := rv.Type()
	si := fields.For(t)

	// unwrap_newtypes: a single field tuple struct written as its inner value.
	if si.IsNewtype() {
		if tup, ok := v.(Tuple); !ok || len(tup.Values) != 1 {
			return d.decodeField(v, rv, &si.Fields[0])
		}
	}

	switch v := v.(type) {
	case Struct:
		for _, f := range v.Fields {
			fi, ok := si.Lookup(f.Name)
			if !ok {
				if d.opts.disallowUnknown {
					return &UnknownFieldError{Field: f.Name, Type: t, Path: d.pathString()}
				}
				continue
			}
			if err := d.decodeField(f.Value, rv, fi); err != nil {
				return err
			}
		}
		return nil
	case Tuple:
		if len(v.Values) != len(si.Fields) {
			return d.typeError(v, t, fmt.Sprintf("expected %d elements, found %d", len(si.Fields), len(v.Values)))
		}
		for i, e := range v.Values {
			if err := d.decodeField(e, rv, &si.Fields[i]); err != nil {
				return err
			}
		}
		return nil
	case Map:
		for _, e := range v {
			var name string
			switch k := e.Key.(type) {
			case String:
				name = string(k)
			case Ident:
				name = string(k)
			default:
				return d.typeError(v, t, "map keys must be strings to decode into a struct")
			}
			fi, ok := si.Lookup(name)
			if !ok {
				if d.opts.disallowUnknown {
					return &UnknownFieldError{Field: name, Type: t, Path: d.pathString()}
				}
				continue
			}
			if err := d.decodeField(e.Value, rv, fi); err != nil {
				return err
			}
		}
		return nil
	case Unit, Ident:
		if len(si.Fields) == 0 {
			return nil
		}
	}
	return d.typeError(v, t, "")
}

func (d *decoder) decodeField(v Value, rv reflect.Value, f *fields.Field) error {
	d.pushField(f.Name)
	defer d.pop()
	fv := fields.ByIndex(rv, f.Index, true)
	return d.decode(v, fv)
}

// native converts a Value into plain Go values for decoding into any.
func (d *decoder) native(v Value) (any, error) {
	switch v := v.(type) {
	case Unit:
		return nil, nil
	case Bool:
		return bool(v), nil
	case Int:
		if n, ok := v.Int64(); ok {
			return n, nil
		}
		if n, ok := v.Uint64(); ok {
			return n, nil
		}
		return v.Big(), nil
	case Float:
		return float64(v), nil
	case String:
		return string(v), nil
	case Char:
		return rune(v), nil
	case Bytes:
		return bytes.Clone(v), nil
	case Ident:
		return string(v), nil
	case Option:
		if v.IsNone() {
			return nil, nil
		}
		return d.native(v.Value)
	case List:
		out := make([]any, len(v))
		for i, e := range v {
			d.pushIndex(i)
			x, err := d.native(e)
			d.pop()
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	case Tuple:
		return d.native(List(v.Values))
	case Struct:
		out := make(map[string]any, len(v.Fields))
		for _, f := range v.Fields {
			d.pushField(f.Name)
			x, err := d.native(f.Value)
			d.pop()
			if err != nil {
				return nil, err
			}
			out[f.Name] = x
		}
		return out, nil
	case Map:
		out := make(map[any]any, len(v))
		for _, e := range v {
			k, err := d.native(e.Key)
			if err != nil {
				return nil, err
			}
			if k != nil && !reflect.TypeOf(k).Comparable() {
				return nil, d.typeError(e.Key, reflect.TypeFor[map[any]any](), "map key is not comparable in Go")
			}
			if _, isBig := k.(*big.Int); isBig {
				k = k.(*big.Int).String()
			}
			x, err := d.native(e.Value)
			if err != nil {
				return nil, err
			}
			out[k] = x
		}
		return out, nil
	}
	return nil, fmt.Errorf("rustyon: unknown value type %T", v)
}

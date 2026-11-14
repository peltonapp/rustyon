// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"cmp"
	"encoding"
	"io"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"

	"github.com/peltonapp/rustyon-go/internal/fields"
)

// Marshaler is implemented by types that can marshal themselves into RON.
// MarshalRON must return a single valid RON value.
type Marshaler interface {
	MarshalRON() ([]byte, error)
}

var (
	marshalerType     = reflect.TypeFor[Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	valueType         = reflect.TypeFor[Value]()
	stringType        = reflect.TypeFor[string]()
	bigIntType        = reflect.TypeFor[big.Int]()
)

type encodeOptions struct {
	prefix         string
	indent         string
	structNames    bool
	implicitSome   bool
	unwrapNewtypes bool
	naming         fields.Naming
	registry       *Registry
	pretty         *PrettyConfig // takes precedence over prefix and indent
}

func (o *encodeOptions) extensions() Extensions {
	var e Extensions
	if o.implicitSome {
		e |= ImplicitSome
	}
	if o.unwrapNewtypes {
		e |= UnwrapNewtypes
	}
	return e
}

// Marshal returns the compact RON encoding of v.
//
// Go values map to RON as follows:
//
//   - bool, integers, floats and strings map to the matching literals.
//     Integers of any size are supported through [big.Int].
//   - []byte maps to a byte string b"...".
//   - Slices and arrays map to lists [a, b].
//   - Maps map to {k: v}, with entries sorted by key.
//   - Structs map to (field: value). Field names default to the snake_case
//     form of the Go name (see [NamingStrategy] for other styles) and can be
//     changed with a `ron:"name"` tag. The
//     options omitempty and omitzero skip empty or zero fields, and "-"
//     skips a field. A blank field of type struct{} named _ with the tag
//     `ron:"Name,tuple"` sets the struct's RON name and, with the tuple
//     option, encodes the struct as a tuple struct (a, b). The option
//     rename_all=camelCase on that field renames the struct's fields, see
//     [NamingStrategy]. Embedded structs, and fields of struct type with the
//     option flatten, have their fields written inline in the parent, as
//     serde's flatten does. A field of type map[string]T with the option
//     flatten collects all fields that no other field matches when decoding,
//     and is written as fields after the regular ones.
//   - Pointers map to Option: nil is None and anything else is Some(v).
//   - Nil interfaces map to None.
//   - Types registered as enum variants (see [Registry]) map to Name,
//     Name(a, b) or Name(field: v).
//   - [Value] trees are written as they are.
//   - Types implementing [Marshaler] or [encoding.TextMarshaler] are
//     marshaled with those methods; text becomes a RON string.
//
// Channels, functions and complex numbers cannot be encoded.
func Marshal(v any) ([]byte, error) {
	return marshal(v, &encodeOptions{})
}

// MarshalIndent is like [Marshal] but writes one element per line. Each line
// starts with prefix followed by one copy of indent per nesting level.
func MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	if indent == "" {
		indent = "    "
	}
	return marshal(v, &encodeOptions{prefix: prefix, indent: indent})
}

func marshal(v any, opts *encodeOptions) ([]byte, error) {
	if opts.pretty != nil {
		if err := opts.pretty.validate(); err != nil {
			return nil, err
		}
	}
	val, err := toValue(v, opts)
	if err != nil {
		return nil, err
	}
	return printDocument(val, opts)
}

func printDocument(val Value, opts *encodeOptions) ([]byte, error) {
	p := &printer{prefix: opts.prefix, indent: opts.indent}
	if opts.pretty != nil {
		p.configure(opts.pretty)
	}
	p.writeHeader(opts.extensions())
	p.value(val)
	if p.err != nil {
		return nil, p.err
	}
	return p.sb.Bytes(), nil
}

// ToValue converts a Go value into a [Value] tree using the same rules as
// [Marshal].
func ToValue(v any) (Value, error) {
	return toValue(v, &encodeOptions{})
}

func toValue(v any, opts *encodeOptions) (Value, error) {
	reg := opts.registry
	if reg == nil {
		reg = DefaultRegistry
	}
	e := &encoder{opts: opts, reg: reg.load()}
	return e.encode(reflect.ValueOf(v))
}

// An Encoder writes RON values to an output stream.
type Encoder struct {
	w    io.Writer
	opts encodeOptions
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// SetIndent makes the encoder write pretty printed output with one element
// per line, as [MarshalIndent] does. An empty indent restores compact output.
func (enc *Encoder) SetIndent(prefix, indent string) {
	enc.opts.prefix = prefix
	enc.opts.indent = indent
	enc.opts.pretty = nil
}

// SetStructNames makes the encoder write struct names, for example
// Config(port: 80) instead of (port: 80).
func (enc *Encoder) SetStructNames(on bool) { enc.opts.structNames = on }

// SetImplicitSome makes the encoder write non-nil pointers as their plain
// value instead of Some(value) and enables the implicit_some extension in
// the output.
func (enc *Encoder) SetImplicitSome(on bool) { enc.opts.implicitSome = on }

// SetUnwrapNewtypes makes the encoder write tuple structs with a single
// field as their inner value and enables the unwrap_newtypes extension in
// the output.
func (enc *Encoder) SetUnwrapNewtypes(on bool) { enc.opts.unwrapNewtypes = on }

// SetFieldNaming sets how Go field names without a tag name become RON field
// names. The default is [SnakeCase]. A rename_all option on a struct
// overrides it for that struct.
func (enc *Encoder) SetFieldNaming(n NamingStrategy) { enc.opts.naming = fields.Naming(n) }

// SetRegistry sets the enum registry. A nil registry selects
// [DefaultRegistry].
func (enc *Encoder) SetRegistry(r *Registry) { enc.opts.registry = r }

// Encode writes the RON encoding of v followed by a newline.
func (enc *Encoder) Encode(v any) error {
	b, err := marshal(v, &enc.opts)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = enc.w.Write(b)
	return err
}

type encoder struct {
	opts  *encodeOptions
	reg   *registryState
	depth int
}

func (e *encoder) encode(rv reflect.Value) (Value, error) {
	if !rv.IsValid() {
		return None, nil
	}
	if rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return None, nil
		}
		return e.encodeConcrete(rv.Elem(), rv.Type())
	}
	return e.encodeConcrete(rv, nil)
}

// encodeConcrete encodes a non-interface value. static is the interface
// type the value was stored in, if any.
func (e *encoder) encodeConcrete(rv reflect.Value, static reflect.Type) (Value, error) {
	e.depth++
	defer func() { e.depth-- }()
	if e.depth > maxDepth {
		return nil, &UnsupportedValueError{Value: rv, Msg: "exceeded maximum nesting depth, possibly a cycle"}
	}
	t := rv.Type()
	if name, info, ok := e.reg.variantOf(t, static); ok {
		return e.encodeVariant(rv, name, info)
	}
	return e.encodePlain(rv)
}

// variantKind is the shape of an enum variant.
type variantKind uint8

const (
	variantUnit variantKind = iota
	variantStruct
	variantTuple
	variantNewtype
)

func (e *encoder) encodeVariant(rv reflect.Value, name string, info *enumInfo) (Value, error) {
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return None, nil
		}
		rv = rv.Elem()
	}
	var (
		kind  variantKind
		flds  []Field
		vals  []Value
		inner Value
		err   error
	)
	if rv.Kind() == reflect.Struct && !hasCustomMarshaler(rv.Type()) {
		si := fields.ForNaming(rv.Type(), e.opts.naming)
		switch {
		case len(si.Fields) == 0 && si.Extra == nil:
			kind = variantUnit
		case si.Tuple:
			kind = variantTuple
			vals, err = e.tupleValues(rv, si)
		default:
			kind = variantStruct
			flds, err = e.structFields(rv, si)
		}
	} else {
		kind = variantNewtype
		inner, err = e.encodePlain(rv)
	}
	if err != nil {
		return nil, err
	}

	rep := info.rep
	switch rep.rep() {
	case repInternal:
		return e.internalVariant(rv, name, rep, kind, flds, vals, inner)
	case repAdjacent:
		// serde writes the tag of an adjacently tagged enum as a variant
		// identifier, in contrast to internally tagged enums.
		tag := Field{Name: rep.Tag, Value: Ident(name)}
		switch kind {
		case variantUnit:
			return Struct{Fields: []Field{tag}}, nil
		case variantStruct:
			inner = Struct{Fields: flds}
		case variantTuple:
			inner = Tuple{Values: vals}
		}
		return Struct{Fields: []Field{tag, {Name: rep.Content, Value: inner}}}, nil
	case repUntagged:
		switch kind {
		case variantUnit:
			return Unit{}, nil
		case variantStruct:
			return Struct{Fields: flds}, nil
		case variantTuple:
			return Tuple{Values: vals}, nil
		}
		return inner, nil
	}
	switch kind {
	case variantUnit:
		return Ident(name), nil
	case variantStruct:
		return Struct{Name: name, Fields: flds}, nil
	case variantTuple:
		return Tuple{Name: name, Values: vals}, nil
	}
	return Tuple{Name: name, Values: []Value{inner}}, nil
}

// internalVariant writes a variant of an internally tagged enum: the fields
// of the variant preceded by the tag field.
func (e *encoder) internalVariant(rv reflect.Value, name string, rep EnumOptions, kind variantKind, flds []Field, vals []Value, inner Value) (Value, error) {
	tag := Field{Name: rep.Tag, Value: String(name)}
	fail := func(msg string) (Value, error) {
		return nil, &UnsupportedValueError{Value: rv, Msg: "variant " + name + " of an internally tagged enum " + msg}
	}
	if kind == variantTuple {
		if len(vals) != 1 {
			return fail("cannot be a tuple")
		}
		kind, inner = variantNewtype, vals[0]
	}
	switch kind {
	case variantUnit:
		return Struct{Fields: []Field{tag}}, nil
	case variantStruct:
		return Struct{Fields: append([]Field{tag}, flds...)}, nil
	}
	switch x := inner.(type) {
	case Struct:
		return Struct{Fields: append([]Field{tag}, x.Fields...)}, nil
	case Unit:
		return Struct{Fields: []Field{tag}}, nil
	case Map:
		out := Map{{Key: String(rep.Tag), Value: String(name)}}
		return append(out, x...), nil
	}
	return fail("must contain a struct or a map")
}

func hasCustomMarshaler(t reflect.Type) bool { return flagsOf(t).customEnc }

// addressable returns a pointer to rv, copying it when it is not
// addressable, so that pointer receiver methods can be called.
func addressable(rv reflect.Value) reflect.Value {
	if rv.CanAddr() {
		return rv.Addr()
	}
	p := reflect.New(rv.Type())
	p.Elem().Set(rv)
	return p
}

func (e *encoder) encodePlain(rv reflect.Value) (Value, error) {
	t := rv.Type()

	if t.Kind() == reflect.Pointer && rv.IsNil() {
		return None, nil
	}
	if info := e.reg.unitEnum(t); info != nil {
		name, ok := info.byValue[rv.Interface()]
		if !ok {
			return nil, &UnsupportedValueError{Value: rv, Msg: "value " + fmtValue(rv) + " of unit enum " + t.String() + " has no registered name"}
		}
		return Ident(name), nil
	}

	// Plain scalars cannot implement any interface we care about, so they
	// skip the checks below.
	switch t.Kind() {
	case reflect.String:
		if t == stringType {
			return String(rv.String()), nil
		}
	case reflect.Int, reflect.Int64:
		if t.PkgPath() == "" {
			return NewInt(rv.Int()), nil
		}
	case reflect.Float64:
		if t.PkgPath() == "" {
			return Float(rv.Float()), nil
		}
	case reflect.Bool:
		if t.PkgPath() == "" {
			return Bool(rv.Bool()), nil
		}
	}

	f := flagsOf(t)
	if f.value {
		return rv.Interface().(Value), nil
	}
	if t == bigIntType {
		b := addressable(rv).Interface().(*big.Int)
		return NewBigInt(b), nil
	}
	if t.Kind() == reflect.Pointer && t.Elem() == bigIntType {
		// *big.Int is the idiomatic way to hold a big integer, so it is not
		// treated as an Option.
		return NewBigInt(rv.Interface().(*big.Int)), nil
	}

	switch {
	case f.marshaler:
		return e.callMarshaler(rv)
	case f.ptrMarshaler:
		return e.callMarshaler(addressable(rv))
	case f.text:
		return e.callTextMarshaler(rv)
	case f.ptrText:
		return e.callTextMarshaler(addressable(rv))
	}

	switch t.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return NewInt(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return NewUint(rv.Uint()), nil
	case reflect.Float32:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return Float(f), nil
		}
		// Round trip through the shortest float32 representation so that
		// 0.1 prints as 0.1 instead of 0.10000000149011612.
		f, _ = strconv.ParseFloat(strconv.FormatFloat(f, 'g', -1, 32), 64)
		return Float(f), nil
	case reflect.Float64:
		return Float(rv.Float()), nil
	case reflect.String:
		return String(rv.String()), nil
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 && !hasCustomMarshaler(t.Elem()) && e.reg.unitEnum(t.Elem()) == nil {
			return Bytes(slices.Clone(rv.Bytes())), nil
		}
		return e.encodeList(rv)
	case reflect.Array:
		return e.encodeList(rv)
	case reflect.Map:
		return e.encodeMap(rv)
	case reflect.Struct:
		return e.encodeStruct(rv)
	case reflect.Pointer:
		inner, err := e.encode(rv.Elem())
		if err != nil {
			return nil, err
		}
		return e.wrapSome(inner), nil
	case reflect.Interface:
		return e.encode(rv)
	}
	return nil, &UnsupportedTypeError{Type: t}
}

func (e *encoder) wrapSome(v Value) Value {
	// An inner Option must stay wrapped, or Some(None) would collapse into
	// None under implicit_some.
	if _, isOption := v.(Option); e.opts.implicitSome && !isOption {
		return v
	}
	return Some(v)
}

func (e *encoder) callMarshaler(rv reflect.Value) (Value, error) {
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return None, nil
	}
	b, err := rv.Interface().(Marshaler).MarshalRON()
	if err != nil {
		return nil, &MarshalerError{Type: rv.Type(), Err: err, Method: "MarshalRON"}
	}
	v, err := Parse(b)
	if err != nil {
		return nil, &MarshalerError{Type: rv.Type(), Err: err, Method: "MarshalRON"}
	}
	return v, nil
}

func (e *encoder) callTextMarshaler(rv reflect.Value) (Value, error) {
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return None, nil
	}
	b, err := rv.Interface().(encoding.TextMarshaler).MarshalText()
	if err != nil {
		return nil, &MarshalerError{Type: rv.Type(), Err: err, Method: "MarshalText"}
	}
	return String(b), nil
}

func (e *encoder) encodeList(rv reflect.Value) (Value, error) {
	list := make(List, rv.Len())
	for i := range list {
		v, err := e.encode(rv.Index(i))
		if err != nil {
			return nil, err
		}
		list[i] = v
	}
	return list, nil
}

func (e *encoder) encodeMap(rv reflect.Value) (Value, error) {
	type entry struct {
		sortKey string
		MapEntry
	}
	entries := make([]entry, 0, rv.Len())
	// Strings and integers sort by value. Other keys, and maps whose keys
	// may mix kinds, sort by their RON text, which is only computed then.
	needText := true
	switch kt := rv.Type().Key(); kt.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		needText = e.reg.unitEnum(kt) != nil || hasCustomMarshaler(kt)
	}
	iter := rv.MapRange()
	for iter.Next() {
		k, err := e.encode(iter.Key())
		if err != nil {
			return nil, err
		}
		v, err := e.encode(iter.Value())
		if err != nil {
			return nil, err
		}
		en := entry{MapEntry: MapEntry{Key: k, Value: v}}
		if needText {
			p := &printer{}
			p.value(k)
			en.sortKey = p.sb.String()
		}
		entries = append(entries, en)
	}
	// A total order: keys of different kinds sort by kind first, then
	// strings and integers by value and everything else by RON text.
	slices.SortFunc(entries, func(a, b entry) int {
		if c := cmp.Compare(a.Key.Kind(), b.Key.Kind()); c != 0 {
			return c
		}
		switch ak := a.Key.(type) {
		case String:
			return cmp.Compare(ak, b.Key.(String))
		case Int:
			return ak.Cmp(b.Key.(Int))
		}
		return cmp.Compare(a.sortKey, b.sortKey)
	})
	m := make(Map, len(entries))
	for i, en := range entries {
		m[i] = en.MapEntry
	}
	return m, nil
}

func (e *encoder) encodeStruct(rv reflect.Value) (Value, error) {
	si := fields.ForNaming(rv.Type(), e.opts.naming)
	name := ""
	if e.opts.structNames {
		name = si.Name
	}
	if si.Tuple {
		vals, err := e.tupleValues(rv, si)
		if err != nil {
			return nil, err
		}
		if e.opts.unwrapNewtypes && len(vals) == 1 {
			return vals[0], nil
		}
		return Tuple{Name: name, Values: vals}, nil
	}
	if len(si.Fields) == 0 && si.Extra == nil {
		if name != "" {
			return Ident(name), nil
		}
		return Unit{}, nil
	}
	fields, err := e.structFields(rv, si)
	if err != nil {
		return nil, err
	}
	return Struct{Name: name, Fields: fields}, nil
}

func (e *encoder) structFields(rv reflect.Value, si *fields.Info) ([]Field, error) {
	out := make([]Field, 0, len(si.Fields))
	for _, f := range si.Fields {
		fv := fields.ByIndex(rv, f.Index, false)
		if !fv.IsValid() {
			continue
		}
		if (f.OmitEmpty && isEmptyValue(fv)) || (f.OmitZero && isZeroValue(fv)) {
			continue
		}
		v, err := e.encode(fv)
		if err != nil {
			return nil, err
		}
		out = append(out, Field{Name: f.Name, Value: v})
	}
	if si.Extra != nil {
		return e.extraFields(rv, si, out)
	}
	return out, nil
}

// extraFields appends the entries of the flattened catch-all map as fields.
func (e *encoder) extraFields(rv reflect.Value, si *fields.Info, out []Field) ([]Field, error) {
	m := fields.ByIndex(rv, si.Extra.Index, false)
	if !m.IsValid() || m.Len() == 0 {
		return out, nil
	}
	keys := make([]string, 0, m.Len())
	for iter := m.MapRange(); iter.Next(); {
		keys = append(keys, iter.Key().String())
	}
	slices.Sort(keys)
	for _, k := range keys {
		v, err := e.encode(m.MapIndex(reflect.ValueOf(k).Convert(m.Type().Key())))
		if err != nil {
			return nil, err
		}
		out = append(out, Field{Name: k, Value: v})
	}
	return out, nil
}

func (e *encoder) tupleValues(rv reflect.Value, si *fields.Info) ([]Value, error) {
	vals := make([]Value, 0, len(si.Fields))
	for _, f := range si.Fields {
		fv := fields.ByIndex(rv, f.Index, false)
		if !fv.IsValid() {
			vals = append(vals, None)
			continue
		}
		v, err := e.encode(fv)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, nil
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}

type isZeroer interface{ IsZero() bool }

var isZeroerType = reflect.TypeFor[isZeroer]()

func isZeroValue(v reflect.Value) bool {
	if v.Type().Implements(isZeroerType) {
		if (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
			return true
		}
		return v.Interface().(isZeroer).IsZero()
	}
	if reflect.PointerTo(v.Type()).Implements(isZeroerType) {
		return addressable(v).Interface().(isZeroer).IsZero()
	}
	return v.IsZero()
}

func fmtValue(rv reflect.Value) string {
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10)
	case reflect.String:
		return strconv.Quote(rv.String())
	}
	return rv.Type().String() + " value"
}

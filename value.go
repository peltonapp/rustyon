// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"math"
	"math/big"
	"strconv"
)

// Kind identifies the concrete type of a [Value].
type Kind uint8

// The kinds of RON values.
const (
	KindUnit   Kind = iota + 1 // ()
	KindBool                   // true, false
	KindInt                    // 42, -0x2A, 1_000u32
	KindFloat                  // 1.5, -inf, NaN
	KindString                 // "text", r#"raw"#
	KindChar                   // 'c'
	KindBytes                  // b"bytes"
	KindList                   // [a, b]
	KindMap                    // {k: v}
	KindTuple                  // (a, b), Name(a, b)
	KindStruct                 // (f: v), Name(f: v)
	KindOption                 // None, Some(v)
	KindIdent                  // Name (unit struct or unit enum variant)
)

var kindNames = [...]string{
	KindUnit:   "unit",
	KindBool:   "bool",
	KindInt:    "integer",
	KindFloat:  "float",
	KindString: "string",
	KindChar:   "char",
	KindBytes:  "byte string",
	KindList:   "list",
	KindMap:    "map",
	KindTuple:  "tuple",
	KindStruct: "struct",
	KindOption: "option",
	KindIdent:  "identifier",
}

// String returns a human readable name for the kind.
func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "invalid"
}

// Value is a node of a parsed RON document. It is implemented by [Unit],
// [Bool], [Int], [Float], [String], [Char], [Bytes], [List], [Map], [Tuple],
// [Struct], [Option] and [Ident].
//
// A Value can be passed to [Marshal] and can be the target of [Unmarshal],
// which makes it possible to work with RON documents whose shape is not
// known in advance.
type Value interface {
	Kind() Kind
	isValue()
}

// Unit is the unit value ().
type Unit struct{}

// Bool is a boolean value.
type Bool bool

// Int is an integer of arbitrary size. RON allows integers up to 128 bits,
// which do not fit into Go's builtin integer types. Values that fit into an
// int64 are stored inline; larger ones use a [big.Int]. The zero value
// represents 0.
type Int struct {
	small int64
	b     *big.Int // non-nil only for values outside the int64 range
}

// Float is a floating point value.
type Float float64

// String is a string value.
type String string

// Char is a character value such as 'a'.
type Char rune

// Bytes is a byte string value such as b"\x00\xff".
type Bytes []byte

// List is a sequence value such as [1, 2, 3].
type List []Value

// MapEntry is a single key value pair of a [Map].
type MapEntry struct {
	Key   Value
	Value Value
}

// Map is a map value such as {"a": 1}. Entries keep the order of the source
// document. Keys can be any value.
type Map []MapEntry

// Tuple is a tuple such as (1, 2), a tuple struct such as Point(1, 2) or a
// tuple enum variant. Name is empty for anonymous tuples.
type Tuple struct {
	Name   string
	Values []Value
}

// Field is a single named field of a [Struct].
type Field struct {
	Name  string
	Value Value
}

// Struct is a struct such as (x: 1, y: 2), a named struct such as
// Point(x: 1, y: 2) or a struct enum variant. Name is empty for anonymous
// structs.
type Struct struct {
	Name   string
	Fields []Field
}

// Option is an optional value. A nil Value represents None, anything else
// represents Some(Value).
type Option struct {
	Value Value
}

// Ident is a bare identifier such as Red. It represents a unit struct or a
// unit enum variant.
type Ident string

// None is the empty [Option].
var None = Option{}

// Some wraps v into an [Option].
func Some(v Value) Option { return Option{Value: v} }

// IsNone reports whether o is None.
func (o Option) IsNone() bool { return o.Value == nil }

// NewInt returns an [Int] holding v.
func NewInt(v int64) Int { return Int{small: v} }

// NewUint returns an [Int] holding v.
func NewUint(v uint64) Int {
	if v <= math.MaxInt64 {
		return Int{small: int64(v)}
	}
	return Int{b: new(big.Int).SetUint64(v)}
}

// NewBigInt returns an [Int] holding a copy of v.
func NewBigInt(v *big.Int) Int {
	if v.IsInt64() {
		return Int{small: v.Int64()}
	}
	return Int{b: new(big.Int).Set(v)}
}

func (i Int) bigInt() *big.Int {
	if i.b != nil {
		return i.b
	}
	return big.NewInt(i.small)
}

// Big returns a copy of the integer as a [big.Int].
func (i Int) Big() *big.Int { return new(big.Int).Set(i.bigInt()) }

// Int64 returns the integer as an int64 and reports whether it fits.
func (i Int) Int64() (int64, bool) {
	if i.b != nil {
		return 0, false
	}
	return i.small, true
}

// Uint64 returns the integer as a uint64 and reports whether it fits.
func (i Int) Uint64() (uint64, bool) {
	if i.b == nil {
		return uint64(i.small), i.small >= 0
	}
	if !i.b.IsUint64() {
		return 0, false
	}
	return i.b.Uint64(), true
}

// Float64 returns the nearest float64 to the integer.
func (i Int) Float64() float64 {
	if i.b == nil {
		return float64(i.small)
	}
	f, _ := new(big.Float).SetInt(i.b).Float64()
	return f
}

// Sign returns -1, 0 or 1 depending on the sign of the integer.
func (i Int) Sign() int {
	if i.b != nil {
		return i.b.Sign()
	}
	switch {
	case i.small < 0:
		return -1
	case i.small > 0:
		return 1
	}
	return 0
}

// Cmp compares i and j and returns -1, 0 or 1.
func (i Int) Cmp(j Int) int {
	if i.b == nil && j.b == nil {
		switch {
		case i.small < j.small:
			return -1
		case i.small > j.small:
			return 1
		}
		return 0
	}
	return i.bigInt().Cmp(j.bigInt())
}

// String returns the decimal representation of the integer.
func (i Int) String() string {
	if i.b == nil {
		return strconv.FormatInt(i.small, 10)
	}
	return i.b.String()
}

// Get returns the value of the field with the given name.
func (s Struct) Get(name string) (Value, bool) {
	for _, f := range s.Fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return nil, false
}

// Set replaces the value of the field with the given name, or appends a new
// field if there is none.
func (s *Struct) Set(name string, v Value) {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			s.Fields[i].Value = v
			return
		}
	}
	s.Fields = append(s.Fields, Field{Name: name, Value: v})
}

// Get returns the value stored under a key equal to key.
func (m Map) Get(key Value) (Value, bool) {
	for _, e := range m {
		if Equal(e.Key, key) {
			return e.Value, true
		}
	}
	return nil, false
}

// Set replaces the value stored under key, or appends a new entry if there
// is none.
func (m *Map) Set(key, v Value) {
	for i := range *m {
		if Equal((*m)[i].Key, key) {
			(*m)[i].Value = v
			return
		}
	}
	*m = append(*m, MapEntry{Key: key, Value: v})
}

func (Unit) Kind() Kind   { return KindUnit }
func (Bool) Kind() Kind   { return KindBool }
func (Int) Kind() Kind    { return KindInt }
func (Float) Kind() Kind  { return KindFloat }
func (String) Kind() Kind { return KindString }
func (Char) Kind() Kind   { return KindChar }
func (Bytes) Kind() Kind  { return KindBytes }
func (List) Kind() Kind   { return KindList }
func (Map) Kind() Kind    { return KindMap }
func (Tuple) Kind() Kind  { return KindTuple }
func (Struct) Kind() Kind { return KindStruct }
func (Option) Kind() Kind { return KindOption }
func (Ident) Kind() Kind  { return KindIdent }

func (Unit) isValue()   {}
func (Bool) isValue()   {}
func (Int) isValue()    {}
func (Float) isValue()  {}
func (String) isValue() {}
func (Char) isValue()   {}
func (Bytes) isValue()  {}
func (List) isValue()   {}
func (Map) isValue()    {}
func (Tuple) isValue()  {}
func (Struct) isValue() {}
func (Option) isValue() {}
func (Ident) isValue()  {}

// Equal reports whether a and b are structurally equal. Floats compare by
// value, except that NaN equals NaN so that documents containing NaN compare
// equal to themselves. Nil values are equal only to nil.
func Equal(a, b Value) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch a := a.(type) {
	case Unit:
		return true
	case Bool:
		return a == b.(Bool)
	case Int:
		return a.Cmp(b.(Int)) == 0
	case Float:
		bf := b.(Float)
		if math.IsNaN(float64(a)) {
			return math.IsNaN(float64(bf))
		}
		return a == bf
	case String:
		return a == b.(String)
	case Char:
		return a == b.(Char)
	case Bytes:
		return bytes.Equal(a, b.(Bytes))
	case Ident:
		return a == b.(Ident)
	case Option:
		return Equal(a.Value, b.(Option).Value)
	case List:
		bl := b.(List)
		if len(a) != len(bl) {
			return false
		}
		for i := range a {
			if !Equal(a[i], bl[i]) {
				return false
			}
		}
		return true
	case Map:
		bm := b.(Map)
		if len(a) != len(bm) {
			return false
		}
		for i := range a {
			if !Equal(a[i].Key, bm[i].Key) || !Equal(a[i].Value, bm[i].Value) {
				return false
			}
		}
		return true
	case Tuple:
		bt := b.(Tuple)
		if a.Name != bt.Name || len(a.Values) != len(bt.Values) {
			return false
		}
		for i := range a.Values {
			if !Equal(a.Values[i], bt.Values[i]) {
				return false
			}
		}
		return true
	case Struct:
		bs := b.(Struct)
		if a.Name != bs.Name || len(a.Fields) != len(bs.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != bs.Fields[i].Name || !Equal(a.Fields[i].Value, bs.Fields[i].Value) {
				return false
			}
		}
		return true
	}
	return false
}

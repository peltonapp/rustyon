// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

// String methods for the Value types that have none, so that printing a
// value with fmt shows compact RON instead of Go's struct syntax. The
// scalar types Bool, Float, String, Char, Bytes and Ident keep their
// default formatting: their Go representation is what code printing them
// with fmt expects. Use [Marshal] on a Value for the RON text of any value.

func compactRON(v Value) string {
	p := &printer{}
	p.value(v)
	return p.sb.String()
}

// String returns the compact RON text "()".
func (v Unit) String() string { return compactRON(v) }

// String returns the compact RON text, such as None or Some(1).
func (v Option) String() string { return compactRON(v) }

// String returns the compact RON text, such as [1,2,3].
func (v List) String() string { return compactRON(v) }

// String returns the compact RON text, such as {"a":1}.
func (v Map) String() string { return compactRON(v) }

// String returns the compact RON text of the entry, such as "a":1.
func (v MapEntry) String() string {
	p := &printer{}
	p.value(v.Key)
	p.colon()
	p.value(v.Value)
	return p.sb.String()
}

// String returns the compact RON text, such as Name(1,2) or (1,2).
func (v Tuple) String() string { return compactRON(v) }

// String returns the compact RON text of the field, such as x:1.
func (v Field) String() string {
	p := &printer{}
	p.ident(v.Name)
	p.colon()
	p.value(v.Value)
	return p.sb.String()
}

// String returns the compact RON text, such as Point(x:1,y:2).
func (v Struct) String() string { return compactRON(v) }

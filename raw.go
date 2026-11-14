// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import "reflect"

// Raw holds one RON value as text, like encoding/json's RawMessage. Use it
// to delay decoding of part of a document, or to pass a value through
// unchanged:
//
//	type Envelope struct {
//		Kind    string
//		Payload rustyon.Raw // decoded later, once Kind is known
//	}
//
// When decoding, a Raw captures the text of the value exactly as written,
// including comments inside it, as long as the document does not enable any
// extensions. With extensions the value is stored as compact RON printed from
// its parse tree, so that the text means the same without them.
//
// When encoding, the text must be a single valid RON value or encoding
// fails. The value is parsed and written in the layout of the surrounding
// output, so comments and spacing inside it are not kept. A nil Raw encodes
// as None.
type Raw []byte

var rawType = reflect.TypeFor[Raw]()

// MarshalRON returns r, or None when r is nil.
func (r Raw) MarshalRON() ([]byte, error) {
	if r == nil {
		return []byte("None"), nil
	}
	return r, nil
}

// UnmarshalRON stores a copy of b in r.
func (r *Raw) UnmarshalRON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

// Decode decodes the value in r into v, like [Unmarshal].
func (r Raw) Decode(v any) error { return Unmarshal(r, v) }

// UnmarshalAs decodes data into a new value of type T. It is [Unmarshal]
// without the pointer, for example
//
//	cfg, err := rustyon.UnmarshalAs[Config](data)
//
// The optional opts select the field naming, unknown field handling and
// registry like [UnmarshalWith]. Only the first is used.
func UnmarshalAs[T any](data []byte, opts ...DecodeOptions) (T, error) {
	var v T
	var err error
	if len(opts) > 0 {
		err = UnmarshalWith(data, &v, opts[0])
	} else {
		err = Unmarshal(data, &v)
	}
	return v, err
}

// DecodeAs reads the next value from dec into a new value of type T, like
// [Decoder.Decode].
func DecodeAs[T any](dec *Decoder) (T, error) {
	var v T
	err := dec.Decode(&v)
	return v, err
}

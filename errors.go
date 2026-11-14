// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"fmt"
	"reflect"
)

// Position is a location in a RON document. Line and Column are 1-based;
// Column counts Unicode code points. Offset is the 0-based byte offset.
type Position struct {
	Offset int
	Line   int
	Column int
}

// String returns the position as "line:column".
func (p Position) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Column) }

// SyntaxError describes malformed RON input.
type SyntaxError struct {
	Msg string
	Pos Position
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("rustyon: syntax error at %s: %s", e.Pos, e.Msg)
}

// UnmarshalTypeError describes a RON value that cannot be stored in a Go
// value of a specific type.
type UnmarshalTypeError struct {
	Value string       // description of the RON value, e.g. "string" or "tuple Circle"
	Type  reflect.Type // Go type that could not hold the value
	Path  string       // path to the value within the document, e.g. "servers[0].port"
	Msg   string       // optional detail
}

func (e *UnmarshalTypeError) Error() string {
	msg := fmt.Sprintf("rustyon: cannot unmarshal %s into Go value of type %s", e.Value, e.Type)
	if e.Path != "" {
		msg += " at " + e.Path
	}
	if e.Msg != "" {
		msg += ": " + e.Msg
	}
	return msg
}

// UnknownFieldError is returned by a [Decoder] with
// [Decoder.DisallowUnknownFields] set when a struct field in the input has no
// matching Go field.
type UnknownFieldError struct {
	Field string
	Type  reflect.Type
	Path  string
}

func (e *UnknownFieldError) Error() string {
	msg := fmt.Sprintf("rustyon: unknown field %q for type %s", e.Field, e.Type)
	if e.Path != "" {
		msg += " at " + e.Path
	}
	return msg
}

// InvalidUnmarshalError describes an invalid argument passed to [Unmarshal].
// The argument must be a non-nil pointer.
type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "rustyon: Unmarshal(nil)"
	}
	if e.Type.Kind() != reflect.Pointer {
		return "rustyon: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "rustyon: Unmarshal(nil " + e.Type.String() + ")"
}

// UnsupportedTypeError is returned by [Marshal] for Go values that have no
// RON representation, such as channels and functions.
type UnsupportedTypeError struct {
	Type reflect.Type
}

func (e *UnsupportedTypeError) Error() string {
	return "rustyon: unsupported type: " + e.Type.String()
}

// UnsupportedValueError is returned by [Marshal] for Go values that cannot
// be encoded, such as a unit enum value without a registered name.
type UnsupportedValueError struct {
	Value reflect.Value
	Msg   string
}

func (e *UnsupportedValueError) Error() string {
	return "rustyon: unsupported value: " + e.Msg
}

// MarshalerError wraps an error returned by a [Marshaler] or
// [encoding.TextMarshaler].
type MarshalerError struct {
	Type   reflect.Type
	Err    error
	Method string // "MarshalRON" or "MarshalText"
}

func (e *MarshalerError) Error() string {
	method := e.Method
	if method == "" {
		method = "MarshalRON"
	}
	return "rustyon: error calling " + method + " for type " + e.Type.String() + ": " + e.Err.Error()
}

func (e *MarshalerError) Unwrap() error { return e.Err }

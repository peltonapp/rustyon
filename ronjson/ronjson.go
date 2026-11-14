// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Package ronjson converts between RON and JSON.
//
// RON is richer than JSON, so the conversion to JSON loses some
// information:
//
//   - () and None become null; Some(v) becomes v.
//   - Structs become objects and tuples become arrays. Struct, tuple and
//     enum variant names are dropped; unit variants such as Red become the
//     string "Red".
//   - Chars become strings and byte strings become arrays of numbers.
//   - Map keys become strings. Keys that are lists, maps, structs or tuples
//     cannot be converted.
//   - NaN and infinities cannot be represented and return an error.
//
// Object key order and integer precision are preserved in both directions.
package ronjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/peltonapp/rustyon-go"
)

// ToJSON converts a RON document to JSON. With an empty indent the output
// is compact.
func ToJSON(src []byte, indent string) ([]byte, error) {
	v, err := rustyon.Parse(src)
	if err != nil {
		return nil, err
	}
	return ValueToJSON(v, indent)
}

// ValueToJSON converts a RON value to JSON. With an empty indent the output
// is compact.
func ValueToJSON(v rustyon.Value, indent string) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, v); err != nil {
		return nil, err
	}
	if indent == "" {
		return buf.Bytes(), nil
	}
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", indent); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, v rustyon.Value) error {
	switch v := v.(type) {
	case nil, rustyon.Unit:
		buf.WriteString("null")
	case rustyon.Option:
		if v.IsNone() {
			buf.WriteString("null")
			return nil
		}
		return writeJSON(buf, v.Value)
	case rustyon.Bool:
		buf.WriteString(strconv.FormatBool(bool(v)))
	case rustyon.Int:
		buf.WriteString(v.String())
	case rustyon.Float:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("ronjson: %v cannot be represented in JSON", f)
		}
		b, _ := json.Marshal(f)
		buf.Write(b)
	case rustyon.String:
		writeString(buf, string(v))
	case rustyon.Char:
		writeString(buf, string(rune(v)))
	case rustyon.Ident:
		writeString(buf, string(v))
	case rustyon.Bytes:
		buf.WriteByte('[')
		for i, b := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(strconv.Itoa(int(b)))
		}
		buf.WriteByte(']')
	case rustyon.List:
		return writeArray(buf, v)
	case rustyon.Tuple:
		return writeArray(buf, v.Values)
	case rustyon.Struct:
		buf.WriteByte('{')
		for i, f := range v.Fields {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeString(buf, f.Name)
			buf.WriteByte(':')
			if err := writeJSON(buf, f.Value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case rustyon.Map:
		buf.WriteByte('{')
		for i, e := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			k, err := keyString(e.Key)
			if err != nil {
				return err
			}
			writeString(buf, k)
			buf.WriteByte(':')
			if err := writeJSON(buf, e.Value); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("ronjson: unsupported value %T", v)
	}
	return nil
}

func writeArray(buf *bytes.Buffer, vals []rustyon.Value) error {
	buf.WriteByte('[')
	for i, e := range vals {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := writeJSON(buf, e); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func writeString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}

func keyString(k rustyon.Value) (string, error) {
	switch k := k.(type) {
	case rustyon.String:
		return string(k), nil
	case rustyon.Char:
		return string(rune(k)), nil
	case rustyon.Ident:
		return string(k), nil
	case rustyon.Int:
		return k.String(), nil
	case rustyon.Bool:
		return strconv.FormatBool(bool(k)), nil
	case rustyon.Float:
		return strconv.FormatFloat(float64(k), 'g', -1, 64), nil
	case rustyon.Unit:
		return "()", nil
	case rustyon.Option:
		if k.IsNone() {
			return "None", nil
		}
		return keyString(k.Value)
	}
	return "", fmt.Errorf("ronjson: a %s cannot be used as a JSON object key", k.Kind())
}

// Options controls [FromJSON].
type Options struct {
	// Structs writes JSON objects whose keys are all valid identifiers as
	// RON structs (key: value) instead of maps {"key": value}.
	Structs bool
	// Indent pretty prints the output with the given indent. An empty
	// Indent produces compact output.
	Indent string
}

// FromJSON converts a JSON document to RON.
func FromJSON(src []byte, opts Options) ([]byte, error) {
	v, err := JSONToValue(src, opts)
	if err != nil {
		return nil, err
	}
	if opts.Indent != "" {
		return rustyon.MarshalIndent(v, "", opts.Indent)
	}
	return rustyon.Marshal(v)
}

// JSONToValue converts a JSON document to a RON value tree. null becomes
// None, integers become [rustyon.Int] without loss of precision and other
// numbers become [rustyon.Float].
func JSONToValue(src []byte, opts Options) (rustyon.Value, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	v, err := readValue(dec, opts)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("ronjson: unexpected data after JSON value")
	}
	return v, nil
}

func readValue(dec *json.Decoder, opts Options) (rustyon.Value, error) {
	tok, err := dec.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("ronjson: unexpected end of JSON input")
		}
		return nil, err
	}
	switch t := tok.(type) {
	case nil:
		return rustyon.None, nil
	case bool:
		return rustyon.Bool(t), nil
	case string:
		return rustyon.String(t), nil
	case json.Number:
		s := t.String()
		if !strings.ContainsAny(s, ".eE") {
			if n, ok := new(big.Int).SetString(s, 10); ok {
				return rustyon.NewBigInt(n), nil
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("ronjson: invalid number %s", s)
		}
		return rustyon.Float(f), nil
	case json.Delim:
		switch t {
		case '[':
			list := rustyon.List{}
			for dec.More() {
				v, err := readValue(dec, opts)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return list, nil
		case '{':
			var keys []string
			var vals []rustyon.Value
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := readValue(dec, opts)
				if err != nil {
					return nil, err
				}
				keys = append(keys, kt.(string))
				vals = append(vals, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			if opts.Structs && len(keys) > 0 && allIdents(keys) {
				s := rustyon.Struct{Fields: make([]rustyon.Field, len(keys))}
				for i := range keys {
					s.Fields[i] = rustyon.Field{Name: keys[i], Value: vals[i]}
				}
				return s, nil
			}
			m := make(rustyon.Map, len(keys))
			for i := range keys {
				m[i] = rustyon.MapEntry{Key: rustyon.String(keys[i]), Value: vals[i]}
			}
			return m, nil
		}
	}
	return nil, fmt.Errorf("ronjson: unexpected token %v", tok)
}

func allIdents(keys []string) bool {
	for _, k := range keys {
		if k == "" {
			return false
		}
		for i, r := range k {
			isLetter := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !isLetter && (i == 0 || r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

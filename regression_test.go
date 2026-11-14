// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// Map keys of mixed kinds must sort deterministically.
func TestMixedMapKeyOrder(t *testing.T) {
	m := map[any]int{9: 0, 10: 0, 2.5: 0, "a": 0, true: 0}
	first, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for range 200 {
		again, _ := Marshal(m)
		if !bytes.Equal(first, again) {
			t.Fatalf("non-deterministic output: %s vs %s", first, again)
		}
	}
	if want := `{true:0,9:0,10:0,2.5:0,"a":0}`; string(first) != want {
		t.Errorf("got %s, want %s", first, want)
	}
}

func TestExplicitStructNames(t *testing.T) {
	var c basicStruct
	err := Unmarshal([]byte("#![enable(explicit_struct_names)] (name: \"x\")"), &c)
	if err == nil || !strings.Contains(err.Error(), "explicit_struct_names") {
		t.Errorf("got %v, want explicit_struct_names error", err)
	}
	if err := Unmarshal([]byte("#![enable(explicit_struct_names)] Config(name: \"x\")"), &c); err != nil {
		t.Errorf("named struct: %v", err)
	}
	// Maps and lists are not structs and need no name.
	var m map[string]int
	if err := Unmarshal([]byte(`#![enable(explicit_struct_names)] {"a": 1}`), &m); err != nil {
		t.Errorf("map: %v", err)
	}

	dec := NewDecoder(strings.NewReader("#![enable(explicit_struct_names)] (name: \"x\")"))
	if err := dec.Decode(&c); err == nil {
		t.Error("Decoder did not enforce explicit_struct_names")
	}
}

func TestDecodeWrongValueKind(t *testing.T) {
	var st Struct
	err := Unmarshal([]byte("(1, 2)"), &st)
	var te *UnmarshalTypeError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "expected struct") {
		t.Errorf("got %v, want type error expecting struct", err)
	}
	var id Ident
	if err := Unmarshal([]byte(`"x"`), &id); err == nil {
		t.Error("string decoded into Ident")
	}
}

type failingText struct{}

func (failingText) MarshalText() ([]byte, error) { return nil, errors.New("nope") }

func TestMarshalerErrorMethod(t *testing.T) {
	_, err := Marshal(failingText{})
	if err == nil || !strings.Contains(err.Error(), "MarshalText") {
		t.Errorf("got %v, want mention of MarshalText", err)
	}
}

func TestImplicitSomeKeepsNestedOption(t *testing.T) {
	var inner *int
	v := struct{ P **int }{&inner}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetImplicitSome(true)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	if want := "#![enable(implicit_some)](p:Some(None))\n"; buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
	var back struct{ P **int }
	if err := Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if back.P == nil || *back.P != nil {
		t.Errorf("Some(None) did not round trip: %+v", back)
	}
}

func TestCharIntoUnsigned(t *testing.T) {
	var u uint32
	if err := Unmarshal([]byte("'a'"), &u); err != nil || u != 'a' {
		t.Errorf("got %d, %v", u, err)
	}
}

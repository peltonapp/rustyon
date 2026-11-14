// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"errors"
	"reflect"
	"testing"
)

type flatBase struct {
	ID   int
	Name string
}

type flatMeta struct {
	_         struct{} `ron:",rename_all=camelCase"`
	CreatedBy string
}

type flatDoc struct {
	Title string
	Base  flatBase       `ron:",flatten"`
	Meta  *flatMeta      `ron:",flatten"`
	Extra map[string]any `ron:",flatten"`
	Last  int
}

type flatTyped struct {
	A    int
	Rest map[string]int `ron:",flatten"`
}

type flatNotFlat struct {
	A    int
	Sub  flatBase       // a normal field, not flattened
	Map  map[string]int // normal
	Skip map[int]int    `ron:",flatten"` // not a string key map: stays a normal field
}

func TestFlattenEncode(t *testing.T) {
	d := flatDoc{
		Title: "t", Base: flatBase{1, "n"}, Meta: &flatMeta{CreatedBy: "me"},
		Extra: map[string]any{"zeta": 1, "alpha": "x"}, Last: 9,
	}
	out, err := Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	want := `(title:"t",id:1,name:"n",createdBy:"me",last:9,alpha:"x",zeta:1)`
	if string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
	// A nil pointer flatten contributes nothing.
	out, _ = Marshal(flatDoc{Title: "t"})
	if string(out) != `(title:"t",id:0,name:"",last:0)` {
		t.Fatalf("%s", out)
	}
	out, _ = Marshal(flatNotFlat{A: 1, Sub: flatBase{2, "x"}})
	if string(out) != `(a:1,sub:(id:2,name:"x"),map:{},skip:{})` {
		t.Fatalf("%s", out)
	}
	if _, err := Marshal(flatTyped{Rest: map[string]int{"not valid": 1}}); err == nil {
		t.Fatal("want an error for a key that is not an identifier")
	}
	out, _ = MarshalWith(d, EncodeOptions{StructNames: true})
	if string(out) != `flatDoc(title:"t",id:1,name:"n",createdBy:"me",last:9,alpha:"x",zeta:1)` {
		t.Fatalf("%s", out)
	}
}

func TestFlattenDecode(t *testing.T) {
	src := `(title: "t", id: 1, name: "n", createdBy: "me", last: 9, alpha: "x", zeta: 1)`
	want := flatDoc{
		Title: "t", Base: flatBase{1, "n"}, Meta: &flatMeta{CreatedBy: "me"},
		Extra: map[string]any{"alpha": "x", "zeta": int64(1)}, Last: 9,
	}
	var got flatDoc
	if err := Unmarshal([]byte(src), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	var viaTree flatDoc
	if err := unmarshalTree([]byte(src), &viaTree, &decodeOptions{}); err != nil || !reflect.DeepEqual(viaTree, want) {
		t.Fatalf("tree: %+v %v", viaTree, err)
	}
	// Map syntax and fields in any order.
	var m flatDoc
	if err := Unmarshal([]byte(`{"zeta": 2, "title": "q", "id": 4}`), &m); err != nil || m.Title != "q" || m.Base.ID != 4 || m.Extra["zeta"] != int64(2) || m.Meta != nil {
		t.Fatalf("%+v %v", m, err)
	}
	// With a catch-all map no field is unknown.
	var typed flatTyped
	if err := UnmarshalWith([]byte(`(a: 1, x: 2, y: 3)`), &typed, DecodeOptions{DisallowUnknownFields: true}); err != nil {
		t.Fatal(err)
	}
	if typed.A != 1 || !reflect.DeepEqual(typed.Rest, map[string]int{"x": 2, "y": 3}) {
		t.Fatalf("%+v", typed)
	}
	// Errors in catch-all values name the field.
	var te *UnmarshalTypeError
	err := Unmarshal([]byte(`(a: 1, x: "no")`), &typed)
	if err == nil || !asTypeError(err, &te) || te.Path != "x" {
		t.Fatalf("%v", err)
	}
	// Without the catch-all, unknown fields are still unknown.
	var plain flatBase
	if err := UnmarshalWith([]byte(`(id: 1, other: 2)`), &plain, DecodeOptions{DisallowUnknownFields: true}); err == nil {
		t.Fatal("want unknown field error")
	}
	// Round trip.
	out, _ := Marshal(want)
	var back flatDoc
	if err := Unmarshal(out, &back); err != nil || !reflect.DeepEqual(back, want) {
		t.Fatalf("%s: %+v %v", out, back, err)
	}
}

func asTypeError(err error, target **UnmarshalTypeError) bool {
	return errors.As(err, target)
}

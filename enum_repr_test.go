// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"reflect"
	"strings"
	"testing"
)

func decodeWith(t *testing.T, src string, v any, opts DecodeOptions) error {
	t.Helper()
	opts.Registry = streamTagRegistry
	err := UnmarshalWith([]byte(src), v, opts)
	// The tree decoder must agree on success.
	treeTarget := reflect.New(reflect.TypeOf(v).Elem()).Interface()
	treeErr := unmarshalTree([]byte(src), treeTarget, opts.internal())
	if (err == nil) != (treeErr == nil) {
		t.Fatalf("%s: stream %v, tree %v", src, err, treeErr)
	}
	return err
}

func marshalTag(t *testing.T, v any) string {
	t.Helper()
	out, err := MarshalWith(v, EncodeOptions{Registry: streamTagRegistry})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestEnumInternallyTagged(t *testing.T) {
	var names = "n"
	items := []sTagI{tCircle{1.5}, tPoly{3, &names}, tDot{}, tMap{"a": 1, "b": 2}}
	got := marshalTag(t, items)
	want := `[(kind:"Circle",radius:1.5),(kind:"Poly",sides:3,name:Some("n")),(kind:"Dot"),{"kind":"Map","a":1,"b":2}]`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	var back []sTagI
	if err := decodeWith(t, got, &back, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, items) {
		t.Fatalf("%#v", back)
	}
	// The tag may come anywhere, as string or identifier.
	var one sTagI
	if err := decodeWith(t, `(radius: 2.0, kind: Circle)`, &one, DecodeOptions{}); err != nil || one != (tCircle{2}) {
		t.Fatalf("%v %v", one, err)
	}
	// A concrete variant type decodes with or without the tag.
	// (tCircle belongs to enums with different representations here, so a
	// registry with only one is used.)
	r := NewRegistry()
	RegisterEnumWithIn[sTagI](r, EnumOptions{Tag: "kind"}, Variant[tCircle]("Circle"))
	var c tCircle
	if err := UnmarshalWith([]byte(`(kind: "Circle", radius: 4.0)`), &c, DecodeOptions{DisallowUnknownFields: true, Registry: r}); err != nil || c.Radius != 4 {
		t.Fatalf("%v %v", c, err)
	}
	if err := UnmarshalWith([]byte(`(radius: 5.0)`), &c, DecodeOptions{Registry: r}); err != nil || c.Radius != 5 {
		t.Fatalf("%v %v", c, err)
	}
	if err := UnmarshalWith([]byte(`(kind: "Other", radius: 5.0)`), &c, DecodeOptions{Registry: r}); err == nil {
		t.Fatal("a wrong tag must be rejected")
	}
	for _, bad := range []string{`(radius: 1.0)`, `(kind: "Nope")`, `(kind: 5)`, `5`, `{1: 2}`} {
		var x sTagI
		if err := decodeWith(t, bad, &x, DecodeOptions{}); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
	if err := decodeWith(t, `(kind: "Circle", radius: 1.0, junk: 1)`, &one, DecodeOptions{DisallowUnknownFields: true}); err == nil {
		t.Error("unknown field accepted")
	}
}

func TestEnumInternallyTaggedEncodeErrors(t *testing.T) {
	r := NewRegistry()
	RegisterEnumWithIn[sTagI](r, EnumOptions{Tag: "k"}, Variant[tRect2]("Rect"), Variant[tNum]("Num"), Variant[tBox]("Box"))
	for _, v := range []sTagI{tRect2{W: 1, H: 2}, tNum(5)} {
		if _, err := MarshalWith(v, EncodeOptions{Registry: r}); err == nil {
			t.Errorf("%T: want an error", v)
		}
	}
	// A tuple struct with one field is a newtype and works if it holds a struct.
	out, err := MarshalWith(tBox{C: tCircle{1}}, EncodeOptions{Registry: r})
	if err != nil || string(out) != `(k:"Box",radius:1.0)` {
		t.Fatalf("%s %v", out, err)
	}
	// When the wrapped struct is a variant of the same enum, its own tag is
	// dropped instead of writing the tag field twice.
	r2 := NewRegistry()
	RegisterEnumWithIn[sTagI](r2, EnumOptions{Tag: "k"}, Variant[tBox]("Box"), Variant[tCircle]("Circle"))
	out, err = MarshalWith(tBox{C: tCircle{1}}, EncodeOptions{Registry: r2})
	if err != nil || string(out) != `(k:"Box",radius:1.0)` {
		t.Fatalf("%s %v", out, err)
	}
	var back sTagI
	if err := UnmarshalWith(out, &back, DecodeOptions{Registry: r2}); err != nil || back != (tBox{C: tCircle{1}}) {
		t.Fatalf("%#v %v", back, err)
	}
}

type tRect2 struct {
	_    struct{} `ron:",tuple"`
	W, H int
}
type tNum int
type tBox struct {
	_ struct{} `ron:",tuple"`
	C tCircle
}

func (tRect2) sTagI() {}
func (tNum) sTagI()   {}
func (tBox) sTagI()   {}

func TestEnumAdjacentlyTagged(t *testing.T) {
	items := []sTagA{tCircle{1}, tRect{W: 1, H: 2}, tLabel("x"), tDot{}}
	got := marshalTag(t, items)
	want := `[(t:Circle,c:(radius:1.0)),(t:Rect,c:(1,2)),(t:Label,c:"x"),(t:Dot)]`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	var back []sTagA
	if err := decodeWith(t, got, &back, DecodeOptions{}); err != nil || !reflect.DeepEqual(back, items) {
		t.Fatalf("%#v %v", back, err)
	}
	// String tags, content first, unit content.
	var l []sTagA
	if err := decodeWith(t, `[(c: "y", t: "Label"), (t: "Dot", c: ()), (t: Rect, c: (3, 4))]`, &l, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l, []sTagA{tLabel("y"), tDot{}, tRect{W: 3, H: 4}}) {
		t.Fatalf("%#v", l)
	}
	var one sTagA
	if err := decodeWith(t, `(t: Dot, extra: 1)`, &one, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := decodeWith(t, `(t: Dot, extra: 1)`, &one, DecodeOptions{DisallowUnknownFields: true}); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := decodeWith(t, `(t: Label)`, &one, DecodeOptions{}); err != nil || one != tLabel("") {
		t.Fatalf("%v %v", one, err)
	}
}

func TestEnumUntagged(t *testing.T) {
	name := "p"
	items := []sTagU{tCircle{1}, tPoly{2, &name}, tRect{W: 3, H: 4}, tLabel("l"), tDot{}}
	got := marshalTag(t, items)
	want := `[(radius:1.0),(sides:2,name:Some("p")),(3,4),"l",()]`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	var back []sTagU
	if err := decodeWith(t, got, &back, DecodeOptions{}); err != nil || !reflect.DeepEqual(back, items) {
		t.Fatalf("%#v %v", back, err)
	}
	// Optional fields may be missing, other fields may not. Unknown fields
	// are ignored unless disallowed.
	var l []sTagU
	if err := decodeWith(t, `[(sides: 1), (radius: 2.0, junk: 1), (sides: 1, radius: 3.0)]`, &l, DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := l[0].(tPoly); !ok || l[1] != (tCircle{2}) || l[2] != (tCircle{3}) {
		t.Fatalf("%#v", l)
	}
	if err := decodeWith(t, `[(radius: 2.0, junk: 1)]`, &l, DecodeOptions{DisallowUnknownFields: true}); err == nil {
		t.Fatal("unknown field accepted")
	}
	for _, bad := range []string{`(nothing: 1)`, `(1, 2, 3)`, `5`, `[1]`, `(radius: "x")`} {
		var x sTagU
		if err := decodeWith(t, bad, &x, DecodeOptions{}); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
	// A failed attempt leaves nothing behind in the result.
	var x sTagU
	if err := decodeWith(t, `(sides: 1, name: 5)`, &x, DecodeOptions{}); err == nil {
		t.Errorf("want an error, got %#v", x)
	}
}

func TestEnumOptionsValidation(t *testing.T) {
	bad := []EnumOptions{
		{Content: "c"}, {Untagged: true, Tag: "t"}, {Tag: "a b"}, {Tag: "t", Content: "t"}, {Tag: "t", Content: "x y"},
	}
	for _, o := range bad {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%+v: want a panic", o)
				}
			}()
			RegisterEnumWithIn[sTagI](NewRegistry(), o, Variant[tCircle]("Circle"))
		}()
	}
	func() {
		defer func() {
			msg, _ := recover().(error)
			if msg == nil || !strings.Contains(msg.Error(), "another representation") {
				t.Errorf("want a representation conflict, got %v", msg)
			}
		}()
		r := NewRegistry()
		RegisterEnumWithIn[sTagI](r, EnumOptions{Tag: "k"}, Variant[tCircle]("Circle"))
		RegisterEnumIn[sTagI](r, Variant[tDot]("Dot"))
	}()
	// Adding variants with the same options works.
	r := NewRegistry()
	RegisterEnumWithIn[sTagI](r, EnumOptions{Tag: "k"}, Variant[tCircle]("Circle"))
	RegisterEnumWithIn[sTagI](r, EnumOptions{Tag: "k"}, Variant[tDot]("Dot"))
	out, err := MarshalWith([]sTagI{tCircle{1}, tDot{}}, EncodeOptions{Registry: r})
	if err != nil || string(out) != `[(k:"Circle",radius:1.0),(k:"Dot")]` {
		t.Fatalf("%s %v", out, err)
	}
}

func TestEnumRepresentationsKeepDefault(t *testing.T) {
	// The plain RegisterEnum keeps the external representation.
	r := NewRegistry()
	RegisterEnumIn[sTagI](r, Variant[tCircle]("Circle"), Variant[tDot]("Dot"))
	out, _ := MarshalWith([]sTagI{tCircle{1}, tDot{}}, EncodeOptions{Registry: r})
	if string(out) != `[Circle(radius:1.0),Dot]` {
		t.Fatalf("%s", out)
	}
}

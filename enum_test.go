// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

type Shape interface{ area() float64 }

type Circle struct{ Radius float64 }

type Rect struct {
	_    struct{} `ron:",tuple"`
	W, H float64
}

type Point struct{}

type Label string

type Polygon struct{ Points []tupleStruct }

func (c Circle) area() float64   { return 3 * c.Radius * c.Radius }
func (r Rect) area() float64     { return r.W * r.H }
func (Point) area() float64      { return 0 }
func (Label) area() float64      { return 0 }
func (p *Polygon) area() float64 { return float64(len(p.Points)) }

type Color int

const (
	Red Color = iota
	Green
	Blue
)

func shapeRegistry() *Registry {
	r := NewRegistry()
	RegisterEnumIn[Shape](r,
		Variant[Circle]("Circle"),
		Variant[Rect]("Rect"),
		Variant[Point]("Point"),
		Variant[Label]("Label"),
		Variant[*Polygon]("Polygon"),
	)
	RegisterUnitEnumIn(r, map[Color]string{Red: "Red", Green: "Green", Blue: "Blue"})
	return r
}

type scene struct {
	Shapes []Shape
	Main   Shape
	None   Shape
	Color  Color
	Colors map[Color]int
	ByName map[string]Shape
}

func TestEnumRoundTrip(t *testing.T) {
	r := shapeRegistry()
	in := scene{
		Shapes: []Shape{Circle{1.5}, Rect{W: 2, H: 3}, Point{}, Label("hi"), &Polygon{[]tupleStruct{{X: 1, Y: 2}}}},
		Main:   Circle{2},
		Color:  Blue,
		Colors: map[Color]int{Green: 1},
		ByName: map[string]Shape{"p": Point{}},
	}
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetRegistry(r)
	enc.SetIndent("", "    ")
	if err := enc.Encode(in); err != nil {
		t.Fatal(err)
	}
	want := `(
    shapes: [
        Circle(
            radius: 1.5,
        ),
        Rect(2.0, 3.0),
        Point,
        Label("hi"),
        Polygon(
            points: [
                (1, 2),
            ],
        ),
    ],
    main: Circle(
        radius: 2.0,
    ),
    none: None,
    color: Blue,
    colors: {
        Green: 1,
    },
    by_name: {
        "p": Point,
    },
)
`
	if buf.String() != want {
		t.Errorf("got\n%s\nwant\n%s", buf.String(), want)
	}

	dec := NewDecoder(&buf)
	dec.SetRegistry(r)
	var out scene
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch\nin  %#v\nout %#v", in, out)
	}
}

func TestEnumDecodeForms(t *testing.T) {
	r := shapeRegistry()
	tests := []struct {
		src  string
		want Shape
	}{
		{"Circle(radius: 1.0)", Circle{1}},
		{"Circle((radius: 1.0))", Circle{1}}, // unwrap_variant_newtypes inverse
		{"Rect(1.0, 2.0)", Rect{W: 1, H: 2}},
		{"Point", Point{}},
		{"Point()", Point{}},
		{`"Point"`, Point{}},
		{`Label("x")`, Label("x")},
		{"Some(Point)", Point{}},
		{"None", nil},
	}
	for _, tt := range tests {
		dec := NewDecoder(strings.NewReader(tt.src))
		dec.SetRegistry(r)
		var got Shape
		if err := dec.Decode(&got); err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %#v, want %#v", tt.src, got, tt.want)
		}
	}
}

func TestEnumDecodeConcrete(t *testing.T) {
	r := shapeRegistry()
	dec := NewDecoder(strings.NewReader("Circle(radius: 4.0)"))
	dec.SetRegistry(r)
	var c Circle
	if err := dec.Decode(&c); err != nil {
		t.Fatal(err)
	}
	if c.Radius != 4 {
		t.Errorf("got %+v", c)
	}

	dec = NewDecoder(strings.NewReader(`Label("direct")`))
	dec.SetRegistry(r)
	var l Label
	if err := dec.Decode(&l); err != nil {
		t.Fatal(err)
	}
	if l != "direct" {
		t.Errorf("got %q", l)
	}
}

func TestEnumErrors(t *testing.T) {
	r := shapeRegistry()
	tests := []struct {
		src string
		ptr any
		msg string
	}{
		{"Triangle(1.0)", new(Shape), `unknown variant "Triangle"`},
		{"(radius: 1.0)", new(Shape), "expected an enum variant"},
		{"5", new(Shape), "expected an enum variant"},
		{"Circle(radius: \"x\")", new(Shape), "radius"},
		{"Purple", new(Color), `unknown variant "Purple"`},
		{"1", new(Color), "expected a unit variant"},
	}
	for _, tt := range tests {
		dec := NewDecoder(strings.NewReader(tt.src))
		dec.SetRegistry(r)
		err := dec.Decode(tt.ptr)
		if err == nil || !strings.Contains(err.Error(), tt.msg) {
			t.Errorf("%s: got %v, want error containing %q", tt.src, err, tt.msg)
		}
	}

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetRegistry(r)
	if err := enc.Encode(Color(99)); err == nil || !strings.Contains(err.Error(), "no registered name") {
		t.Errorf("unknown unit value: got %v", err)
	}
}

func TestRegisterPanics(t *testing.T) {
	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		f()
	}
	r := NewRegistry()
	mustPanic("not interface", func() { RegisterEnumIn[Circle](r, Variant[Circle]("C")) })
	mustPanic("not implementing", func() { RegisterEnumIn[Shape](r, Variant[int]("I")) })
	mustPanic("pointer receiver needs pointer", func() { RegisterEnumIn[Shape](r, Variant[Polygon]("P")) })
	mustPanic("invalid name", func() { RegisterEnumIn[Shape](r, Variant[Circle]("bad name")) })
	mustPanic("zero spec", func() { RegisterEnumIn[Shape](r, VariantSpec{}) })
	mustPanic("interface variant", func() { RegisterEnumIn[any](r, Variant[Shape]("S")) })
	mustPanic("duplicate name", func() {
		RegisterEnumIn[Shape](r, Variant[Circle]("X"), Variant[Rect]("X"))
	})
	mustPanic("duplicate type", func() {
		RegisterEnumIn[Shape](r, Variant[Circle]("A"), Variant[Circle]("B"))
	})
	mustPanic("unit invalid name", func() { RegisterUnitEnumIn(r, map[Color]string{Red: ""}) })
	mustPanic("unit duplicate", func() { RegisterUnitEnumIn(r, map[Color]string{Red: "A", Blue: "A"}) })
}

type ambiguousA interface{ a() }
type ambiguousB interface{ b() }
type both struct{ V int }

func (both) a() {}
func (both) b() {}

func TestVariantInSeveralEnums(t *testing.T) {
	r := NewRegistry()
	RegisterEnumIn[ambiguousA](r, Variant[both]("InA"))
	RegisterEnumIn[ambiguousB](r, Variant[both]("InB"))

	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetRegistry(r)
	v := struct {
		A ambiguousA
		B ambiguousB
		C any
	}{both{1}, both{2}, both{3}}
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	// The static interface type selects the name; without one the type is
	// ambiguous and encodes as a plain struct.
	want := "(a:InA(v:1),b:InB(v:2),c:(v:3))\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestDefaultRegistry(t *testing.T) {
	type defaultShape interface{ isDefault() }
	RegisterEnum[defaultShape](Variant[defaultVariant]("DefaultVariant"))
	RegisterUnitEnum(map[defaultUnit]string{1: "One"})

	out, err := Marshal(struct {
		S defaultShape
		U defaultUnit
	}{defaultVariant{}, 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "(s:DefaultVariant,u:One)" {
		t.Errorf("got %s", out)
	}
	var back struct {
		S defaultShape
		U defaultUnit
	}
	if err := Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.S != (defaultVariant{}) || back.U != 1 {
		t.Errorf("got %+v", back)
	}
}

type defaultVariant struct{}

func (defaultVariant) isDefault() {}

type defaultUnit uint8

// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"math"
	"math/big"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// These types mirror the serde fixture in compat/ronrs/src/fixture.rs.

type fxShape interface{ fxShape() }

type fxEmpty struct{}

type fxRadius float32

type fxRect struct {
	_ struct{} `ron:",tuple"`
	A uint32
	B uint32
}

type fxPoly struct {
	Sides uint8
	Name  string
}

func (fxEmpty) fxShape()  {}
func (fxRadius) fxShape() {}
func (fxRect) fxShape()   {}
func (fxPoly) fxShape()   {}

type fxMeters struct {
	_ struct{} `ron:"Meters,tuple"`
	V float64
}

type fxPair struct {
	_ struct{} `ron:"Pair,tuple"`
	A int32
	B string
}

type fxMarker struct {
	_ struct{} `ron:"Marker"`
}

type fxInner struct {
	_ struct{} `ron:"Inner"`
	X int32
	Y int32
}

type fxFixture struct {
	_              struct{} `ron:"Fixture"`
	Name           string
	Count          uint64
	Neg            int64
	Big            *big.Int
	UBig           *big.Int `ron:"ubig"`
	Ratio          float32
	Exact          float64
	Flag           bool
	Letter         Value
	Text           string
	Blob           []byte
	Meters         fxMeters
	Pair           fxPair
	Marker         fxMarker
	Inner          fxInner
	Tuple          Value
	Unit           Value
	Shapes         []fxShape
	Some           *int32
	None           *int32
	NestedSome     **int32
	NestedNone     **int32
	NestedSomeNone **int32
	List           []int32
	Empty          []int32
	Map            map[string]int32
	IntMap         map[uint8][]string
}

func fxRegistry() *Registry {
	r := NewRegistry()
	RegisterEnumIn[fxShape](r,
		Variant[fxEmpty]("Empty"),
		Variant[fxRadius]("Radius"),
		Variant[fxRect]("Rect"),
		Variant[fxPoly]("Poly"),
	)
	return r
}

func fxExpected() fxFixture {
	big1, _ := new(big.Int).SetString("-170141183460469231731687303715884105728", 10)
	big2, _ := new(big.Int).SetString("340282366920938463463374607431768211455", 10)
	six := int32(6)
	sixp := &six
	five := int32(5)
	var nilp *int32
	return fxFixture{
		Name:   "fixture",
		Count:  math.MaxUint64,
		Neg:    math.MinInt64,
		Big:    big1,
		UBig:   big2,
		Ratio:  0.1,
		Exact:  -1.5e300,
		Flag:   true,
		Letter: Char('\U0001F600'),
		Text:   "quote \" backslash \\ newline \n tab \t nul \x00 unicode é中\U0001F600 bell \a del \x7f",
		Blob:   []byte{0, 1, 2, 0x7f, 0x80, 0xff},
		Meters: fxMeters{V: 2.5},
		Pair:   fxPair{A: 7, B: "seven"},
		Inner:  fxInner{X: 1, Y: -2},
		Tuple:  Tuple{Values: []Value{NewInt(3), String("three"), Bool(false)}},
		Unit:   Unit{},
		Shapes: []fxShape{fxEmpty{}, fxRadius(1.5), fxRect{A: 3, B: 4}, fxPoly{Sides: 5, Name: "penta"}},
		Some:   &five,
		// The nested options are Some(Some(6)), None and Some(None).
		NestedSome:     &sixp,
		NestedSomeNone: &nilp,
		List:           []int32{1, 2, 3},
		Empty:          []int32{},
		Map:            map[string]int32{"one": 1, "two": 2},
		IntMap:         map[uint8][]string{1: {"a", "b"}, 2: {}},
	}
}

func fxEqual(t *testing.T, got, want fxFixture) {
	t.Helper()
	if got.Big == nil || got.Big.Cmp(want.Big) != 0 || got.UBig == nil || got.UBig.Cmp(want.UBig) != 0 {
		t.Errorf("big integers differ: %v %v", got.Big, got.UBig)
	}
	got.Big, got.UBig, want.Big, want.UBig = nil, nil, nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fixture mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestCompatTypedFromRonRs(t *testing.T) {
	path := oraclePath(t)
	out, err := exec.Command(path, "fixture").Output()
	if err != nil {
		t.Fatal(err)
	}
	compact, pretty, ok := strings.Cut(string(out), "\n---\n")
	if !ok {
		t.Fatalf("unexpected fixture output %q", out)
	}
	for name, src := range map[string]string{"compact": compact, "pretty": pretty} {
		t.Run(name, func(t *testing.T) {
			var got fxFixture
			dec := NewDecoder(strings.NewReader(src))
			dec.SetRegistry(fxRegistry())
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("decode: %v\n%s", err, src)
			}
			fxEqual(t, got, fxExpected())
		})
	}
}

func TestCompatTypedToRonRs(t *testing.T) {
	path := oraclePath(t)
	want := fxExpected()
	for _, mode := range []struct {
		name   string
		indent string
		names  bool
	}{
		{"compact", "", false},
		{"pretty", "    ", false},
		{"compact_struct_names", "", true},
		{"pretty_struct_names", "    ", true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			var buf bytes.Buffer
			enc := NewEncoder(&buf)
			enc.SetRegistry(fxRegistry())
			enc.SetStructNames(mode.names)
			if mode.indent != "" {
				enc.SetIndent("", mode.indent)
			}
			if err := enc.Encode(want); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(path, "check")
			cmd.Stdin = bytes.NewReader(buf.Bytes())
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("ron-rs rejected the output: %v\n%s\n%s", err, stderr.String(), buf.String())
			}
		})
	}
}

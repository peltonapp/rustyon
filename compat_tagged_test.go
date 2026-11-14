// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// These types mirror compat/ronrs/src/tagged.rs: serde enums with the
// internally tagged, adjacently tagged and untagged representations.

type tgPoint struct{ X, Y int32 }

type tgInternal interface{ tgInternal() }

type tgEmpty struct{}
type tgCircle struct{ Radius float64 }
type tgPoly struct {
	Sides uint8
	Name  string
}
type tgWrapped tgPoint // a newtype variant around a struct

func (tgEmpty) tgInternal()   {}
func (tgCircle) tgInternal()  {}
func (tgPoly) tgInternal()    {}
func (tgWrapped) tgInternal() {}

type tgAdjacent interface{ tgAdjacent() }

type tgARect struct {
	_ struct{} `ron:",tuple"`
	A uint32
	B uint32
}
type tgALabel string

func (tgEmpty) tgAdjacent()   {}
func (tgCircle) tgAdjacent()  {}
func (tgARect) tgAdjacent()   {}
func (tgALabel) tgAdjacent()  {}
func (tgWrapped) tgAdjacent() {}

type tgUntagged interface{ tgUntagged() }

type tgUCount int64

func (tgCircle) tgUntagged() {}
func (tgPoly) tgUntagged()   {}
func (tgARect) tgUntagged()  {}
func (tgALabel) tgUntagged() {}
func (tgUCount) tgUntagged() {}

type tgTagged struct {
	_        struct{} `ron:"Tagged"`
	Internal []tgInternal
	Adjacent []tgAdjacent
	Untagged []tgUntagged
}

func tgRegistry() *Registry {
	r := NewRegistry()
	RegisterEnumWithIn[tgInternal](r, EnumOptions{Tag: "type"},
		Variant[tgEmpty]("Empty"),
		Variant[tgCircle]("Circle"),
		Variant[tgPoly]("Poly"),
		Variant[tgWrapped]("Wrapped"),
	)
	RegisterEnumWithIn[tgAdjacent](r, EnumOptions{Tag: "t", Content: "c"},
		Variant[tgEmpty]("Empty"),
		Variant[tgCircle]("Circle"),
		Variant[tgARect]("Rect"),
		Variant[tgALabel]("Label"),
		Variant[tgWrapped]("Wrapped"),
	)
	RegisterEnumWithIn[tgUntagged](r, EnumOptions{Untagged: true},
		Variant[tgCircle]("Circle"),
		Variant[tgPoly]("Poly"),
		Variant[tgARect]("Rect"),
		Variant[tgALabel]("Label"),
		Variant[tgUCount]("Count"),
	)
	return r
}

func tgExpected() tgTagged {
	return tgTagged{
		Internal: []tgInternal{tgEmpty{}, tgCircle{1.5}, tgPoly{5, "penta"}, tgWrapped{1, -2}},
		Adjacent: []tgAdjacent{tgEmpty{}, tgCircle{2.5}, tgARect{A: 3, B: 4}, tgALabel("lbl"), tgWrapped{5, 6}},
		Untagged: []tgUntagged{tgCircle{3.5}, tgPoly{6, "hexa"}, tgARect{A: 7, B: 8}, tgALabel("u"), tgUCount(-9)},
	}
}

func TestCompatTaggedFromRonRs(t *testing.T) {
	out, err := exec.Command(oraclePath(t), "tagged").Output()
	if err != nil {
		t.Fatal(err)
	}
	compact, pretty, ok := strings.Cut(string(out), "\n---\n")
	if !ok {
		t.Fatalf("unexpected output %q", out)
	}
	for name, src := range map[string]string{"compact": compact, "pretty": pretty} {
		t.Run(name, func(t *testing.T) {
			var got tgTagged
			if err := UnmarshalWith([]byte(src), &got, DecodeOptions{Registry: tgRegistry()}); err != nil {
				t.Fatalf("decode: %v\n%s", err, src)
			}
			if !reflect.DeepEqual(got, tgExpected()) {
				t.Errorf("mismatch\n got: %#v\nwant: %#v", got, tgExpected())
			}
			// The tree decoder agrees.
			var viaTree tgTagged
			if err := unmarshalTree([]byte(src), &viaTree, &decodeOptions{registry: tgRegistry()}); err != nil || !reflect.DeepEqual(viaTree, got) {
				t.Errorf("tree decode: %v", err)
			}
		})
	}
	// The compact encoding is the very text ron-rs writes.
	enc, err := MarshalWith(tgExpected(), EncodeOptions{Registry: tgRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != strings.TrimSpace(compact) {
		t.Errorf("compact output differs\n got: %s\nwant: %s", enc, compact)
	}
}

func TestCompatTaggedToRonRs(t *testing.T) {
	path := oraclePath(t)
	for _, mode := range []struct {
		name   string
		pretty *PrettyConfig
		names  bool
	}{
		{"compact", nil, false},
		{"pretty", &PrettyConfig{}, false},
		{"struct_names", nil, true},
		{"layout", &PrettyConfig{CompactArrays: true, SeparateTupleMembers: true, EscapeNonASCII: true}, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			out, err := MarshalWith(tgExpected(), EncodeOptions{Registry: tgRegistry(), StructNames: mode.names, Pretty: mode.pretty})
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(path, "tagged-check")
			cmd.Stdin = bytes.NewReader(out)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("ron-rs rejected the output: %v\n%s\n%s", err, stderr.String(), out)
			}
		})
	}
}

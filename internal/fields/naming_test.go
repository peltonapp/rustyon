// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package fields

import (
	"reflect"
	"testing"
)

func TestNamingApply(t *testing.T) {
	tests := []struct {
		in   string
		want [NumNamings]string
	}{
		// snake, camel, pascal, SCREAMING_SNAKE, kebab, SCREAMING-KEBAB, lower, upper, identity
		{"MaxHP", [NumNamings]string{"max_hp", "maxHp", "MaxHp", "MAX_HP", "max-hp", "MAX-HP", "maxhp", "MAXHP", "MaxHP"}},
		{"HTTPServer", [NumNamings]string{"http_server", "httpServer", "HttpServer", "HTTP_SERVER", "http-server", "HTTP-SERVER", "httpserver", "HTTPSERVER", "HTTPServer"}},
		{"Name", [NumNamings]string{"name", "name", "Name", "NAME", "name", "NAME", "name", "NAME", "Name"}},
		{"Vec2D", [NumNamings]string{"vec2_d", "vec2D", "Vec2D", "VEC2_D", "vec2-d", "VEC2-D", "vec2d", "VEC2D", "Vec2D"}},
		{"X", [NumNamings]string{"x", "x", "X", "X", "x", "X", "x", "X", "X"}},
	}
	for _, tc := range tests {
		for n := range NumNamings {
			if got := n.Apply(tc.in); got != tc.want[n] {
				t.Errorf("%v.Apply(%q) = %q, want %q", n, tc.in, got, tc.want[n])
			}
		}
	}
}

func TestParseNaming(t *testing.T) {
	for n := range NumNamings {
		got, ok := ParseNaming(n.String())
		if !ok || got != n {
			t.Errorf("ParseNaming(%q) = %v, %v", n.String(), got, ok)
		}
	}
	if _, ok := ParseNaming("nope"); ok {
		t.Error("unknown name accepted")
	}
	if Naming(200).String() != "Naming(200)" {
		t.Error(Naming(200).String())
	}
}

type renamed struct {
	_      struct{} `ron:",rename_all=camelCase"`
	MaxHP  int
	Tagged int `ron:"Keep_Me"`
	sub    int //nolint:unused // unexported
	Embed
	Plain
}

type Embed struct {
	_       struct{} `ron:",rename_all=SCREAMING_SNAKE_CASE"`
	InEmbed int
}

type Plain struct{ InPlain int }

func TestForNaming(t *testing.T) {
	names := func(si *Info) []string {
		var out []string
		for _, f := range si.Fields {
			out = append(out, f.Name)
		}
		return out
	}
	// The struct's own rename_all wins over the default strategy; the
	// embedded struct keeps its own, one without inherits the outer one.
	want := []string{"maxHp", "Keep_Me", "IN_EMBED", "inPlain"}
	for _, def := range []Naming{Snake, Pascal} {
		if got := names(ForNaming(reflect.TypeFor[renamed](), def)); !reflect.DeepEqual(got, want) {
			t.Errorf("def %v: %v, want %v", def, got, want)
		}
	}
	type S struct{ MaxHP int }
	if got := names(ForNaming(reflect.TypeFor[S](), Kebab)); !reflect.DeepEqual(got, []string{"max-hp"}) {
		t.Errorf("kebab: %v", got)
	}
	if got := names(For(reflect.TypeFor[S]())); !reflect.DeepEqual(got, []string{"max_hp"}) {
		t.Errorf("default: %v", got)
	}
	if ForNaming(reflect.TypeFor[S](), Kebab) == For(reflect.TypeFor[S]()) {
		t.Error("cache must key on the naming strategy")
	}
	// Out of range strategies fall back to the default.
	if ForNaming(reflect.TypeFor[S](), 99) != For(reflect.TypeFor[S]()) {
		t.Error("invalid naming should select snake_case")
	}
}

// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package path

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want []Step
	}{
		{"", nil},
		{".", nil},
		{"a", []Step{{Kind: Name, Name: "a", Text: ".a"}}},
		{".a.b", []Step{{Kind: Name, Name: "a", Text: ".a"}, {Kind: Name, Name: "b", Text: ".b"}}},
		{"items[0].name", []Step{
			{Kind: Name, Name: "items", Text: ".items"},
			{Kind: Index, Index: 0, Text: "[0]"},
			{Kind: Name, Name: "name", Text: ".name"},
		}},
		{"tags[-1]", []Step{{Kind: Name, Name: "tags", Text: ".tags"}, {Kind: Index, Index: -1, Text: "[-1]"}}},
		{`m["a.b"]`, []Step{{Kind: Name, Name: "m", Text: ".m"}, {Kind: Key, Name: "a.b", Text: `["a.b"]`}}},
		{`[0][1]`, []Step{{Kind: Index, Index: 0, Text: "[0]"}, {Kind: Index, Index: 1, Text: "[1]"}}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.in, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("Parse(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("Parse(%q)[%d] = %v, want %v", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"a..b", "a.", "a[", "a[x]", `a["x]`, `a["x"`, "a]", `a["\q"]`, "a[0]b"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): expected error", in)
		}
	}
}

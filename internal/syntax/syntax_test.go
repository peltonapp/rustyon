// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package syntax

import (
	"testing"
)

func lexAll(t *testing.T, src string) []Token {
	t.Helper()
	l := NewLexer([]byte(src))
	var toks []Token
	for {
		tok, err := l.Next()
		if err != nil {
			t.Fatalf("lex %q: %v", src, err)
		}
		toks = append(toks, tok)
		if tok.Kind == EOF {
			return toks
		}
	}
}

func TestLexerTokens(t *testing.T) {
	toks := lexAll(t, "Point( // c\n  x: -0x1Fu8, /* b */ r#type: r\"s\")")
	want := []Kind{Ident, LParen, LineComment, Ident, Colon, Int, Comma, BlockComment, Ident, Colon, String, RParen, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(want))
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: %v, want %v", i, toks[i].Kind, k)
		}
	}
	src := "Point( // c\n  x: -0x1Fu8, /* b */ r#type: r\"s\")"
	if p := ResolvePosition(src, toks[3].Pos.Offset); toks[3].Newlines != 1 || p.Line != 2 || p.Column != 3 {
		t.Errorf("x position: %+v newlines %d", p, toks[3].Newlines)
	}
	if p := ResolvePosition("\xEF\xBB\xBFab", 4); p.Line != 1 || p.Column != 2 {
		t.Errorf("BOM position: %+v", p)
	}
	if toks[5].Str != "-0x1F" || toks[5].Suffix != "u8" {
		t.Errorf("int token: %+v", toks[5])
	}
	if !toks[8].RawIdent || toks[8].Str != "type" || toks[8].Raw != "r#type" {
		t.Errorf("raw ident: %+v", toks[8])
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		src   string
		small int64
		big   string
	}{
		{"0", 0, ""},
		{"-9223372036854775808", -9223372036854775808, ""},
		{"9223372036854775808", 0, "9223372036854775808"},
		{"0xffffffffffffffffff", 0, "4722366482869645213695"},
		{"1i128", 1, ""},
	}
	for _, tt := range tests {
		tok := lexAll(t, tt.src)[0]
		small, b, err := ParseInt(tok)
		if err != nil {
			t.Errorf("%s: %v", tt.src, err)
			continue
		}
		if tt.big == "" {
			if b != nil || small != tt.small {
				t.Errorf("%s: got %d %v, want %d", tt.src, small, b, tt.small)
			}
		} else if b == nil || b.String() != tt.big {
			t.Errorf("%s: got %v, want %s", tt.src, b, tt.big)
		}
	}
}

func TestExtensionBit(t *testing.T) {
	if b, ok := ExtensionBit("unwrap_newtypes"); !ok || b != 1 {
		t.Errorf("got %d %v", b, ok)
	}
	if _, ok := ExtensionBit("nope"); ok {
		t.Error("unknown extension accepted")
	}
}

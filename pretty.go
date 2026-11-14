// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"fmt"
	"strings"
)

// PrettyConfig controls the layout of pretty printed output. It mirrors the
// options of ron-rs's PrettyConfig that make sense for Go. The zero value
// gives the layout of [MarshalIndent] with four spaces: one element per line,
// trailing commas, ": " after field names.
//
// Whatever the configuration, the output parses back to the same value.
// Because of that, Prefix, Indent, NewLine and Separator must consist of
// spaces, tabs and line breaks only.
//
// A collection that is written on one line ("inline") is written on one line
// together with everything in it, with ", " between elements.
type PrettyConfig struct {
	// Prefix starts every line after the first. It is the prefix of
	// [MarshalIndent].
	Prefix string
	// Indent is written once per nesting level. The default is four spaces.
	Indent string
	// NewLine separates lines. The default is "\n". With EnumerateArrays it
	// must contain a line break.
	NewLine string
	// Separator is written after the colon of a field or map entry. The
	// default is a single space.
	Separator string

	// DepthLimit limits the pretty printing depth: collections nested more
	// than DepthLimit levels deep are written inline. With DepthLimit 1 only
	// the outermost collection is spread over lines. Zero means no limit.
	DepthLimit int

	// CompactArrays writes lists on one line, as [1, 2, 3].
	CompactArrays bool
	// CompactStructs writes structs on one line, as (a: 1, b: 2).
	CompactStructs bool
	// CompactMaps writes maps on one line, as {"a": 1, "b": 2}.
	CompactMaps bool
	// SeparateTupleMembers puts every member of a tuple on its own line.
	// Without it, tuples of simple values stay on one line.
	SeparateTupleMembers bool
	// EnumerateArrays writes a comment with the index, such as // [0], before
	// every element of a list that is spread over several lines.
	EnumerateArrays bool
	// EscapeNonASCII writes every non-ASCII character in strings and chars
	// as a \u{...} escape.
	EscapeNonASCII bool
}

func isLayoutWhitespace(s string) bool {
	for i := range len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return true
}

func (c *PrettyConfig) validate() error {
	for _, f := range []struct{ name, val string }{
		{"Prefix", c.Prefix}, {"Indent", c.Indent}, {"NewLine", c.NewLine}, {"Separator", c.Separator},
	} {
		if !isLayoutWhitespace(f.val) {
			return fmt.Errorf("rustyon: PrettyConfig.%s must consist of whitespace, got %q", f.name, f.val)
		}
	}
	if c.EnumerateArrays && c.NewLine != "" && !strings.ContainsAny(c.NewLine, "\r\n") {
		return fmt.Errorf("rustyon: PrettyConfig.NewLine must contain a line break with EnumerateArrays, got %q", c.NewLine)
	}
	if c.DepthLimit < 0 {
		return fmt.Errorf("rustyon: PrettyConfig.DepthLimit must not be negative")
	}
	return nil
}

// MarshalWithConfig is like [Marshal] but writes pretty printed output laid
// out as cfg says.
func MarshalWithConfig(v any, cfg PrettyConfig) ([]byte, error) {
	return MarshalWith(v, EncodeOptions{Pretty: &cfg})
}

// SetPrettyConfig makes the encoder write pretty printed output laid out as
// cfg says. A nil cfg restores compact output. It replaces the setting of
// [Encoder.SetIndent].
func (enc *Encoder) SetPrettyConfig(cfg *PrettyConfig) {
	enc.opts.prefix, enc.opts.indent = "", ""
	enc.opts.pretty = nil
	if cfg != nil {
		c := *cfg
		enc.opts.pretty = &c
	}
}

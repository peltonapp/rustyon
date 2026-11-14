// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

// Format reformats a RON document in a canonical style while preserving
// comments, literal spellings (number bases, raw strings, escapes) and the
// author's choice between single line and multi line collections:
//
//   - A collection whose contents span several lines in the input, or that
//     contains comments, is written with one element per line, indented by
//     four spaces, with trailing commas.
//   - A collection written on one line stays on one line, with ", " between
//     elements and ": " after keys.
//   - A single nested collection inside a tuple, such as Some((...)), hugs
//     its parent's parentheses.
//   - Single blank lines between elements are kept; longer runs are
//     collapsed.
//
// Format accepts exactly the documents [Parse] accepts, and the result has
// the same value as the input. Format is idempotent. It returns a
// [*SyntaxError] if src is not valid RON.
func Format(src []byte) ([]byte, error) {
	return FormatIndent(src, "    ")
}

// FormatIndent is like [Format] but uses indent for each nesting level.
func FormatIndent(src []byte, indent string) ([]byte, error) {
	d, _, err := buildTree(src)
	if err != nil {
		return nil, err
	}
	return formatTree(d, len(src), indent), nil
}

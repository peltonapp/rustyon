// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Package path parses the path syntax that selects a value inside a RON
// document. The rustyon command line tool and the editable document API of
// the root package share it.
//
// The syntax is:
//
//	path    = [ "." ] [ name ] { "." name | "[" index "]" | "[" string "]" }
//	name    = one or more characters other than "." "[" and "]"
//	index   = [ "-" ] digits
//	string  = a double quoted string with Go escapes
//
// Examples: items[0].name, .config.port, players["alice"], tags[-1].
// An empty path or "." selects the whole document.
package path

import (
	"fmt"
	"strconv"
	"strings"
)

// Kind is the kind of a path step.
type Kind int

// Step kinds.
const (
	// Name is .name: a struct field, or a map entry whose key is the
	// string or identifier name.
	Name Kind = iota
	// Index is [N]: a list or tuple element (negative N counts from the
	// end), or a map entry whose key is the integer N.
	Index
	// Key is ["key"]: a map entry with that string key, or an identifier
	// key of the same name, or a struct field.
	Key
)

// Step is one step of a path.
type Step struct {
	Kind  Kind
	Name  string // Name and Key
	Index int    // Index
	Text  string // the step as written, for messages
}

// Parse splits a path into steps. The empty path and "." have no steps.
func Parse(path string) ([]Step, error) {
	var steps []Step
	i := 0
	if strings.HasPrefix(path, ".") {
		i = 1
	}
	first := true
	for i < len(path) {
		switch {
		case path[i] == '[':
			s, next, err := parseBracket(path, i)
			if err != nil {
				return nil, err
			}
			steps = append(steps, s)
			i = next
		case path[i] == '.' || first:
			if path[i] == '.' {
				i++
			}
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' && path[j] != ']' {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("invalid path %q: empty name at offset %d", path, i)
			}
			steps = append(steps, Step{Kind: Name, Name: path[i:j], Text: "." + path[i:j]})
			i = j
		default:
			return nil, fmt.Errorf("invalid path %q: unexpected %q at offset %d", path, path[i], i)
		}
		first = false
	}
	return steps, nil
}

// parseBracket parses "[...]" starting at path[at] and returns the step and
// the offset after the closing bracket.
func parseBracket(path string, at int) (Step, int, error) {
	rest := path[at+1:]
	if strings.HasPrefix(rest, `"`) {
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			return Step{}, 0, fmt.Errorf("invalid path %q: unterminated string at offset %d", path, at+1)
		}
		key, err := strconv.Unquote(quoted)
		if err != nil {
			return Step{}, 0, fmt.Errorf("invalid path %q: bad string at offset %d", path, at+1)
		}
		end := at + 1 + len(quoted)
		if end >= len(path) || path[end] != ']' {
			return Step{}, 0, fmt.Errorf("invalid path %q: missing ] at offset %d", path, end)
		}
		return Step{Kind: Key, Name: key, Text: "[" + quoted + "]"}, end + 1, nil
	}
	closing := strings.IndexByte(rest, ']')
	if closing < 0 {
		return Step{}, 0, fmt.Errorf("invalid path %q: missing ] after offset %d", path, at)
	}
	n, err := strconv.Atoi(rest[:closing])
	if err != nil {
		return Step{}, 0, fmt.Errorf("invalid path %q: %q is not an integer or a quoted string", path, rest[:closing])
	}
	return Step{Kind: Index, Index: n, Text: "[" + rest[:closing] + "]"}, at + 1 + closing + 1, nil
}

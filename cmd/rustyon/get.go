// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/peltonapp/rustyon-go"
)

// A path selects a value inside a RON document. The syntax is:
//
//	path    = [ "." ] [ name ] { "." name | "[" index "]" | "[" string "]" }
//	name    = one or more characters other than "." "[" and "]"
//	index   = [ "-" ] digits
//	string  = a double quoted string with Go escapes
//
// Examples: items[0].name, .config.port, players["alice"], tags[-1].
// An empty path or "." selects the whole document.
//
// Steps apply to values like this:
//
//	.name     struct field; also a map entry whose key is the string or
//	          identifier name
//	[N]       list or tuple element (negative N counts from the end); also a
//	          map entry whose key is the integer N
//	["key"]   map entry with that string key, or an identifier key of the
//	          same name; also a struct field
//
// Some(x) is looked through, so a step applies to x. None has nothing to
// step into.

type stepKind int

const (
	stepName stepKind = iota
	stepIndex
	stepKey
)

type step struct {
	kind stepKind
	name string // stepName and stepKey
	idx  int    // stepIndex
	text string // the step as written, for messages
}

// parsePath splits a path into steps.
func parsePath(path string) ([]step, error) {
	var steps []step
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
			steps = append(steps, step{kind: stepName, name: path[i:j], text: "." + path[i:j]})
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
func parseBracket(path string, at int) (step, int, error) {
	rest := path[at+1:]
	if strings.HasPrefix(rest, `"`) {
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			return step{}, 0, fmt.Errorf("invalid path %q: unterminated string at offset %d", path, at+1)
		}
		key, err := strconv.Unquote(quoted)
		if err != nil {
			return step{}, 0, fmt.Errorf("invalid path %q: bad string at offset %d", path, at+1)
		}
		end := at + 1 + len(quoted)
		if end >= len(path) || path[end] != ']' {
			return step{}, 0, fmt.Errorf("invalid path %q: missing ] at offset %d", path, end)
		}
		return step{kind: stepKey, name: key, text: "[" + quoted + "]"}, end + 1, nil
	}
	closing := strings.IndexByte(rest, ']')
	if closing < 0 {
		return step{}, 0, fmt.Errorf("invalid path %q: missing ] after offset %d", path, at)
	}
	n, err := strconv.Atoi(rest[:closing])
	if err != nil {
		return step{}, 0, fmt.Errorf("invalid path %q: %q is not an integer or a quoted string", path, rest[:closing])
	}
	return step{kind: stepIndex, idx: n, text: "[" + rest[:closing] + "]"}, at + 1 + closing + 1, nil
}

// errNotFound is wrapped by lookup errors.
var errNotFound = errors.New("not found")

// lookup applies steps to v.
func lookup(v rustyon.Value, steps []step) (rustyon.Value, error) {
	walked := ""
	for _, s := range steps {
		next, err := apply(v, s)
		if err != nil {
			where := walked
			if where == "" {
				where = "."
			}
			return nil, fmt.Errorf("%w: %s%s: %w (at %s)", errNotFound, walked, s.text, err, where)
		}
		walked += s.text
		v = next
	}
	return v, nil
}

func apply(v rustyon.Value, s step) (rustyon.Value, error) {
	for {
		opt, ok := v.(rustyon.Option)
		if !ok {
			break
		}
		if opt.IsNone() {
			return nil, errors.New("value is None")
		}
		v = opt.Value
	}
	switch s.kind {
	case stepName, stepKey:
		switch v := v.(type) {
		case rustyon.Struct:
			if f, ok := v.Get(s.name); ok {
				return f, nil
			}
			return nil, fmt.Errorf("no field %q", s.name)
		case rustyon.Map:
			if e, ok := v.Get(rustyon.String(s.name)); ok {
				return e, nil
			}
			if e, ok := v.Get(rustyon.Ident(s.name)); ok {
				return e, nil
			}
			return nil, fmt.Errorf("no key %q", s.name)
		}
		return nil, fmt.Errorf("a %s has no field or key %q", v.Kind(), s.name)
	default:
		switch v := v.(type) {
		case rustyon.List:
			return element(v, s.idx)
		case rustyon.Tuple:
			return element(v.Values, s.idx)
		case rustyon.Map:
			if e, ok := v.Get(rustyon.NewInt(int64(s.idx))); ok {
				return e, nil
			}
			return nil, fmt.Errorf("no key %d", s.idx)
		}
		return nil, fmt.Errorf("a %s cannot be indexed", v.Kind())
	}
}

func element(vs []rustyon.Value, idx int) (rustyon.Value, error) {
	i := idx
	if i < 0 {
		i += len(vs)
	}
	if i < 0 || i >= len(vs) {
		return nil, fmt.Errorf("index %d out of range, length %d", idx, len(vs))
	}
	return vs[i], nil
}

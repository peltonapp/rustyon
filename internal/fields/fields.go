// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Package fields computes and caches the RON view of Go struct types:
// field names, tag options and embedded struct promotion.
package fields

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// Info is the RON view of a Go struct type.
type Info struct {
	Name   string // RON struct name: from a `_` field tag or the Go type name
	Tuple  bool   // encode as a tuple struct
	Fields []Field
	byName map[string]int
	byFold map[string]int
}

// Field is a single encodable struct field.
type Field struct {
	Name      string
	Index     []int
	Type      reflect.Type
	OmitEmpty bool
	OmitZero  bool
}

// IsNewtype reports whether the struct is a tuple struct with one field.
func (si *Info) IsNewtype() bool { return si.Tuple && len(si.Fields) == 1 }

// Lookup finds a field by exact name, then case-insensitively.
func (si *Info) Lookup(name string) (*Field, bool) {
	idx, ok := si.LookupIndex(name)
	if !ok {
		return nil, false
	}
	return &si.Fields[idx], true
}

// LookupIndex is like Lookup but returns the index into Fields.
func (si *Info) LookupIndex(name string) (int, bool) {
	idx, ok := si.byName[name]
	if !ok {
		idx, ok = si.byFold[strings.ToLower(name)]
	}
	return idx, ok
}

var cache sync.Map // reflect.Type to *Info

// For returns the cached Info for a struct type.
func For(t reflect.Type) *Info {
	if si, ok := cache.Load(t); ok {
		return si.(*Info)
	}
	si, _ := cache.LoadOrStore(t, build(t))
	return si.(*Info)
}

func parseTag(tag string) (name string, opts []string) {
	name, rest, _ := strings.Cut(tag, ",")
	if rest != "" {
		opts = strings.Split(rest, ",")
	}
	return name, opts
}

func build(t reflect.Type) *Info {
	si := &Info{Name: t.Name(), byName: map[string]int{}, byFold: map[string]int{}}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Name != "_" {
			continue
		}
		name, opts := parseTag(f.Tag.Get("ron"))
		if name != "" {
			si.Name = name
		}
		si.Tuple = si.Tuple || slices.Contains(opts, "tuple")
	}

	type candidate struct {
		Field
		depth  int
		tagged bool
	}
	var cands []candidate
	var walk func(t reflect.Type, index []int, depth int, seen map[reflect.Type]bool)
	walk = func(t reflect.Type, index []int, depth int, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Name == "_" {
				continue
			}
			tag := f.Tag.Get("ron")
			if tag == "-" {
				continue
			}
			name, opts := parseTag(tag)
			idx := append(slices.Clone(index), i)
			if f.Anonymous && name == "" {
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					if !f.IsExported() && f.Type.Kind() == reflect.Pointer {
						continue
					}
					walk(ft, idx, depth+1, seen)
					continue
				}
			}
			if !f.IsExported() {
				continue
			}
			tagged := name != ""
			if !tagged {
				name = SnakeCase(f.Name)
			}
			cands = append(cands, candidate{
				Field: Field{
					Name:      name,
					Index:     idx,
					Type:      f.Type,
					OmitEmpty: slices.Contains(opts, "omitempty"),
					OmitZero:  slices.Contains(opts, "omitzero"),
				},
				depth:  depth,
				tagged: tagged,
			})
		}
	}
	walk(t, nil, 0, map[reflect.Type]bool{})

	// Resolve name conflicts like encoding/json: the shallowest field wins,
	// a tagged field wins over untagged ones at the same depth, and
	// remaining ties hide the name entirely.
	groups := map[string][]candidate{}
	for _, c := range cands {
		groups[c.Name] = append(groups[c.Name], c)
	}
	for _, c := range cands {
		if !dominant(c, groups[c.Name], func(x candidate) (int, bool) { return x.depth, x.tagged }) {
			continue
		}
		si.byName[c.Name] = len(si.Fields)
		fold := strings.ToLower(c.Name)
		if _, ok := si.byFold[fold]; !ok {
			si.byFold[fold] = len(si.Fields)
		}
		si.Fields = append(si.Fields, c.Field)
	}
	return si
}

// dominant reports whether c is the single winner of its name group.
func dominant[T any](c T, group []T, key func(T) (int, bool)) bool {
	minDepth := -1
	for _, g := range group {
		if d, _ := key(g); minDepth < 0 || d < minDepth {
			minDepth = d
		}
	}
	atMin, taggedAtMin := 0, 0
	for _, g := range group {
		if d, tagged := key(g); d == minDepth {
			atMin++
			if tagged {
				taggedAtMin++
			}
		}
	}
	cd, ctagged := key(c)
	if cd != minDepth {
		return false
	}
	if atMin == 1 {
		return true
	}
	return ctagged && taggedAtMin == 1
}

// SnakeCase converts a Go identifier such as MaxHP or HTTPServer to
// snake_case (max_hp, http_server).
func SnakeCase(s string) string {
	runes := []rune(s)
	var sb strings.Builder
	sb.Grow(len(s) + 4)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
					sb.WriteByte('_')
				}
			}
			sb.WriteRune(unicode.ToLower(r))
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// ByIndex returns the field at index, allocating nil embedded pointers when
// alloc is set. It returns an invalid Value when a nil embedded pointer is
// encountered and alloc is false.
func ByIndex(v reflect.Value, index []int, alloc bool) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !alloc {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}

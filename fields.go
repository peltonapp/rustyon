// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// structInfo is the cached RON view of a Go struct type.
type structInfo struct {
	name   string // RON struct name: from a `_` field tag or the Go type name
	tuple  bool   // encode as a tuple struct
	fields []fieldInfo
	byName map[string]int
	byFold map[string]int
}

type fieldInfo struct {
	name      string
	index     []int
	typ       reflect.Type
	omitEmpty bool
	omitZero  bool
}

var structCache sync.Map // reflect.Type to *structInfo

func cachedStructInfo(t reflect.Type) *structInfo {
	if si, ok := structCache.Load(t); ok {
		return si.(*structInfo)
	}
	si, _ := structCache.LoadOrStore(t, buildStructInfo(t))
	return si.(*structInfo)
}

func parseTag(tag string) (name string, opts []string) {
	name, rest, _ := strings.Cut(tag, ",")
	if rest != "" {
		opts = strings.Split(rest, ",")
	}
	return name, opts
}

func buildStructInfo(t reflect.Type) *structInfo {
	si := &structInfo{name: t.Name(), byName: map[string]int{}, byFold: map[string]int{}}
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Name != "_" {
			continue
		}
		name, opts := parseTag(f.Tag.Get("ron"))
		if name != "" {
			si.name = name
		}
		si.tuple = si.tuple || slices.Contains(opts, "tuple")
	}

	type candidate struct {
		fieldInfo
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
				name = snakeCase(f.Name)
			}
			cands = append(cands, candidate{
				fieldInfo: fieldInfo{
					name:      name,
					index:     idx,
					typ:       f.Type,
					omitEmpty: slices.Contains(opts, "omitempty"),
					omitZero:  slices.Contains(opts, "omitzero"),
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
	byName := map[string][]candidate{}
	for _, c := range cands {
		byName[c.name] = append(byName[c.name], c)
	}
	for _, c := range cands {
		group := byName[c.name]
		if !dominant(c, group, func(x candidate) (int, bool) { return x.depth, x.tagged }) {
			continue
		}
		si.byName[c.name] = len(si.fields)
		fold := strings.ToLower(c.name)
		if _, ok := si.byFold[fold]; !ok {
			si.byFold[fold] = len(si.fields)
		}
		si.fields = append(si.fields, c.fieldInfo)
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
	var atMin, taggedAtMin []T
	for _, g := range group {
		if d, tagged := key(g); d == minDepth {
			atMin = append(atMin, g)
			if tagged {
				taggedAtMin = append(taggedAtMin, g)
			}
		}
	}
	cd, ctagged := key(c)
	if cd != minDepth {
		return false
	}
	if len(atMin) == 1 {
		return true
	}
	return ctagged && len(taggedAtMin) == 1
}

// snakeCase converts a Go identifier such as MaxHP or HTTPServer to
// snake_case (max_hp, http_server).
func snakeCase(s string) string {
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

// fieldByIndex returns the field at index, allocating nil embedded pointers
// when alloc is set. It returns an invalid Value when a nil embedded pointer
// is encountered and alloc is false.
func fieldByIndex(v reflect.Value, index []int, alloc bool) reflect.Value {
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

// isNewtype reports whether a struct is a tuple struct with one field.
func (si *structInfo) isNewtype() bool { return si.tuple && len(si.fields) == 1 }

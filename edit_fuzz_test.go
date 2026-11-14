// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// addrPath is an addressable value of a document together with the path of
// its container and the last step of its own path.
type addrPath struct {
	path, parent, last string
	value              Value
}

// addressPaths lists paths that select values of v, looking through Some the
// way paths do.
func addressPaths(v Value) []addrPath {
	out := []addrPath{{path: "", value: v}}
	var walk func(v Value, path string, depth int)
	walk = func(v Value, path string, depth int) {
		if depth > 6 || len(out) > 200 {
			return
		}
		for {
			o, ok := v.(Option)
			if !ok || o.Value == nil {
				break
			}
			v = o.Value
		}
		add := func(step string, child Value) {
			p := path + step
			out = append(out, addrPath{path: p, parent: path, last: step, value: child})
			walk(child, p, depth+1)
		}
		switch v := v.(type) {
		case Struct:
			for _, f := range v.Fields {
				add("."+f.Name, f.Value)
			}
		case Tuple:
			for i, c := range v.Values {
				add("["+strconv.Itoa(i)+"]", c)
			}
		case List:
			for i, c := range v {
				add("["+strconv.Itoa(i)+"]", c)
			}
		case Map:
			for _, e := range v {
				switch k := e.Key.(type) {
				case String:
					add("["+strconv.Quote(string(k))+"]", e.Value)
				case Ident:
					add("."+string(k), e.Value)
				case Int:
					if n, ok := k.Int64(); ok {
						add("["+strconv.FormatInt(n, 10)+"]", e.Value)
					}
				}
			}
		}
	}
	walk(v, "", 0)
	return out
}

// FuzzEditable applies random edits to documents. Every successful edit
// must leave a document that parses, and the edited value must be visible.
// A failed edit must leave the document unchanged.
func FuzzEditable(f *testing.F) {
	ops := []byte{1, 0, 2, 3, 4, 0, 5, 1, 6, 2, 7, 3, 8, 4, 9, 5, 10, 0}
	for _, s := range fuzzSeeds {
		f.Add([]byte(s), ops)
	}
	files, _ := filepath.Glob("testdata/format/*.ron")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b, ops)
		}
	}
	f.Fuzz(func(t *testing.T, data, ops []byte) {
		e, err := ParseEditable(data)
		if err != nil {
			return
		}
		if _, err := Parse(data); err != nil {
			t.Fatalf("ParseEditable accepted input that Parse rejects: %q", data)
		}
		if len(ops) > 24 {
			ops = ops[:24]
		}
		for i := 0; i+1 < len(ops); i += 2 {
			cur, err := Parse(e.Bytes())
			if err != nil {
				t.Fatalf("document does not parse: %v\n%q", err, e.Bytes())
			}
			paths := addressPaths(cur)
			ap := paths[int(ops[i])%len(paths)]
			before := e.String()
			kind := ops[i+1] % 6
			var opErr error
			check := func() {}
			newVal := NewInt(4242)
			switch kind {
			case 0, 1:
				opErr = e.Set(ap.path, newVal)
				check = func() {
					if v, ok := e.Get(ap.path); !ok || !Equal(v, newVal) {
						t.Fatalf("Set(%q): value not visible, got %v %v\nbefore: %q\nafter: %q", ap.path, v, ok, before, e.String())
					}
				}
			case 2:
				name := ".zz_new"
				opErr = e.Set(ap.path+name, newVal)
				check = func() {
					if v, ok := e.Get(ap.path + name); !ok || !Equal(v, newVal) {
						t.Fatalf("Set(%q): new field not visible, got %v %v\nbefore: %q\nafter: %q", ap.path+name, v, ok, before, e.String())
					}
				}
			case 3:
				if ap.path == "" {
					continue
				}
				opErr = e.Delete(ap.path)
				check = func() {
					if strings.HasPrefix(ap.last, "[") && !strings.HasPrefix(ap.last, `["`) {
						return // list indexes shift
					}
					if v, ok := e.Get(ap.path); ok && Equal(v, ap.value) && !hasDuplicateKey(cur, ap) {
						t.Fatalf("Delete(%q): still visible\nbefore: %q\nafter: %q", ap.path, before, e.String())
					}
				}
			case 4:
				l, isList := ap.value.(List)
				if !isList {
					continue
				}
				opErr = e.Append(ap.path, newVal)
				check = func() {
					v, ok := e.Get(ap.path + "[-1]")
					if !ok || !Equal(v, newVal) {
						t.Fatalf("Append(%q): element not visible, got %v %v (list had %d)\nbefore: %q\nafter: %q", ap.path, v, ok, len(l), before, e.String())
					}
				}
			case 5:
				opErr = e.SetRON(ap.path, "0x2A")
				check = func() {
					if v, ok := e.Get(ap.path); !ok || !Equal(v, NewInt(42)) {
						t.Fatalf("SetRON(%q): value not visible, got %v %v\nbefore: %q\nafter: %q", ap.path, v, ok, before, e.String())
					}
				}
			}
			if opErr != nil {
				if e.String() != before {
					t.Fatalf("failed edit changed the document: %v\nbefore: %q\nafter: %q", opErr, before, e.String())
				}
				continue
			}
			if _, err := Parse(e.Bytes()); err != nil {
				t.Fatalf("edit produced an unparseable document: %v\nbefore: %q\nafter: %q", err, before, e.String())
			}
			check()
			formatted := e.Format("  ")
			v1, _ := Parse(e.Bytes())
			v2, err := Parse(formatted)
			if err != nil || !Equal(v1, v2) {
				t.Fatalf("formatting the edited document changed it: %v\n%q\n%q", err, e.String(), formatted)
			}
		}
	})
}

// hasDuplicateKey reports whether a map or struct on the way to ap has a
// second entry that the path would select after the first one is deleted.
func hasDuplicateKey(root Value, ap addrPath) bool {
	n := 0
	for _, other := range addressPaths(root) {
		if other.path == ap.path {
			n++
		}
	}
	return n > 1
}

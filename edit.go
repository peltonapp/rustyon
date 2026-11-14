// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	rpath "github.com/peltonapp/rustyon-go/internal/path"
)

// ErrPathNotFound is wrapped by the errors that [Editable] methods return
// when a path does not select anything in the document.
var ErrPathNotFound = errors.New("rustyon: path not found")

// Editable is a RON document that can be edited in place while keeping its
// comments and layout. It is created with [ParseEditable].
//
// # Paths
//
// Values are addressed with the same path syntax as the get command of the
// rustyon tool:
//
//	items[0].name   a field of the first element of a list
//	.config.port    a leading dot is optional
//	tags[-1]        negative indexes count from the end
//	users["alice"]  a map entry with a string key
//
// A path is a sequence of steps. A step .name selects a struct field, or a
// map entry whose key is the string or identifier name. A step [N] selects a
// list or tuple element, or a map entry with the integer key N. A step
// ["key"] selects a map entry with that string key, or a struct field. The
// empty path and "." select the whole document. Some(x) is looked through,
// so a step applies to x; None has nothing to step into. Map keys that are
// not strings, identifiers or integers cannot be addressed.
//
// # Edits
//
// An edit is a text change at the place the path selects, so everything
// else in the document is preserved byte for byte: comments, spelling of
// literals, blank lines and indentation. After every edit the result is
// parsed again; if an edit would produce a document that does not parse,
// the document is left unchanged and an error is returned. An Editable is
// not safe for concurrent use.
type Editable struct {
	src  string
	tree *fdoc
}

// ParseEditable parses src, which must be a valid RON document, for editing.
// It accepts exactly the documents [Parse] accepts.
func ParseEditable(src []byte) (*Editable, error) {
	d, p, err := buildTree(src)
	if err != nil {
		return nil, err
	}
	return &Editable{src: p.src, tree: d}, nil
}

// Bytes returns the current text of the document.
func (e *Editable) Bytes() []byte { return []byte(e.src) }

// String returns the current text of the document.
func (e *Editable) String() string { return e.src }

// Format returns the document reformatted like [FormatIndent] does, with
// all comments kept.
func (e *Editable) Format(indent string) []byte {
	return formatTree(e.tree, len(e.src), indent)
}

// Extensions returns the extensions enabled by the document's attributes.
func (e *Editable) Extensions() Extensions { return e.tree.exts }

// Get returns the value that path selects. It reports false if the path is
// malformed or selects nothing. The value is parsed from the current text,
// so it reflects all edits made so far.
func (e *Editable) Get(path string) (Value, bool) {
	l, err := e.locate(path)
	if err != nil || l.node == nil {
		return nil, false
	}
	v, err := e.valueOf(l.node)
	if err != nil {
		return nil, false
	}
	return v, true
}

// Set replaces the value that path selects with v. The comments around the
// old value stay where they are; comments inside the old value go with it.
// The new value is written on one line.
//
// If the path ends in a name or key that does not exist, it is added: as a
// new field at the end of a struct (or of an empty () or Name()), or as a new
// entry at the end of a map. A new map key is written as an identifier if
// the existing keys are identifiers and as a string otherwise; a step [N]
// adds the integer key N. A new element cannot be added with Set to a list
// or tuple, use [Editable.Insert] or [Editable.Append] for lists.
//
// Every parent of the last step must exist, otherwise Set returns an error
// that wraps [ErrPathNotFound]. Set with an empty path replaces the whole
// document value. The path addresses the node itself: setting a field whose
// value is Some(1) replaces all of Some(1).
func (e *Editable) Set(path string, v Value) error {
	text, err := renderValue(v)
	if err != nil {
		return err
	}
	return e.set(path, text)
}

// SetRON is like [Editable.Set] but takes the new value as RON text. The text
// must be exactly one value, with no surrounding comments, and is inserted
// as written, including any line breaks. It must be valid where it is
// inserted, for example Some 1 is only valid in a document that enables
// implicit_some.
func (e *Editable) SetRON(path, ron string) error {
	text := strings.TrimSpace(ron)
	p := &parser{src: text, exts: e.tree.exts, limit: recursionLimit}
	if _, err := p.parseValue(); err != nil {
		return fmt.Errorf("rustyon: invalid value %q: %w", ron, p.wrap(err))
	}
	if p.cur != len(text) {
		return fmt.Errorf("rustyon: invalid value %q: expected a single value", ron)
	}
	return e.set(path, text)
}

func (e *Editable) set(path, text string) error {
	l, err := e.locate(path)
	if err != nil {
		return err
	}
	if l.node != nil {
		return e.apply(splice{int(l.node.start), int(l.node.end - l.node.start), text})
	}
	// A missing step: add a field or a map entry.
	c := l.parent.c
	var item string
	switch l.kind {
	case ckStruct, ckEmpty:
		if l.step.Kind == rpath.Index {
			return fmt.Errorf("%w: %s: cannot add an element with a path index", ErrPathNotFound, l.step.Text)
		}
		if !isRawIdent(l.step.Name) {
			return fmt.Errorf("rustyon: %q is not a valid field name", l.step.Name)
		}
		item = formatIdent(l.step.Name) + ": " + text
	case ckMap:
		item = e.newMapKey(l) + ": " + text
	default:
		return fmt.Errorf("%w: %s", ErrPathNotFound, l.step.Text)
	}
	return e.apply(e.insertItem(c, len(c.items), item)...)
}

// Delete removes the value that path selects together with its field name
// or map key. The comments directly above the element (without a blank line
// in between) and the comments after it on its line are removed with it; all
// other comments stay. Deleting the last remaining element leaves an empty
// collection. Delete returns an error that wraps [ErrPathNotFound] if the
// path selects nothing, and an error if the path selects the whole document.
func (e *Editable) Delete(path string) error {
	l, err := e.locate(path)
	if err != nil {
		return err
	}
	if l.parent == nil {
		return errors.New("rustyon: cannot delete the document value")
	}
	if l.node == nil {
		return fmt.Errorf("%w: %s", ErrPathNotFound, l.step.Text)
	}
	return e.apply(e.deleteItem(l.parent.c, l.item)...)
}

// Insert inserts v as element i of the list that path selects, so that it
// ends up at index i. i must be between 0 and the length of the list. The
// new element follows the layout of its neighbors: on its own line if the
// elements are written one per line, otherwise inline. Comments stay with
// the elements they belong to.
func (e *Editable) Insert(path string, i int, v Value) error {
	text, err := renderValue(v)
	if err != nil {
		return err
	}
	l, err := e.locate(path)
	if err != nil {
		return err
	}
	if l.node == nil {
		return fmt.Errorf("%w: %s", ErrPathNotFound, l.step.Text)
	}
	cont, err := unwrapSome(l.node)
	if err != nil {
		return err
	}
	if cont.kindOf() != ckList {
		return fmt.Errorf("rustyon: %s is not a list", path)
	}
	c := cont.c
	if i < 0 || i > len(c.items) {
		return fmt.Errorf("rustyon: index %d out of range, length %d", i, len(c.items))
	}
	return e.apply(e.insertItem(c, i, text)...)
}

// Append adds v at the end of the list that path selects. See
// [Editable.Insert].
func (e *Editable) Append(path string, v Value) error {
	l, err := e.locate(path)
	if err != nil {
		return err
	}
	if l.node == nil {
		return fmt.Errorf("%w: %s", ErrPathNotFound, l.step.Text)
	}
	cont, err := unwrapSome(l.node)
	if err != nil {
		return err
	}
	if cont.kindOf() != ckList {
		return fmt.Errorf("rustyon: %s is not a list", path)
	}
	return e.Insert(path, len(cont.c.items), v)
}

func renderValue(v Value) (string, error) {
	b, err := printDocument(v, &encodeOptions{})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// valueOf parses the value of a node from its source text.
func (e *Editable) valueOf(n *fnode) (Value, error) {
	p := &parser{src: e.src, cur: int(n.start), exts: e.tree.exts, limit: recursionLimit}
	v, err := p.parseValue()
	if err != nil {
		return nil, p.wrap(err)
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Locating values

type ckind uint8

const (
	ckAtom   ckind = iota // scalar, name or None
	ckList                // [ ... ]
	ckMap                 // { ... }
	ckTuple               // ( ... ) or Name( ... ) with plain elements
	ckStruct              // ( ... ) or Name( ... ) with named fields
	ckEmpty               // (), ( ) or Name()
)

func (n *fnode) kindOf() ckind {
	c := n.c
	if c == nil {
		return ckAtom
	}
	switch c.open {
	case '[':
		return ckList
	case '{':
		return ckMap
	case '(':
		switch {
		case len(c.items) == 0:
			return ckEmpty
		case c.items[0].isField:
			return ckStruct
		}
		return ckTuple
	}
	return ckAtom
}

// unwrapSome looks through Some(...) and implicit Some values. The result
// is the node whose structure a path step applies to.
func unwrapSome(n *fnode) (*fnode, error) {
	for n.isSome() {
		c := n.c
		if len(c.items) == 1 && !c.items[0].isField {
			n = &c.items[0].value
			continue
		}
		// Some(x: 1) and Some(1, 2): the content is written inside the
		// parentheses of Some.
		break
	}
	if n.c == nil && n.text == "None" {
		return nil, errors.New("value is None")
	}
	return n, nil
}

type loc struct {
	node   *fnode     // the selected value, nil if the last step is missing
	parent *fnode     // the container of node, nil for the document value
	item   int        // index of node in parent
	kind   ckind      // kind of parent
	step   rpath.Step // the last step
}

func (e *Editable) locate(p string) (loc, error) {
	steps, err := rpath.Parse(p)
	if err != nil {
		return loc{}, err
	}
	cur := &e.tree.value
	l := loc{node: cur, item: -1}
	walked := ""
	for i, s := range steps {
		cont, err := unwrapSome(cur)
		if err != nil {
			return loc{}, fmt.Errorf("%w: %s%s: %w", ErrPathNotFound, walked, s.Text, err)
		}
		idx, err := e.findItem(cont, s)
		if err != nil {
			return loc{}, fmt.Errorf("%w: %s%s: %w", ErrPathNotFound, walked, s.Text, err)
		}
		l = loc{parent: cont, kind: cont.kindOf(), step: s, item: idx}
		if idx < 0 {
			if i < len(steps)-1 {
				return loc{}, fmt.Errorf("%w: %s%s", ErrPathNotFound, walked, s.Text)
			}
			return l, nil
		}
		cur = &cont.c.items[idx].value
		l.node = cur
		walked += s.Text
	}
	return l, nil
}

// findItem returns the index of the element a step selects, or -1 if the
// step is valid for the container but names something that is not there.
func (e *Editable) findItem(cont *fnode, s rpath.Step) (int, error) {
	kind := cont.kindOf()
	var items []fitem
	if cont.c != nil {
		items = cont.c.items
	}
	switch s.Kind {
	case rpath.Name, rpath.Key:
		switch kind {
		case ckStruct:
			for i := range items {
				if strings.TrimPrefix(items[i].key.text, "r#") == s.Name {
					return i, nil
				}
			}
			return -1, nil
		case ckMap:
			for i := range items {
				k, err := e.valueOf(&items[i].key)
				if err != nil {
					continue
				}
				switch k := k.(type) {
				case String:
					if string(k) == s.Name {
						return i, nil
					}
				case Ident:
					if string(k) == s.Name {
						return i, nil
					}
				}
			}
			return -1, nil
		case ckEmpty:
			if cont.isSome() {
				return 0, errors.New("a Some() has no field " + strconv.Quote(s.Name))
			}
			return -1, nil
		}
		return 0, fmt.Errorf("a %s has no field or key %q", kindName(kind), s.Name)
	default:
		switch kind {
		case ckList, ckTuple:
			i := s.Index
			if i < 0 {
				i += len(items)
			}
			if i < 0 || i >= len(items) {
				return 0, fmt.Errorf("index %d out of range, length %d", s.Index, len(items))
			}
			return i, nil
		case ckMap:
			for i := range items {
				k, err := e.valueOf(&items[i].key)
				if err != nil {
					continue
				}
				if n, ok := k.(Int); ok && n.Cmp(NewInt(int64(s.Index))) == 0 {
					return i, nil
				}
			}
			return -1, nil
		}
		return 0, fmt.Errorf("a %s cannot be indexed", kindName(kind))
	}
}

func kindName(k ckind) string {
	switch k {
	case ckList:
		return "list"
	case ckMap:
		return "map"
	case ckTuple:
		return "tuple"
	case ckStruct:
		return "struct"
	case ckEmpty:
		return "empty tuple"
	}
	return "scalar"
}

// newMapKey returns the source text of the key for a new map entry.
func (e *Editable) newMapKey(l loc) string {
	if l.step.Kind == rpath.Index {
		return strconv.Itoa(l.step.Index)
	}
	items := l.parent.c.items
	if len(items) > 0 {
		if k, err := e.valueOf(&items[0].key); err == nil {
			if _, ok := k.(Ident); ok && isPlainIdent(l.step.Name) && !reservedIdents[l.step.Name] {
				return l.step.Name
			}
		}
	}
	var sb bytes.Buffer
	writeQuoted(&sb, l.step.Name)
	return sb.String()
}

// ---------------------------------------------------------------------------
// Text edits

type splice struct {
	at, del int
	text    string
}

// apply makes the edits, which must not overlap, and parses the result. The
// document is only changed if the result is valid.
func (e *Editable) apply(edits ...splice) error {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	src := e.src
	for _, s := range edits {
		src = src[:s.at] + s.text + src[s.at+s.del:]
	}
	d, p, err := buildTree([]byte(src))
	if err != nil {
		return fmt.Errorf("rustyon: the edit would produce an invalid document: %w", err)
	}
	e.src, e.tree = p.src, d
	return nil
}

func (e *Editable) lineStart(off int) int { return strings.LastIndexByte(e.src[:off], '\n') + 1 }

func (e *Editable) lineEnd(off int) int {
	if i := strings.IndexByte(e.src[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(e.src)
}

// indentOf returns the leading whitespace of the line containing off.
func (e *Editable) indentOf(off int) string {
	ls := e.lineStart(off)
	i := ls
	for i < len(e.src) && (e.src[i] == ' ' || e.src[i] == '\t') {
		i++
	}
	return e.src[ls:i]
}

func (e *Editable) startsLine(off int) bool {
	return strings.TrimSpace(e.src[e.lineStart(off):off]) == ""
}

// indentUnit guesses the indentation of one nesting level from the first
// indented line of the document.
func (e *Editable) indentUnit() string {
	for _, line := range strings.Split(e.src, "\n") {
		rest := strings.TrimLeft(line, " \t")
		if len(rest) < len(line) && rest != "" && !strings.HasPrefix(rest, "*") {
			return line[:len(line)-len(rest)]
		}
	}
	return "    "
}

// attachedStart returns where item j starts including the comments written
// directly above it.
func attachedStart(c *fcoll, j int) int {
	it := &c.items[j]
	start := int(it.start)
	lead := it.leading()
	cur := int(it.newlines)
	for k := len(lead) - 1; k >= 0; k-- {
		cm := lead[k]
		if cm.off > it.start {
			continue // a comment inside the item that was moved up
		}
		if cur > 1 {
			break
		}
		start = int(cm.off)
		cur = cm.newlines
	}
	return start
}

// regionEnd returns the end of an item including its comma and the comments
// after it on its line.
func regionEnd(it *fitem) int {
	end := int(it.value.end)
	if it.comma >= 0 {
		end = max(end, int(it.comma)+1)
	}
	for _, cm := range it.trailing() {
		end = max(end, int(cm.end()))
	}
	return end
}

func hasLineComment(cs []fcomment) bool {
	for _, c := range cs {
		if !c.block {
			return true
		}
	}
	return false
}

// insertItem returns the edits that insert the item text as element j of c.
func (e *Editable) insertItem(c *fcoll, j int, text string) []splice {
	unit := e.indentUnit()
	o, cl := int(c.o), int(c.cl)
	if len(c.items) == 0 {
		interior := e.src[o+1 : cl]
		if strings.TrimSpace(interior) == "" {
			if !strings.Contains(interior, "\n") {
				return []splice{{o + 1, len(interior), text}}
			}
			ind := e.indentOf(o)
			return []splice{{o + 1, len(interior), "\n" + ind + unit + text + ",\n" + ind}}
		}
		if strings.Contains(interior, "\n") && e.startsLine(cl) {
			return []splice{{e.lineStart(cl), 0, e.indentOf(cl) + unit + text + ",\n"}}
		}
		return []splice{{cl, 0, " " + text}}
	}
	if j < len(c.items) {
		a0 := attachedStart(c, j)
		if e.startsLine(a0) {
			return []splice{{e.lineStart(a0), 0, e.indentOf(a0) + text + ",\n"}}
		}
		return []splice{{a0, 0, text + ", "}}
	}
	last := &c.items[len(c.items)-1]
	end := regionEnd(last)
	if e.startsLine(attachedStart(c, len(c.items)-1)) || hasLineComment(last.trailing()) {
		ind := e.indentOf(int(last.start))
		if last.comma >= 0 {
			return []splice{{end, 0, "\n" + ind + text + ","}}
		}
		if ve := int(last.value.end); ve < end {
			return []splice{{ve, 0, ","}, {end, 0, "\n" + ind + text}}
		}
		return []splice{{end, 0, ",\n" + ind + text}}
	}
	if last.comma >= 0 {
		return []splice{{end, 0, " " + text + ","}}
	}
	return []splice{{end, 0, ", " + text}}
}

// deleteItem returns the edits that remove element j of c.
func (e *Editable) deleteItem(c *fcoll, j int) []splice {
	it := &c.items[j]
	a0 := attachedStart(c, j)
	b0 := regionEnd(it)
	if len(c.items) == 1 && len(c.afterOpen) == 0 && len(c.tail) == 0 {
		allAttached := true
		for _, cm := range it.leading() {
			if cm.off < it.start && int(cm.off) < a0 {
				allAttached = false
			}
		}
		if allAttached {
			return []splice{{int(c.o) + 1, int(c.cl) - int(c.o) - 1, ""}}
		}
	}
	if e.startsLine(a0) && strings.TrimSpace(e.src[b0:e.lineEnd(b0)]) == "" {
		from, to := e.lineStart(a0), e.lineEnd(b0)
		if to < len(e.src) {
			to++
		}
		return []splice{{from, to - from, ""}}
	}
	if j+1 < len(c.items) {
		next := attachedStart(c, j+1)
		return []splice{{a0, next - a0, ""}}
	}
	if j > 0 {
		prev := &c.items[j-1]
		x := regionEnd(prev)
		if len(prev.trailing()) == 0 {
			x = int(prev.value.end)
		}
		return []splice{{x, b0 - x, ""}}
	}
	return []splice{{a0, b0 - a0, ""}}
}

// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bytes"
	"strings"
)

// The comment preserving syntax tree behind Format and Editable. It is built
// by a mirror of the value parser (see treebuild.go), so it accepts exactly
// the documents Parse accepts. Nodes keep byte offsets into the source so
// edits can be applied as text splices.

type fcomment struct {
	text     string
	off      int32 // offset of the comment in the source
	block    bool
	newlines int // newlines before the comment
}

func (c *fcomment) end() int32 { return c.off + int32(len(c.text)) }

// fnode is a value: an atom (c == nil) or a collection.
type fnode struct {
	text  string // atom text, or the name before a collection
	c     *fcoll
	start int32 // offset of the first byte
	end   int32 // offset just past the last byte
}

func (n *fnode) breaks() bool { return n.c != nil && n.c.breaks }

// isSome reports whether the node is Some(...) or an implicit Some value.
func (n *fnode) isSome() bool { return n.c != nil && n.text == "Some" }

type fcoll struct {
	open      byte // 0 for an implicit Some without delimiters
	close     byte
	multiline bool // source had a line break between elements
	hug       bool // single nested collection printed as Name((...))
	expanded  bool // printed one element per line
	breaks    bool // printing produces line breaks
	spaced    bool // empty collection with anything between the delimiters
	closeN    uint8
	o, cl     int32 // offsets of the delimiters
	items     []fitem
	afterOpen []fcomment // comments on the line of the opening delimiter
	tail      []fcomment // comments after the last item
}

type itemComments struct {
	leading  []fcomment
	trailing []fcomment
}

type fitem struct {
	key      fnode // map key, or the struct field name as an atom
	value    fnode
	cm       *itemComments
	start    int32 // offset of the first token
	comma    int32 // offset of the comma after the item, or -1
	newlines uint8 // newlines before the item's first token, capped at 2
	hasKey   bool  // key holds a map key
	isField  bool  // key holds a struct field name
}

func (it *fitem) leading() []fcomment {
	if it.cm == nil {
		return nil
	}
	return it.cm.leading
}

func (it *fitem) trailing() []fcomment {
	if it.cm == nil {
		return nil
	}
	return it.cm.trailing
}

func (it *fitem) comments() *itemComments {
	if it.cm == nil {
		it.cm = &itemComments{}
	}
	return it.cm
}

// fheader is a comment or an attribute before the document value.
type fheader struct {
	comment  *fcomment
	attr     string // normalized attribute text
	newlines int
}

type fdoc struct {
	header   []fheader
	value    fnode
	valueN   int // newlines before the value
	exts     Extensions
	trailing []fcomment
	after    []fcomment
}

type formatter struct {
	sb     bytes.Buffer
	indent string
	level  int
}

func (f *formatter) newline(blank bool) {
	if blank {
		f.sb.WriteByte('\n')
	}
	f.sb.WriteByte('\n')
	for range f.level {
		f.sb.WriteString(f.indent)
	}
}

func (f *formatter) doc(d *fdoc) {
	first := true
	for _, h := range d.header {
		if !first {
			f.newline(h.newlines > 1)
		}
		first = false
		if h.comment != nil {
			f.sb.WriteString(h.comment.text)
		} else {
			f.sb.WriteString(h.attr)
		}
	}
	if !first {
		f.newline(d.valueN > 1)
	}
	f.node(&d.value)
	f.trailing(d.trailing)
	for _, c := range d.after {
		f.newline(c.newlines > 1)
		f.sb.WriteString(c.text)
	}
	f.sb.WriteByte('\n')
}

// trailing writes comments that follow an element on the same line. A line
// comment ends the line, so any comments after it go on their own lines.
func (f *formatter) trailing(cs []fcomment) {
	onLine := true
	for _, c := range cs {
		if onLine {
			f.sb.WriteByte(' ')
		} else {
			f.newline(false)
		}
		f.sb.WriteString(c.text)
		if !c.block {
			onLine = false
		}
	}
}

func (f *formatter) node(nd *fnode) {
	f.sb.WriteString(nd.text)
	n := nd.c
	if n == nil {
		return
	}
	if n.open == 0 {
		// implicit Some: the value follows the name
		f.sb.WriteByte(' ')
		f.node(&n.items[0].value)
		return
	}
	f.sb.WriteByte(n.open)
	switch {
	case len(n.items) == 0 && len(n.tail) == 0 && len(n.afterOpen) == 0:
		// "()" is the unit value but "( )" is an empty tuple.
		if n.spaced && n.open == '(' && nd.text == "" {
			f.sb.WriteByte(' ')
		}
	case n.hug:
		f.node(&n.items[0].value)
	case !n.expanded:
		for i := range n.items {
			if i > 0 {
				f.sb.WriteString(", ")
			}
			f.itemBody(&n.items[i])
		}
	default:
		f.level++
		f.trailing(n.afterOpen)
		first := true
		for i := range n.items {
			it := &n.items[i]
			for _, c := range it.leading() {
				f.newline(!first && c.newlines > 1)
				f.sb.WriteString(c.text)
				first = false
			}
			f.newline(!first && it.newlines > 1)
			first = false
			f.itemBody(it)
			f.sb.WriteByte(',')
			f.trailing(it.trailing())
		}
		for _, c := range n.tail {
			f.newline(!first && c.newlines > 1)
			f.sb.WriteString(c.text)
			first = false
		}
		f.level--
		f.newline(false)
	}
	f.sb.WriteByte(n.close)
}

func (f *formatter) itemBody(it *fitem) {
	if it.hasKey || it.isField {
		f.node(&it.key)
		f.sb.WriteString(": ")
	}
	f.node(&it.value)
}

// formatTree writes d in the canonical layout.
func formatTree(d *fdoc, srcLen int, indent string) []byte {
	f := &formatter{indent: indent}
	f.sb.Grow(srcLen + srcLen/2)
	f.doc(d)
	return f.sb.Bytes()
}

func capNewlines(n int) uint8 {
	if n > 2 {
		return 2
	}
	return uint8(n)
}

// joinNames is used to normalize attribute lists.
func joinNames(names []string) string { return strings.Join(names, ", ") }

// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"reflect"
	"strconv"

	"github.com/peltonapp/rustyon-go/internal/fields"
)

// This file decodes the serde style enum representations selected with
// EnumOptions. Enums are always decoded through the tree, so the streaming
// decoder needs no counterpart.

// decodeConcreteVariant decodes v into rv, a registered variant type that is
// the target itself rather than an interface.
func (d *decoder) decodeConcreteVariant(v Value, name string, info *enumInfo, rv reflect.Value) error {
	switch info.rep.rep() {
	case repExternal:
		return d.decodeVariantBody(v, name, rv)
	case repUntagged:
		return d.decodeUntaggedBody(v, rv, name)
	}
	tag, found, rest, err := d.readTagged(v, info)
	if err != nil {
		return err
	}
	if found && tag != name {
		return d.typeError(v, rv.Type(), "variant "+strconv.Quote(tag)+" does not match "+strconv.Quote(name))
	}
	body, err := d.taggedBody(info, name, rv.Type(), rest)
	if err != nil {
		return err
	}
	return d.decodeVariantBody(body, name, rv)
}

// readTagged splits the value of a tagged enum into the tag and the other
// fields. found is false when the tag is missing.
func (d *decoder) readTagged(v Value, info *enumInfo) (tag string, found bool, rest []Field, err error) {
	var flds []Field
	switch x := v.(type) {
	case Struct:
		flds = x.Fields
	case Map:
		for _, e := range x {
			switch k := e.Key.(type) {
			case String:
				flds = append(flds, Field{Name: string(k), Value: e.Value})
			case Ident:
				flds = append(flds, Field{Name: string(k), Value: e.Value})
			default:
				return "", false, nil, d.typeError(v, info.iface, "map keys must be strings")
			}
		}
	default:
		return "", false, nil, d.typeError(v, info.iface, "expected a struct with a "+strconv.Quote(info.rep.Tag)+" field")
	}
	rest = make([]Field, 0, len(flds))
	for _, f := range flds {
		if f.Name != info.rep.Tag {
			rest = append(rest, f)
			continue
		}
		switch x := f.Value.(type) {
		case String:
			tag = string(x)
		case Ident:
			tag = string(x)
		default:
			return "", false, nil, d.typeError(v, info.iface, "the "+strconv.Quote(info.rep.Tag)+" field must be a string")
		}
		found = true
	}
	return tag, found, rest, nil
}

// taggedBody converts the fields of a tagged enum value, without the tag,
// into the shape decodeVariantBody expects for the variant type vt.
func (d *decoder) taggedBody(info *enumInfo, name string, vt reflect.Type, rest []Field) (Value, error) {
	if info.rep.rep() == repInternal {
		return Struct{Fields: rest}, nil
	}
	var content Value
	hasContent := false
	for _, f := range rest {
		if f.Name == info.rep.Content {
			content, hasContent = f.Value, true
			continue
		}
		if d.opts.disallowUnknown {
			return nil, &UnknownFieldError{Field: f.Name, Type: info.iface, Path: d.pathString()}
		}
	}
	if !hasContent {
		return Ident(name), nil
	}
	for vt.Kind() == reflect.Pointer {
		vt = vt.Elem()
	}
	if vt.Kind() != reflect.Struct || hasCustomUnmarshaler(vt) {
		return Tuple{Values: []Value{content}}, nil
	}
	switch c := content.(type) {
	case Struct:
		return Struct{Fields: c.Fields}, nil
	case Tuple:
		return Tuple{Values: c.Values}, nil
	case Unit:
		return Ident(name), nil
	}
	return Tuple{Values: []Value{content}}, nil
}

// decodeTaggedInterface decodes an internally or adjacently tagged enum value
// into the interface rv.
func (d *decoder) decodeTaggedInterface(v Value, info *enumInfo, rv reflect.Value) error {
	t := rv.Type()
	tag, found, rest, err := d.readTagged(v, info)
	if err != nil {
		return err
	}
	if !found {
		return d.typeError(v, t, "missing the "+strconv.Quote(info.rep.Tag)+" field")
	}
	vt, ok := info.byName[tag]
	if !ok {
		return d.typeError(v, t, "unknown variant "+strconv.Quote(tag))
	}
	body, err := d.taggedBody(info, tag, vt, rest)
	if err != nil {
		return err
	}
	target := reflect.New(vt).Elem()
	if err := d.decodeVariantBody(body, tag, target); err != nil {
		return err
	}
	rv.Set(target)
	return nil
}

// decodeUntagged decodes v into the interface rv with the first variant that
// fits.
func (d *decoder) decodeUntagged(v Value, info *enumInfo, rv reflect.Value) error {
	hookErr, depth := d.hookErr, len(d.path)
	for _, name := range info.order {
		target := reflect.New(info.byName[name]).Elem()
		err := d.decodeUntaggedBody(v, target, name)
		d.path = d.path[:depth]
		if err == nil {
			rv.Set(target)
			return nil
		}
		d.hookErr = hookErr
	}
	return d.typeError(v, rv.Type(), "no variant matches")
}

func (d *decoder) decodeUntaggedBody(v Value, rv reflect.Value, name string) error {
	t := rv.Type()
	if t.Kind() == reflect.Pointer {
		rv.Set(reflect.New(t.Elem()))
		return d.decodeUntaggedBody(v, rv.Elem(), name)
	}
	if t.Kind() != reflect.Struct || hasCustomUnmarshaler(t) {
		return d.decodePlain(v, rv)
	}
	si := fields.ForNaming(t, d.opts.naming)
	if !untaggedFits(si, v) {
		return d.typeError(v, t, "does not fit variant "+strconv.Quote(name))
	}
	switch x := v.(type) {
	case Struct:
		return d.decodeStruct(Struct{Fields: x.Fields}, rv)
	case Tuple:
		return d.decodeStruct(Tuple{Values: x.Values}, rv)
	}
	return d.decodeStruct(v, rv)
}

// untaggedFits reports whether the struct value v has everything the struct
// variant needs: all fields that are not optional, and nothing at all for a
// variant without fields.
func untaggedFits(si *fields.Info, v Value) bool {
	var names []string
	switch x := v.(type) {
	case Struct:
		for _, f := range x.Fields {
			names = append(names, f.Name)
		}
	case Map:
		for _, e := range x {
			switch k := e.Key.(type) {
			case String:
				names = append(names, string(k))
			case Ident:
				names = append(names, string(k))
			default:
				return false
			}
		}
	case Tuple:
		return len(x.Values) == len(si.Fields)
	case Unit, Ident:
		return len(si.Fields) == 0
	default:
		return false
	}
	if len(si.Fields) == 0 && si.Extra == nil {
		return len(names) == 0
	}
	have := make(map[int]bool, len(names))
	for _, n := range names {
		if i, ok := si.LookupIndex(n); ok {
			have[i] = true
		}
	}
	for i, f := range si.Fields {
		if have[i] || f.OmitEmpty || f.OmitZero {
			continue
		}
		if k := f.Type.Kind(); k == reflect.Pointer || k == reflect.Interface {
			continue
		}
		return false
	}
	return true
}

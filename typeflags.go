// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"reflect"
	"sync"
)

// typeFlags caches the interface checks the encoder and decoder do for
// every value. reflect.Type.Implements is comparatively slow, and the
// answers never change for a type.
type typeFlags struct {
	value        bool // T implements Value (non-pointer, non-interface types only)
	marshaler    bool // T implements Marshaler
	ptrMarshaler bool // *T implements Marshaler, T does not
	text         bool // T implements encoding.TextMarshaler
	ptrText      bool // *T implements encoding.TextMarshaler, T does not
	customEnc    bool // T or *T has a Marshaler or TextMarshaler

	unmarshaler     bool // *T implements Unmarshaler
	textUnmarshaler bool // *T implements encoding.TextUnmarshaler
}

var flagCache sync.Map // reflect.Type to *typeFlags

func flagsOf(t reflect.Type) *typeFlags {
	if f, ok := flagCache.Load(t); ok {
		return f.(*typeFlags)
	}
	pt := reflect.PointerTo(t)
	notPtr := t.Kind() != reflect.Pointer
	f := &typeFlags{
		value:           notPtr && t.Kind() != reflect.Interface && t.Implements(valueType),
		marshaler:       t.Implements(marshalerType),
		text:            t.Implements(textMarshalerType),
		unmarshaler:     pt.Implements(unmarshalerType),
		textUnmarshaler: pt.Implements(textUnmarshalerType),
	}
	f.ptrMarshaler = notPtr && !f.marshaler && pt.Implements(marshalerType)
	f.ptrText = notPtr && !f.text && pt.Implements(textMarshalerType)
	f.customEnc = f.marshaler || f.ptrMarshaler || f.text || f.ptrText ||
		pt.Implements(marshalerType) || pt.Implements(textMarshalerType)
	actual, _ := flagCache.LoadOrStore(t, f)
	return actual.(*typeFlags)
}

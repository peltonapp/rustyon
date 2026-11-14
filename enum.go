// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"fmt"
	"reflect"
	"sync"
)

// Registry maps Go types to Rust style enums. Go has no sum types, so an
// enum is modelled as an interface type whose implementations are the
// variants:
//
//	type Shape interface{ isShape() }
//
//	type Circle struct{ Radius float64 }      // struct variant: Circle(radius: 1.0)
//	type Rect struct {                         // tuple variant:  Rect(2.0, 3.0)
//		_    struct{} `ron:",tuple"`
//		W, H float64
//	}
//	type Empty struct{}                        // unit variant:   Empty
//	type Label string                          // newtype variant: Label("x")
//
//	func (Circle) isShape() {}
//	func (Rect) isShape()   {}
//	func (Empty) isShape()  {}
//	func (Label) isShape()  {}
//
//	rustyon.RegisterEnum[Shape](
//		rustyon.Variant[Circle]("Circle"),
//		rustyon.Variant[Rect]("Rect"),
//		rustyon.Variant[Empty]("Empty"),
//		rustyon.Variant[Label]("Label"),
//	)
//
// Enums whose variants carry no data can also be modelled as integer or
// string constants with [RegisterUnitEnum].
//
// Most programs use [DefaultRegistry] through the package level Register
// functions. A separate Registry can be passed to an [Encoder] or [Decoder].
// A Registry is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	enums    map[reflect.Type]*enumInfo     // interface type to enum
	variants map[reflect.Type][]variantRef  // variant type to its enums
	units    map[reflect.Type]*unitEnumInfo // unit enum type to names
}

type enumInfo struct {
	iface  reflect.Type
	byName map[string]reflect.Type
	byType map[reflect.Type]string
}

type variantRef struct {
	enum *enumInfo
	name string
}

type unitEnumInfo struct {
	byName  map[string]reflect.Value
	byValue map[any]string
}

// VariantSpec describes one variant of an enum. Create it with [Variant].
type VariantSpec struct {
	name string
	typ  reflect.Type
}

// Variant declares that the Go type T is the enum variant called name.
// T can be a pointer type when the interface is implemented by a pointer
// receiver.
func Variant[T any](name string) VariantSpec {
	return VariantSpec{name: name, typ: reflect.TypeFor[T]()}
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		enums:    map[reflect.Type]*enumInfo{},
		variants: map[reflect.Type][]variantRef{},
		units:    map[reflect.Type]*unitEnumInfo{},
	}
}

// DefaultRegistry is the registry used by [Marshal], [Unmarshal] and by
// encoders and decoders that have no registry set.
var DefaultRegistry = NewRegistry()

// RegisterEnum registers the interface type I as an enum with the given
// variants in [DefaultRegistry]. It panics if I is not an interface type, if
// a variant does not implement I, or if names or types are duplicated.
// Registering the same interface again adds variants to it.
func RegisterEnum[I any](variants ...VariantSpec) {
	RegisterEnumIn[I](DefaultRegistry, variants...)
}

// RegisterEnumIn is like [RegisterEnum] but registers into r.
func RegisterEnumIn[I any](r *Registry, variants ...VariantSpec) {
	if err := r.registerEnum(reflect.TypeFor[I](), variants); err != nil {
		panic(err)
	}
}

func (r *Registry) registerEnum(iface reflect.Type, variants []VariantSpec) error {
	if iface.Kind() != reflect.Interface {
		return fmt.Errorf("rustyon: RegisterEnum: %s is not an interface type", iface)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	info := r.enums[iface]
	if info == nil {
		info = &enumInfo{iface: iface, byName: map[string]reflect.Type{}, byType: map[reflect.Type]string{}}
	}
	for _, v := range variants {
		if v.typ == nil {
			return fmt.Errorf("rustyon: RegisterEnum %s: zero VariantSpec", iface)
		}
		if !isRawIdent(v.name) {
			return fmt.Errorf("rustyon: RegisterEnum %s: invalid variant name %q", iface, v.name)
		}
		if v.typ.Kind() == reflect.Interface {
			return fmt.Errorf("rustyon: RegisterEnum %s: variant %s is an interface type", iface, v.typ)
		}
		if !v.typ.Implements(iface) {
			return fmt.Errorf("rustyon: RegisterEnum %s: variant type %s does not implement it", iface, v.typ)
		}
		if t, ok := info.byName[v.name]; ok && t != v.typ {
			return fmt.Errorf("rustyon: RegisterEnum %s: duplicate variant name %q", iface, v.name)
		}
		if n, ok := info.byType[v.typ]; ok && n != v.name {
			return fmt.Errorf("rustyon: RegisterEnum %s: type %s registered twice", iface, v.typ)
		}
		info.byName[v.name] = v.typ
		info.byType[v.typ] = v.name
	}
	r.enums[iface] = info
	// Rebuild the reverse index for this enum.
	for t, refs := range r.variants {
		kept := refs[:0]
		for _, ref := range refs {
			if ref.enum != info {
				kept = append(kept, ref)
			}
		}
		r.variants[t] = kept
	}
	for t, name := range info.byType {
		r.variants[t] = append(r.variants[t], variantRef{enum: info, name: name})
	}
	return nil
}

// RegisterUnitEnum registers T as an enum whose variants carry no data,
// such as a set of integer constants. names maps each value to its variant
// name. Values of T encode as bare identifiers and decode from identifiers
// or strings. It panics if a name is invalid or used twice.
func RegisterUnitEnum[T comparable](names map[T]string) {
	RegisterUnitEnumIn(DefaultRegistry, names)
}

// RegisterUnitEnumIn is like [RegisterUnitEnum] but registers into r.
func RegisterUnitEnumIn[T comparable](r *Registry, names map[T]string) {
	t := reflect.TypeFor[T]()
	info := &unitEnumInfo{byName: map[string]reflect.Value{}, byValue: map[any]string{}}
	for v, name := range names {
		if !isRawIdent(name) {
			panic(fmt.Errorf("rustyon: RegisterUnitEnum %s: invalid variant name %q", t, name))
		}
		if _, dup := info.byName[name]; dup {
			panic(fmt.Errorf("rustyon: RegisterUnitEnum %s: duplicate variant name %q", t, name))
		}
		info.byName[name] = reflect.ValueOf(v)
		info.byValue[v] = name
	}
	r.mu.Lock()
	r.units[t] = info
	r.mu.Unlock()
}

func (r *Registry) enum(iface reflect.Type) *enumInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.enums[iface]
}

func (r *Registry) unitEnum(t reflect.Type) *unitEnumInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.units[t]
}

// variantName returns the variant name for a concrete type. When static is
// a registered enum interface its name wins, otherwise the type must belong
// to exactly one enum.
func (r *Registry) variantName(t reflect.Type, static reflect.Type) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if info := r.enums[static]; static != nil && info != nil {
		name, ok := info.byType[t]
		return name, ok
	}
	refs := r.variants[t]
	if len(refs) == 0 {
		return "", false
	}
	name := refs[0].name
	for _, ref := range refs[1:] {
		if ref.name != name {
			return "", false
		}
	}
	return name, true
}

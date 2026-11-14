// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import "github.com/peltonapp/rustyon-go/internal/fields"

// NamingStrategy selects how Go field names without an explicit tag name are
// written as RON field names. The zero value is [SnakeCase], which is the
// default of every function in this package.
//
// Set it for a whole encoder or decoder with [Encoder.SetFieldNaming],
// [Decoder.SetFieldNaming], [EncodeOptions] or [DecodeOptions], or for a
// single struct with a rename_all option on its blank field, as serde does:
//
//	type Config struct {
//		_          struct{} `ron:",rename_all=camelCase"`
//		WindowSize int      // window_size becomes windowSize
//	}
//
// The option accepts the serde names: "snake_case", "camelCase",
// "PascalCase", "SCREAMING_SNAKE_CASE", "kebab-case", "SCREAMING-KEBAB-CASE",
// "lowercase", "UPPERCASE" and "identity" (the Go name as it is). Unknown
// values are ignored. An explicit `ron:"name"` tag on a field always wins,
// and an embedded struct that has its own rename_all keeps it for its fields.
//
// Names that are not plain RON identifiers, such as the kebab-case ones, are
// written as raw identifiers (r#window-size).
type NamingStrategy uint8

// The field naming strategies. The examples show the name for the Go field
// MaxHP, or for the words max and hp.
const (
	SnakeCase          NamingStrategy = NamingStrategy(fields.Snake)          // max_hp
	CamelCase          NamingStrategy = NamingStrategy(fields.Camel)          // maxHp
	PascalCase         NamingStrategy = NamingStrategy(fields.Pascal)         // MaxHp
	ScreamingSnakeCase NamingStrategy = NamingStrategy(fields.ScreamingSnake) // MAX_HP
	KebabCase          NamingStrategy = NamingStrategy(fields.Kebab)          // r#max-hp
	ScreamingKebabCase NamingStrategy = NamingStrategy(fields.ScreamingKebab) // r#MAX-HP
	LowerCase          NamingStrategy = NamingStrategy(fields.Lower)          // maxhp
	UpperCase          NamingStrategy = NamingStrategy(fields.Upper)          // MAXHP
	Identity           NamingStrategy = NamingStrategy(fields.Identity)       // MaxHP
)

// String returns the serde name of the strategy, for example "camelCase".
func (n NamingStrategy) String() string { return fields.Naming(n).String() }

// Name converts a Go field name according to the strategy.
func (n NamingStrategy) Name(goName string) string { return fields.Naming(n).Apply(goName) }

// EncodeOptions configures [MarshalWith]. The zero value gives the same
// output as [Marshal].
type EncodeOptions struct {
	// FieldNaming is the default field naming. The zero value is [SnakeCase].
	FieldNaming NamingStrategy
	// StructNames writes struct names, as [Encoder.SetStructNames].
	StructNames bool
	// ImplicitSome writes non-nil pointers without Some, as
	// [Encoder.SetImplicitSome].
	ImplicitSome bool
	// UnwrapNewtypes writes single field tuple structs as their inner value,
	// as [Encoder.SetUnwrapNewtypes].
	UnwrapNewtypes bool
	// Registry is the enum registry, or nil for [DefaultRegistry].
	Registry *Registry
	// Pretty selects pretty printed output with the given layout. Nil gives
	// compact output.
	Pretty *PrettyConfig
}

func (o EncodeOptions) internal() *encodeOptions {
	return &encodeOptions{
		pretty:         o.Pretty,
		structNames:    o.StructNames,
		implicitSome:   o.ImplicitSome,
		unwrapNewtypes: o.UnwrapNewtypes,
		naming:         fields.Naming(o.FieldNaming),
		registry:       o.Registry,
	}
}

// MarshalWith is like [Marshal] with the given options.
func MarshalWith(v any, opts EncodeOptions) ([]byte, error) {
	return marshal(v, opts.internal())
}

// DecodeOptions configures [UnmarshalWith]. The zero value gives the same
// behavior as [Unmarshal].
type DecodeOptions struct {
	// FieldNaming is the default field naming. The zero value is [SnakeCase].
	FieldNaming NamingStrategy
	// DisallowUnknownFields makes decoding fail on fields without a Go field,
	// as [Decoder.DisallowUnknownFields].
	DisallowUnknownFields bool
	// Registry is the enum registry, or nil for [DefaultRegistry].
	Registry *Registry
}

func (o DecodeOptions) internal() *decodeOptions {
	return &decodeOptions{
		disallowUnknown: o.DisallowUnknownFields,
		naming:          fields.Naming(o.FieldNaming),
		registry:        o.Registry,
	}
}

// UnmarshalWith is like [Unmarshal] with the given options.
func UnmarshalWith(data []byte, v any, opts DecodeOptions) error {
	return unmarshal(data, v, opts.internal())
}

func unmarshal(data []byte, v any, opts *decodeOptions) error {
	err := unmarshalStream(data, v, opts)
	if err == nil {
		return nil
	}
	// The streaming decoder only reports that it failed. Parsing finds
	// syntax errors and tree decoding into a scratch value finds the rest.
	return unmarshalFallback(data, v, opts, err)
}

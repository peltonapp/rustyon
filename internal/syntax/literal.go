// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package syntax

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ExtensionNames lists the extensions accepted in #![enable(...)], in bit
// order.
var ExtensionNames = []string{
	"implicit_some",
	"unwrap_newtypes",
	"unwrap_variant_newtypes",
	"explicit_struct_names",
}

// ExtensionBit returns the bit index of an extension name.
func ExtensionBit(name string) (int, bool) {
	for i, n := range ExtensionNames {
		if n == name {
			return i, true
		}
	}
	return 0, false
}

var intSuffixes = map[string]bool{
	"i8": true, "i16": true, "i32": true, "i64": true, "i128": true, "isize": true,
	"u8": true, "u16": true, "u32": true, "u64": true, "u128": true, "usize": true,
}

type intRange struct {
	min, max *big.Int
}

func bitRange(bits uint, signed bool) intRange {
	if signed {
		m := new(big.Int).Lsh(big.NewInt(1), bits-1)
		return intRange{min: new(big.Int).Neg(m), max: m.Sub(m, big.NewInt(1))}
	}
	m := new(big.Int).Lsh(big.NewInt(1), bits)
	return intRange{min: new(big.Int), max: m.Sub(m, big.NewInt(1))}
}

var suffixRanges = map[string]intRange{
	"i8": bitRange(8, true), "i16": bitRange(16, true), "i32": bitRange(32, true),
	"i64": bitRange(64, true), "i128": bitRange(128, true), "isize": bitRange(64, true),
	"u8": bitRange(8, false), "u16": bitRange(16, false), "u32": bitRange(32, false),
	"u64": bitRange(64, false), "u128": bitRange(128, false), "usize": bitRange(64, false),
}

// ParseInt decodes an integer token. Values that fit into an int64 are
// returned as small with big == nil; larger values are returned in big.
func ParseInt(t Token) (small int64, bigN *big.Int, err error) {
	s := t.Str
	neg := strings.HasPrefix(s, "-")
	digits := strings.TrimPrefix(s, "-")
	base := 10
	if len(digits) > 2 && digits[0] == '0' {
		switch digits[1] {
		case 'x':
			base = 16
		case 'o':
			base = 8
		case 'b':
			base = 2
		}
		if base != 10 {
			digits = digits[2:]
		}
	}
	if t.Suffix == "" || (t.Suffix != "u128" && t.Suffix != "i128") {
		// Fast path for values that fit into 64 bits.
		if u, perr := strconv.ParseUint(digits, base, 64); perr == nil {
			var v int64
			fits := false
			switch {
			case !neg && u <= math.MaxInt64:
				v, fits = int64(u), true
			case neg && u <= 1<<63:
				v, fits = -int64(u), true
			}
			if fits {
				if t.Suffix != "" {
					r := suffixRanges[t.Suffix]
					if big.NewInt(v).Cmp(r.min) < 0 || big.NewInt(v).Cmp(r.max) > 0 {
						return 0, nil, errors.New("integer " + t.Raw + " out of range for " + t.Suffix)
					}
				}
				return v, nil, nil
			}
		}
	}
	n, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return 0, nil, errors.New("invalid integer " + strconv.Quote(t.Raw))
	}
	if neg {
		n.Neg(n)
	}
	if t.Suffix != "" {
		r := suffixRanges[t.Suffix]
		if n.Cmp(r.min) < 0 || n.Cmp(r.max) > 0 {
			return 0, nil, errors.New("integer " + t.Raw + " out of range for " + t.Suffix)
		}
	}
	if n.IsInt64() {
		return n.Int64(), nil, nil
	}
	return 0, n, nil
}

// ParseFloat decodes a float token. Values out of range become infinities.
func ParseFloat(t Token) (float64, error) {
	if strings.TrimLeft(t.Str, "+-") == "NaN" {
		return math.NaN(), nil
	}
	bits := 64
	if t.Suffix == "f32" {
		bits = 32
	}
	f, err := strconv.ParseFloat(t.Str, bits)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			return f, nil
		}
		return 0, errors.New("invalid float " + strconv.Quote(t.Raw))
	}
	return f, nil
}

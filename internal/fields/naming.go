// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package fields

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Naming selects how Go field names become RON field names.
type Naming uint8

// The naming strategies. Snake is the zero value and the default.
const (
	Snake          Naming = iota // MaxHP -> max_hp
	Camel                        // MaxHP -> maxHp
	Pascal                       // MaxHP -> MaxHp
	ScreamingSnake               // MaxHP -> MAX_HP
	Kebab                        // MaxHP -> max-hp
	ScreamingKebab               // MaxHP -> MAX-HP
	Lower                        // MaxHP -> maxhp
	Upper                        // MaxHP -> MAXHP
	Identity                     // MaxHP -> MaxHP

	NumNamings
)

var namingNames = [NumNamings]string{
	Snake:          "snake_case",
	Camel:          "camelCase",
	Pascal:         "PascalCase",
	ScreamingSnake: "SCREAMING_SNAKE_CASE",
	Kebab:          "kebab-case",
	ScreamingKebab: "SCREAMING-KEBAB-CASE",
	Lower:          "lowercase",
	Upper:          "UPPERCASE",
	Identity:       "identity",
}

// String returns the serde style name of the strategy, such as "camelCase".
func (n Naming) String() string {
	if n < NumNamings {
		return namingNames[n]
	}
	return "Naming(" + strconv.Itoa(int(n)) + ")"
}

// ParseNaming finds a strategy by its serde style name.
func ParseNaming(s string) (Naming, bool) {
	for n, name := range namingNames {
		if name == s {
			return Naming(n), true
		}
	}
	return 0, false
}

// Apply converts a Go field name according to the strategy.
func (n Naming) Apply(s string) string {
	switch n {
	case Camel, Pascal:
		var sb strings.Builder
		sb.Grow(len(s) + 2)
		first := true
		for w := range strings.SplitSeq(SnakeCase(s), "_") {
			if w == "" {
				continue
			}
			if first && n == Camel {
				sb.WriteString(w)
			} else {
				r, size := utf8.DecodeRuneInString(w)
				sb.WriteRune(unicode.ToUpper(r))
				sb.WriteString(w[size:])
			}
			first = false
		}
		return sb.String()
	case ScreamingSnake:
		return strings.ToUpper(SnakeCase(s))
	case Kebab:
		return strings.ReplaceAll(SnakeCase(s), "_", "-")
	case ScreamingKebab:
		return strings.ToUpper(strings.ReplaceAll(SnakeCase(s), "_", "-"))
	case Lower:
		return strings.ToLower(s)
	case Upper:
		return strings.ToUpper(s)
	case Identity:
		return s
	}
	return SnakeCase(s)
}

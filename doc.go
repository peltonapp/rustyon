// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Package rustyon implements Rusty Object Notation (RON), the human readable
// data format used throughout the Rust ecosystem.
//
// rustyon is an independent implementation and is not affiliated with the
// ron-rs project. It follows the grammar of current ron-rs releases,
// including raw strings, byte strings, nested block comments, integer and
// float suffixes and the #![enable(...)] extensions.
//
// The package offers four layers:
//
//   - [Marshal], [Unmarshal], [Encoder] and [Decoder] convert between RON and
//     Go values, in the style of encoding/json.
//   - [Parse] and [Value] give a dynamic tree for documents whose shape is
//     not known in advance.
//   - [Registry] maps Rust enums onto Go interfaces so that variants such as
//     Circle(radius: 1.0) decode into concrete Go types.
//   - [Format] pretty prints RON documents while preserving comments.
//
// A short example:
//
//	type Config struct {
//		WindowTitle string
//		MaxFPS      *int
//	}
//
//	var c Config
//	err := rustyon.Unmarshal([]byte(`(window_title: "Game", max_fps: Some(60))`), &c)
//
// Go field names are converted to snake_case by default, so WindowTitle
// matches window_title. See [Marshal] and [Unmarshal] for the full mapping.
package rustyon

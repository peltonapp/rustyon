<p align="center">
  <img src=".github/assets/logo.svg" alt="rustyon" width="480">
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/peltonapp/rustyon-go"><img src="https://img.shields.io/badge/go.dev-reference-00ADD8?logo=go&logoColor=white" alt="Go Reference"></a>
  <a href="https://github.com/peltonapp/rustyon-go/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/peltonapp/rustyon-go/ci.yml?branch=main&label=CI&logo=github" alt="CI"></a>
  <a href="https://codecov.io/gh/peltonapp/rustyon-go"><img src="https://codecov.io/gh/peltonapp/rustyon-go/graph/badge.svg" alt="Coverage"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/peltonapp/rustyon-go"><img src="https://api.scorecard.dev/projects/github.com/peltonapp/rustyon-go/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://github.com/peltonapp/rustyon-go/releases"><img src="https://img.shields.io/github/v/release/peltonapp/rustyon-go?include_prereleases&sort=semver&color=CE422B" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/peltonapp/rustyon-go?color=00ADD8" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD-3-Clause"></a>
  <a href="https://github.com/peltonapp/rustyon-go/wiki"><img src="https://img.shields.io/badge/docs-wiki-CE422B" alt="Docs"></a>
  <a href="https://peltonapp.github.io/rustyon-go/"><img src="https://img.shields.io/badge/playground-try%20it-00ADD8" alt="Playground"></a>
  <img src="https://img.shields.io/badge/dependencies-0-success" alt="Zero dependencies">
</p>

**rustyon** is a Go implementation of [Rusty Object Notation (RON)](https://github.com/ron-rs/ron), the human-readable serialization format used in the Rust ecosystem. It is an independent implementation and is not affiliated with the [ron-rs](https://github.com/ron-rs/ron) project.

```ron
// Read and write files like this from Go.
#![enable(implicit_some)]
GameConfig(
    window_size: (800, 600),
    window_title: "Rusty Game",
    difficulty: Hard,
    spawn: Circle(radius: 2.5),
    key_bindings: {"up": Up, "down": Down},
)
```

## Features

- **`Marshal` / `Unmarshal`** with an `encoding/json` style API, struct tags, `omitempty` and `omitzero`, `Encoder` and `Decoder` for streams.
- **Rust enums on Go interfaces.** Register variants once and `Circle(radius: 1.0)` decodes into your `Circle` type.
- **Dynamic `Value` tree** for documents with an unknown shape, including 128-bit integers.
- **Formatter that keeps comments**, like `gofmt` for `.ron` files.
- **Full ron-rs grammar**: raw strings, byte strings, nested block comments, number suffixes, `#![enable(...)]` extensions.
- **`rustyon` CLI** to format, validate, query and convert RON to and from JSON, YAML and TOML.
- **Zero dependencies** in the library, fuzz tested, Go 1.24+. The CLI is a separate module, so its YAML and TOML libraries never reach your build.

## Install

```sh
go get github.com/peltonapp/rustyon-go
```

The package name is `rustyon`:

```go
import "github.com/peltonapp/rustyon-go"
```

## Quick start

```go
type Config struct {
    WindowTitle string    // matches window_title
    WindowSize  [2]int    // matches (800, 600)
    MaxFPS      *int      // Option<u32>: None or Some(60)
    Tags        []string  `ron:"labels,omitempty"`
}

var c Config
err := rustyon.Unmarshal(data, &c)

out, err := rustyon.MarshalIndent(c, "", "    ")
```

Go field names become `snake_case` by default, so Go structs line up with Rust structs without tags. Pointers map to `Option`: `nil` is `None`, anything else is `Some(v)`. The [wiki](https://github.com/peltonapp/rustyon-go/wiki) has the full type mapping.

### Field naming

`snake_case` stays the default. Pick another strategy for a whole encoder or decoder, or per struct with a serde style `rename_all` option on the blank field. Explicit `ron:"name"` tags always win.

```go
out, err := rustyon.MarshalWith(cfg, rustyon.EncodeOptions{FieldNaming: rustyon.CamelCase})
err = rustyon.UnmarshalWith(data, &cfg, rustyon.DecodeOptions{FieldNaming: rustyon.CamelCase})

type Window struct {
    _     struct{} `ron:",rename_all=PascalCase"` // also: camelCase, SCREAMING_SNAKE_CASE, kebab-case, identity, ...
    Title string                                  // Title
}
```

### Pretty printing options

`MarshalIndent` covers the common case. `MarshalWithConfig` (or `Encoder.SetPrettyConfig`) mirrors ron-rs's `PrettyConfig`: compact arrays, structs and maps, separate tuple members, a depth limit, `\u{..}` escapes for non-ASCII text, custom line break, indent, prefix and separator after the colon, and `// [0]` index comments for lists. The output always parses back to the same value.

```go
out, err := rustyon.MarshalWithConfig(v, rustyon.PrettyConfig{CompactArrays: true, DepthLimit: 2})
```

### Raw values and generics

`rustyon.Raw` (like `json.RawMessage`) keeps one RON value as text, so a part of a document can be decoded later or passed through. `UnmarshalAs[T]` and `DecodeAs[T]` return the value instead of filling a pointer.

```go
type Envelope struct {
    Kind    string
    Payload rustyon.Raw // captured as written, comments included
}
cfg, err := rustyon.UnmarshalAs[Config](data)
```

### Enums

Go has no sum types, so a Rust enum becomes an interface and each variant a type that implements it:

```go
type Shape interface{ isShape() }

type Circle struct{ Radius float64 }        // Circle(radius: 1.0)
type Rect struct {                          // Rect(2.0, 3.0)
    _    struct{} `ron:",tuple"`
    W, H float64
}
type Empty struct{}                         // Empty

func (Circle) isShape() {}
func (Rect) isShape()   {}
func (Empty) isShape()  {}

func init() {
    rustyon.RegisterEnum[Shape](
        rustyon.Variant[Circle]("Circle"),
        rustyon.Variant[Rect]("Rect"),
        rustyon.Variant[Empty]("Empty"),
    )
}

var shapes []Shape
err := rustyon.Unmarshal([]byte(`[Circle(radius: 1.5), Rect(2.0, 3.0), Empty]`), &shapes)
```

Enums without data, like `enum Difficulty { Easy, Hard }`, map onto Go constants with `rustyon.RegisterUnitEnum`.

### Dynamic values

```go
v, err := rustyon.Parse([]byte(`Point(x: 1, y: -2)`))
s := v.(rustyon.Struct)
y, _ := s.Get("y") // rustyon.Int
```

### Formatting

```go
pretty, err := rustyon.Format(src) // comments stay where they were
```

## CLI

```sh
go install github.com/peltonapp/rustyon-go/cmd/rustyon@latest

rustyon fmt -w config.ron       # format in place, keeps comments
rustyon validate *.ron          # check syntax
rustyon to-json save.ron        # RON to JSON
rustyon from-json data.json     # JSON to RON
rustyon to-yaml save.ron        # RON to YAML
rustyon from-yaml cfg.yaml      # YAML to RON
rustyon to-toml save.ron        # RON to TOML
rustyon from-toml cfg.toml      # TOML to RON
rustyon get 'items[0].name' save.ron   # print one value
```

Binaries for Linux, macOS, Windows and FreeBSD are attached to the [releases tagged `cmd/rustyon/v*`](https://github.com/peltonapp/rustyon-go/releases). The CLI is its own Go module (`github.com/peltonapp/rustyon-go/cmd/rustyon`) with its own version tags, so the library stays free of dependencies.

Syntax errors point at the problem:

```text
$ rustyon validate game.ron
game.ron:3:16: expected ',' or ')'
 3 |     size: (800 600),
   |                ^
```

**Conversions.** `to-*` and `from-*` read a file or standard input and write to standard output. `from-json`, `from-yaml` and `from-toml` take `-structs` to write mappings whose keys are identifiers as structs (`key: value`) instead of maps (`{"key": value}`), and `-indent` to change or disable pretty printing. Like JSON, YAML and TOML cannot hold everything RON can, so struct and variant names are dropped. YAML keeps the field order. TOML has no null and needs a table at the top level: `None` fields are left out, `None` inside an array is an error, and keys are written sorted. Dates and times in TOML become strings.

**`get`.** `rustyon get <path> [file]` prints the RON value at a path and exits with status 1 if it does not exist. A path is built from three steps:

| Step      | Selects                                                                     |
| --------- | --------------------------------------------------------------------------- |
| `.name`   | struct field, or map entry with a string or identifier key (leading dot is optional) |
| `[N]`     | list or tuple element, `-1` is the last; map entry with integer key `N`     |
| `["key"]` | map entry with a string key (Go escapes), or struct field                   |

Examples: `items[0].name`, `.config.port`, `players["alice"]`, `tags[-1]`. `Some(x)` is looked through, and `.` selects the whole document. Use `-indent ""` for compact output.

## Performance

Everything is linear in the input size. Typed `Unmarshal` decodes straight from the text without building an intermediate tree and runs on par with `encoding/json`; parsing into a dynamic tree is about twice as fast as `encoding/json` decoding into `any`; typed `Marshal` is about 2.3x slower than `encoding/json`. `Format` keeps comments and is several times slower than `json.Indent`. A 13 KB file encodes in about 0.13 ms, decodes in about 0.09 ms and formats in about 0.3 ms on an Apple M2. Charts, Big O tables and methodology are on the [Performance](https://github.com/peltonapp/rustyon-go/wiki/Performance) wiki page.

## Documentation

- [Wiki](https://github.com/peltonapp/rustyon-go/wiki): guides, type mapping, enums, formatter rules, performance
- [API reference](https://pkg.go.dev/github.com/peltonapp/rustyon-go) on pkg.go.dev
- RON itself: [ron-rs/ron](https://github.com/ron-rs/ron) and the Rust crate docs on [docs.rs/ron](https://docs.rs/ron)

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md); all commits need a DCO sign-off (`git commit -s`).

## License

[BSD-3-Clause](LICENSE). The RON format was created by the [ron-rs](https://github.com/ron-rs/ron) authors.

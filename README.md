<p align="center">
  <img src=".github/assets/logo.svg" alt="rustyon" width="480">
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/peltonapp/rustyon-go"><img src="https://img.shields.io/badge/go.dev-reference-00ADD8?logo=go&logoColor=white" alt="Go Reference"></a>
  <a href="https://github.com/peltonapp/rustyon-go/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/peltonapp/rustyon-go/ci.yml?branch=main&label=CI&logo=github" alt="CI"></a>
  <a href="https://github.com/peltonapp/rustyon-go/releases"><img src="https://img.shields.io/github/v/release/peltonapp/rustyon-go?include_prereleases&sort=semver&color=CE422B" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/peltonapp/rustyon-go?color=00ADD8" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD-3-Clause"></a>
  <a href="https://github.com/peltonapp/rustyon-go/wiki"><img src="https://img.shields.io/badge/docs-wiki-CE422B" alt="Docs"></a>
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
- **`rustyon` CLI** to format, validate and convert RON to and from JSON.
- **Zero dependencies**, fuzz tested, Go 1.24+.

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
```

## Documentation

- [Wiki](https://github.com/peltonapp/rustyon-go/wiki): guides, type mapping, enums, formatter rules, performance
- [API reference](https://pkg.go.dev/github.com/peltonapp/rustyon-go) on pkg.go.dev
- RON itself: [ron-rs/ron](https://github.com/ron-rs/ron) and the Rust crate docs on [docs.rs/ron](https://docs.rs/ron)

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md); all commits need a DCO sign-off (`git commit -s`).

## License

[BSD-3-Clause](LICENSE). The RON format was created by the [ron-rs](https://github.com/ron-rs/ron) authors.

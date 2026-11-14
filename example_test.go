// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon_test

import (
	"fmt"
	"log"
	"os"

	"github.com/peltonapp/rustyon-go"
)

func ExampleUnmarshal() {
	type Config struct {
		WindowTitle string
		WindowSize  [2]int
		MaxFPS      *int
		Tags        []string
	}
	src := []byte(`
// Rust style config
Config(
    window_title: "Rusty Game",
    window_size: (800, 600),
    max_fps: Some(60),
    tags: ["fast", "fun"],
)`)
	var c Config
	if err := rustyon.Unmarshal(src, &c); err != nil {
		log.Fatal(err)
	}
	fmt.Println(c.WindowTitle, c.WindowSize, *c.MaxFPS, c.Tags)
	// Output: Rusty Game [800 600] 60 [fast fun]
}

func ExampleMarshalIndent() {
	type Player struct {
		Name  string
		MaxHP int
		Pos   struct {
			_    struct{} `ron:",tuple"`
			X, Y float64
		}
	}
	p := Player{Name: "Ferris", MaxHP: 100}
	p.Pos.X, p.Pos.Y = 1, 2.5
	out, err := rustyon.MarshalIndent(p, "", "    ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
	// Output:
	// (
	//     name: "Ferris",
	//     max_hp: 100,
	//     pos: (1.0, 2.5),
	// )
}

type Shape interface{ isShape() }

type Circle struct{ Radius float64 }

type Square struct {
	_    struct{} `ron:",tuple"`
	Side float64
}

type Dot struct{}

func (Circle) isShape() {}
func (Square) isShape() {}
func (Dot) isShape()    {}

func ExampleRegisterEnum() {
	// In real code this goes into an init function.
	rustyon.RegisterEnum[Shape](
		rustyon.Variant[Circle]("Circle"),
		rustyon.Variant[Square]("Square"),
		rustyon.Variant[Dot]("Dot"),
	)

	var shapes []Shape
	err := rustyon.Unmarshal([]byte(`[Circle(radius: 1.5), Square(2.0), Dot]`), &shapes)
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range shapes {
		fmt.Printf("%T %+v\n", s, s)
	}
	out, _ := rustyon.Marshal(shapes)
	fmt.Println(string(out))
	// Output:
	// rustyon_test.Circle {Radius:1.5}
	// rustyon_test.Square {_:{} Side:2}
	// rustyon_test.Dot {}
	// [Circle(radius:1.5),Square(2.0),Dot]
}

type Level int

const (
	Easy Level = iota
	Hard
)

func ExampleRegisterUnitEnum() {
	rustyon.RegisterUnitEnum(map[Level]string{Easy: "Easy", Hard: "Hard"})

	var l Level
	if err := rustyon.Unmarshal([]byte("Hard"), &l); err != nil {
		log.Fatal(err)
	}
	out, _ := rustyon.Marshal(struct{ Difficulty Level }{l})
	fmt.Println(l == Hard, string(out))
	// Output: true (difficulty:Hard)
}

func ExampleParse() {
	v, err := rustyon.Parse([]byte(`Point(x: 1, y: -2)`))
	if err != nil {
		log.Fatal(err)
	}
	s := v.(rustyon.Struct)
	y, _ := s.Get("y")
	fmt.Println(s.Name, len(s.Fields), y.(rustyon.Int).String())
	// Output: Point 2 -2
}

func ExampleFormat() {
	src := []byte(`(name:"x", // the name
tags:[1,2,3])`)
	out, err := rustyon.Format(src)
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(out)
	// Output:
	// (
	//     name: "x", // the name
	//     tags: [1, 2, 3],
	// )
}

func ExampleParseEditable() {
	src := []byte(`// server settings
(
    host: "localhost", // where to listen
    port: 8080,
    users: ["ann", "bob"],
)`)
	doc, err := rustyon.ParseEditable(src)
	if err != nil {
		log.Fatal(err)
	}
	port, _ := doc.Get("port")
	fmt.Println("port was", port)

	if err := doc.Set("port", rustyon.NewInt(9090)); err != nil {
		log.Fatal(err)
	}
	if err := doc.Append("users", rustyon.String("cy")); err != nil {
		log.Fatal(err)
	}
	if err := doc.Delete("users[0]"); err != nil {
		log.Fatal(err)
	}
	// A missing field is added at the end.
	if err := doc.Set("debug", rustyon.Bool(true)); err != nil {
		log.Fatal(err)
	}
	fmt.Print(doc)
	// Output:
	// port was 8080
	// // server settings
	// (
	//     host: "localhost", // where to listen
	//     port: 9090,
	//     users: ["bob", "cy"],
	//     debug: true,
	// )
}

func ExampleEditable_SetRON() {
	doc, err := rustyon.ParseEditable([]byte(`(range: 1..3, name: "x") // keep me`))
	if err != nil {
		log.Fatal(err)
	}
	// Literals can be written exactly as they should appear.
	if err := doc.SetRON("range", "0x10..=0x20"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(doc)
	// Output: (range: 0x10..=0x20, name: "x") // keep me
}

func ExampleEditable_Insert() {
	doc, err := rustyon.ParseEditable([]byte("[\n    1,\n    // three\n    3,\n]"))
	if err != nil {
		log.Fatal(err)
	}
	if err := doc.Insert("", 1, rustyon.NewInt(2)); err != nil {
		log.Fatal(err)
	}
	fmt.Println(doc)
	// Output:
	// [
	//     1,
	//     2,
	//     // three
	//     3,
	// ]
}

func ExampleEditable_Delete() {
	doc, err := rustyon.ParseEditable([]byte("(\n    a: 1,\n    // b is unused\n    b: 2,\n    c: Some((x: 3)),\n)"))
	if err != nil {
		log.Fatal(err)
	}
	// The comment above b goes with it. Paths look through Some.
	if err := doc.Delete("b"); err != nil {
		log.Fatal(err)
	}
	if err := doc.Delete("c.x"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(doc)
	// Output:
	// (
	//     a: 1,
	//     c: Some(()),
	// )
}

func ExampleEncoder() {
	enc := rustyon.NewEncoder(os.Stdout)
	enc.SetStructNames(true)
	enc.SetImplicitSome(true)
	type Window struct {
		Title string
		Width *int
	}
	w := 640
	if err := enc.Encode(Window{"main", &w}); err != nil {
		log.Fatal(err)
	}
	// Output: #![enable(implicit_some)]Window(title:"main",width:640)
}

func ExampleMarshalWithConfig() {
	type Level struct {
		Name   string
		Tiles  []int
		Spawn  [2]int
		Memo   string
		Nested struct{ Deep []int }
	}
	l := Level{Name: "café", Tiles: []int{1, 2, 3}, Spawn: [2]int{4, 5}, Memo: "x"}
	l.Nested.Deep = []int{9}
	out, err := rustyon.MarshalWithConfig(l, rustyon.PrettyConfig{
		CompactArrays:  true,
		EscapeNonASCII: true,
		DepthLimit:     1,
		Indent:         "  ",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
	// Output:
	// (
	//   name: "caf\u{e9}",
	//   tiles: [1, 2, 3],
	//   spawn: [4, 5],
	//   memo: "x",
	//   nested: (deep: [9]),
	// )
}

func ExampleNamingStrategy() {
	type Window struct {
		WindowTitle string
		MaxFPS      int
	}
	out, err := rustyon.MarshalWith(Window{"main", 60}, rustyon.EncodeOptions{FieldNaming: rustyon.CamelCase})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))

	// A rename_all option on the struct overrides the default, as in serde.
	type Fixed struct {
		_      struct{} `ron:",rename_all=SCREAMING_SNAKE_CASE"`
		MaxFPS int
		Tagged int `ron:"keep"`
	}
	out, _ = rustyon.MarshalWith(Fixed{MaxFPS: 60, Tagged: 1}, rustyon.EncodeOptions{FieldNaming: rustyon.CamelCase})
	fmt.Println(string(out))

	var w Window
	err = rustyon.UnmarshalWith([]byte(`(windowTitle: "x", maxFps: 30)`), &w, rustyon.DecodeOptions{FieldNaming: rustyon.CamelCase})
	fmt.Println(w, err)
	// Output:
	// (windowTitle:"main",maxFps:60)
	// (MAX_FPS:60,keep:1)
	// {x 30} <nil>
}

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

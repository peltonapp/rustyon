// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

// Command rustyon formats, validates and converts RON (Rusty Object
// Notation) files.
//
// Usage:
//
//	rustyon fmt [-w] [-l] [-indent str] [file ...]
//	rustyon validate [-q] [file ...]
//	rustyon to-json [-indent str] [file]
//	rustyon from-json [-structs] [-indent str] [file]
//	rustyon to-yaml [file]
//	rustyon from-yaml [-structs] [-indent str] [file]
//	rustyon to-toml [file]
//	rustyon from-toml [-structs] [-indent str] [file]
//	rustyon get [-indent str] path [file]
//	rustyon version
//
// Without file arguments the commands read from standard input. Syntax
// errors are reported as file:line:col: message with the source line and a
// caret under the column.
//
// A path for get is a field name, an index or a quoted map key, joined as in
// items[0].name, .config.port or players["alice"]. Run "rustyon get -h" for
// the full syntax. get exits with status 1 when the path does not exist.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/peltonapp/rustyon-go"
	"github.com/peltonapp/rustyon-go/ronjson"
)

// version is set at build time by GoReleaser.
var version = ""

const usage = `rustyon formats, validates and converts RON (Rusty Object Notation).

Usage:
  rustyon <command> [flags] [file ...]

Commands:
  fmt         format RON files, keeping comments
  validate    check that RON files are well formed
  to-json     convert RON to JSON
  from-json   convert JSON to RON
  to-yaml     convert RON to YAML
  from-yaml   convert YAML to RON
  to-toml     convert RON to TOML
  from-toml   convert TOML to RON
  get         print the value at a path, such as items[0].name
  version     print the version

Without file arguments, commands read from standard input.
Run "rustyon <command> -h" for the flags of a command.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the CLI and returns the process exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "fmt":
		err = cmdFmt(rest, stdin, stdout, stderr)
	case "validate":
		err = cmdValidate(rest, stdin, stdout, stderr)
	case "to-json":
		err = cmdToJSON(rest, stdin, stdout, stderr)
	case "from-json":
		err = cmdFromJSON(rest, stdin, stdout, stderr)
	case "to-yaml":
		err = cmdToYAML(rest, stdin, stdout, stderr)
	case "from-yaml":
		err = cmdFromText("from-yaml", yamlToRON, rest, stdin, stdout, stderr)
	case "to-toml":
		err = cmdToTOML(rest, stdin, stdout, stderr)
	case "from-toml":
		err = cmdFromText("from-toml", tomlToRON, rest, stdin, stdout, stderr)
	case "get":
		err = cmdGet(rest, stdin, stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "rustyon", buildVersion())
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "rustyon: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errUsage):
		return 2
	case errors.Is(err, errReported):
		return 1
	default:
		fmt.Fprintln(stderr, "rustyon:", err)
		return 1
	}
}

var (
	errUsage    = errors.New("usage error")
	errReported = errors.New("errors were reported")
)

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

func newFlagSet(name, synopsis string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: rustyon %s\n\nFlags:\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errUsage
	}
	return nil
}

type input struct {
	name string
	data []byte
}

// readInputs reads the named files, or standard input when there are none.
func readInputs(names []string, stdin io.Reader) ([]input, error) {
	if len(names) == 0 {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		return []input{{name: "<stdin>", data: data}}, nil
	}
	inputs := make([]input, 0, len(names))
	for _, n := range names {
		data, err := os.ReadFile(n)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input{name: n, data: data})
	}
	return inputs, nil
}

func cmdFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("fmt", "fmt [-w] [-l] [-indent str] [file ...]", stderr)
	write := fs.Bool("w", false, "write the result to the file instead of standard output")
	list := fs.Bool("l", false, "list files whose formatting differs")
	indent := fs.String("indent", "    ", "indentation for each nesting level")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *write && fs.NArg() == 0 {
		fmt.Fprintln(stderr, "rustyon fmt: -w requires file arguments")
		return errUsage
	}
	inputs, err := readInputs(fs.Args(), stdin)
	if err != nil {
		return err
	}
	failed := false
	for _, in := range inputs {
		out, err := rustyon.FormatIndent(in.data, *indent)
		if err != nil {
			reportError(stderr, in, err)
			failed = true
			continue
		}
		changed := !bytes.Equal(in.data, out)
		if *list && changed {
			fmt.Fprintln(stdout, in.name)
		}
		if *write {
			if changed {
				info, err := os.Stat(in.name)
				if err != nil {
					return err
				}
				if err := os.WriteFile(in.name, out, info.Mode().Perm()); err != nil {
					return err
				}
			}
			continue
		}
		if !*list {
			if _, err := stdout.Write(out); err != nil {
				return err
			}
		}
	}
	if failed {
		return errReported
	}
	return nil
}

func cmdValidate(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("validate", "validate [-q] [file ...]", stderr)
	quiet := fs.Bool("q", false, "only report errors")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	inputs, err := readInputs(fs.Args(), stdin)
	if err != nil {
		return err
	}
	failed := false
	for _, in := range inputs {
		if _, err := rustyon.ParseDocument(in.data); err != nil {
			reportError(stderr, in, err)
			failed = true
		} else if !*quiet {
			fmt.Fprintf(stdout, "%s: ok\n", in.name)
		}
	}
	if failed {
		return errReported
	}
	return nil
}

func singleInput(fs *flag.FlagSet, stdin io.Reader, stderr io.Writer) (input, error) {
	if fs.NArg() > 1 {
		fmt.Fprintf(stderr, "rustyon %s: expected at most one file\n", fs.Name())
		return input{}, errUsage
	}
	inputs, err := readInputs(fs.Args(), stdin)
	if err != nil {
		return input{}, err
	}
	return inputs[0], nil
}

func cmdToJSON(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("to-json", "to-json [-indent str] [file]", stderr)
	indent := fs.String("indent", "  ", "indentation; empty for compact output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := singleInput(fs, stdin, stderr)
	if err != nil {
		return err
	}
	out, err := ronjson.ToJSON(in.data, *indent)
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	_, err = fmt.Fprintf(stdout, "%s\n", out)
	return err
}

func cmdToYAML(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("to-yaml", "to-yaml [-indent n] [file]", stderr)
	indent := fs.Int("indent", 2, "number of spaces per nesting level")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *indent < 1 {
		fmt.Fprintln(stderr, "rustyon to-yaml: -indent must be at least 1")
		return errUsage
	}
	in, err := singleInput(fs, stdin, stderr)
	if err != nil {
		return err
	}
	out, err := ronToYAML(in.data, *indent)
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	_, err = stdout.Write(out)
	return err
}

func cmdToTOML(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("to-toml", "to-toml [file]", stderr)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := singleInput(fs, stdin, stderr)
	if err != nil {
		return err
	}
	out, err := ronToTOML(in.data)
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	_, err = stdout.Write(out)
	return err
}

// cmdFromText implements from-yaml and from-toml, which take the same flags
// as from-json.
func cmdFromText(name string, convert func([]byte, ronjson.Options) ([]byte, error), args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet(name, name+" [-structs] [-indent str] [file]", stderr)
	structs := fs.Bool("structs", false, "write mappings with identifier keys as structs (key: value)")
	indent := fs.String("indent", "    ", "indentation; empty for compact output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := singleInput(fs, stdin, stderr)
	if err != nil {
		return err
	}
	out, err := convert(in.data, ronjson.Options{Structs: *structs, Indent: *indent})
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	_, err = fmt.Fprintf(stdout, "%s\n", out)
	return err
}

const pathHelp = `
Path syntax:
  .name       struct field, or map entry with a string or identifier key
  [N]         list or tuple element (negative N counts from the end),
              or map entry with integer key N
  ["key"]     map entry with a string key (Go escapes), or struct field
Steps chain: items[0].name, .config.port, players["alice"]. A leading dot is
optional; "." selects the whole document. Some(x) is looked through.
Exit status 1 means the path does not exist.
`

func cmdGet(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("get", "get [-indent str] path [file]", stderr)
	baseUsage := fs.Usage
	fs.Usage = func() {
		baseUsage()
		fmt.Fprint(stderr, pathHelp)
	}
	indent := fs.String("indent", "    ", "indentation; empty for compact output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, "rustyon get: expected a path and at most one file")
		return errUsage
	}
	steps, err := parsePath(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "rustyon get:", err)
		return errUsage
	}
	inputs, err := readInputs(fs.Args()[1:], stdin)
	if err != nil {
		return err
	}
	in := inputs[0]
	root, err := rustyon.Parse(in.data)
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	v, err := lookup(root, steps)
	if err != nil {
		fmt.Fprintf(stderr, "rustyon get: %s: %v\n", in.name, err)
		return errReported
	}
	out, err := valueText(v, *indent)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", out)
	return err
}

func cmdFromJSON(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet("from-json", "from-json [-structs] [-indent str] [file]", stderr)
	structs := fs.Bool("structs", false, "write objects with identifier keys as structs (key: value)")
	indent := fs.String("indent", "    ", "indentation; empty for compact output")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	in, err := singleInput(fs, stdin, stderr)
	if err != nil {
		return err
	}
	out, err := ronjson.FromJSON(in.data, ronjson.Options{Structs: *structs, Indent: *indent})
	if err != nil {
		reportError(stderr, in, err)
		return errReported
	}
	_, err = fmt.Fprintf(stdout, "%s\n", out)
	return err
}

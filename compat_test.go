// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package rustyon

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The compatibility suite compares rustyon with ron-rs through the oracle
// binary built from compat/ronrs. It is skipped unless RONRS_ORACLE names
// that binary. See compat/README or the compat Makefile target.

type oracle struct {
	mu   sync.Mutex
	path string
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
}

var (
	oracleOnce sync.Once
	theOracle  *oracle
)

func oraclePath(tb testing.TB) string {
	tb.Helper()
	path := os.Getenv("RONRS_ORACLE")
	if path == "" {
		tb.Skip("RONRS_ORACLE not set")
	}
	return path
}

func getOracle(tb testing.TB) *oracle {
	tb.Helper()
	path := oraclePath(tb)
	oracleOnce.Do(func() { theOracle = &oracle{path: path} })
	return theOracle
}

func (o *oracle) start() error {
	cmd := exec.Command(o.path, "serve")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	o.cmd, o.in, o.out = cmd, in, bufio.NewReaderSize(out, 1<<20)
	return nil
}

func (o *oracle) stop() {
	if o.cmd != nil {
		_ = o.in.Close()
		_ = o.cmd.Process.Kill()
		_ = o.cmd.Wait()
		o.cmd = nil
	}
}

// ask sends one document to ron-rs and returns whether it was accepted and
// the canonical form or error message.
func (o *oracle) ask(doc []byte) (bool, string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if o.cmd == nil {
			if err := o.start(); err != nil {
				return false, "", err
			}
		}
		_, err := fmt.Fprintf(o.in, "%d\n", len(doc))
		if err == nil {
			_, err = o.in.Write(doc)
		}
		var line string
		if err == nil {
			line, err = o.out.ReadString('\n')
		}
		if err != nil {
			o.stop()
			continue
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "OK "):
			return true, line[3:], nil
		case strings.HasPrefix(line, "ERR"):
			return false, strings.TrimPrefix(line, "ERR "), nil
		}
		return false, "", fmt.Errorf("unexpected oracle answer %q", line)
	}
	return false, "", fmt.Errorf("oracle crashed on %q", doc)
}

// canon projects a rustyon value into the canonical form printed by the
// oracle for ron::Value.
func canon(v Value) string {
	var sb strings.Builder
	writeCanon(&sb, v)
	return sb.String()
}

func hexOf(s string) string { return hex.EncodeToString([]byte(s)) }

func writeCanon(sb *strings.Builder, v Value) {
	switch v := v.(type) {
	case Unit, Ident:
		sb.WriteString("unit")
	case Bool:
		sb.WriteString(strconv.FormatBool(bool(v)))
	case Char:
		fmt.Fprintf(sb, "char(%d)", rune(v))
	case String:
		sb.WriteString("str(" + hexOf(string(v)) + ")")
	case Bytes:
		sb.WriteString("bytes(" + hexOf(string(v)) + ")")
	case Int:
		sb.WriteString("int(" + v.String() + ")")
	case Float:
		if math.IsNaN(float64(v)) {
			sb.WriteString("float(nan)")
		} else {
			fmt.Fprintf(sb, "float(%016x)", math.Float64bits(float64(v)))
		}
	case Option:
		if v.Value == nil {
			sb.WriteString("none")
		} else {
			sb.WriteString("some(")
			writeCanon(sb, v.Value)
			sb.WriteString(")")
		}
	case List:
		writeSeq(sb, v)
	case Tuple:
		writeSeq(sb, v.Values)
	case Map:
		entries := make([][2]Value, len(v))
		for i, e := range v {
			entries[i] = [2]Value{e.Key, e.Value}
		}
		writeMap(sb, entries)
	case Struct:
		entries := make([][2]Value, len(v.Fields))
		for i, f := range v.Fields {
			entries[i] = [2]Value{String(f.Name), f.Value}
		}
		writeMap(sb, entries)
	default:
		sb.WriteString("?")
	}
}

func writeSeq(sb *strings.Builder, vs []Value) {
	sb.WriteString("seq[")
	for i, e := range vs {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeCanon(sb, e)
	}
	sb.WriteByte(']')
}

// writeMap mirrors the indexmap semantics of ron: a repeated key keeps its
// first position and takes the last value.
func writeMap(sb *strings.Builder, entries [][2]Value) {
	keys := make([]string, 0, len(entries))
	vals := map[string]string{}
	for _, e := range entries {
		k := canon(e[0])
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k] = canon(e[1])
	}
	sb.WriteString("map{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(k + ":" + vals[k])
	}
	sb.WriteByte('}')
}

// compareDoc returns a description of a divergence between rustyon and
// ron-rs for doc, or "" when they agree.
func compareDoc(o *oracle, doc []byte) (string, error) {
	send := doc
	if strings.HasPrefix(string(doc), "\xEF\xBB\xBF") {
		// rustyon tolerates a leading byte order mark, ron-rs does not.
		send = doc[3:]
	}
	ok, want, err := o.ask(send)
	if err != nil {
		return "", err
	}
	v, perr := Parse(doc)
	switch {
	case ok && perr != nil:
		return fmt.Sprintf("ron-rs accepts, rustyon rejects (%v)\nron-rs: %s", perr, want), nil
	case !ok && perr == nil:
		return fmt.Sprintf("ron-rs rejects (%s), rustyon accepts\nrustyon: %s", want, canon(v)), nil
	case !ok:
		return "", nil
	}
	if strings.Contains(want, "!dup") {
		return "", nil
	}
	if got := canon(v); got != want {
		return fmt.Sprintf("canonical forms differ\nron-rs:  %s\nrustyon: %s", want, got), nil
	}
	return "", nil
}

// loadCorpus reads testdata/compat/*.txt. Cases are separated by a line that
// holds only %%%%.
func loadCorpus(tb testing.TB) [][]byte {
	tb.Helper()
	files, err := filepath.Glob("testdata/compat/*.txt")
	if err != nil {
		tb.Fatal(err)
	}
	var cases [][]byte
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			tb.Fatal(err)
		}
		for _, c := range strings.Split(string(b), "\n%%%%\n") {
			cases = append(cases, []byte(strings.TrimSuffix(c, "\n")))
		}
	}
	return cases
}

// rawCases holds documents that are awkward to write in the corpus files.
var rawCases = []string{
	"\xff", "[\"a\xffb\"]", "\xEF\xBB\xBF1", "\xEF\xBB\xBF", "\x00", "1\x00", "\"a\x00b\"",
	" 1", " 1", "‎1", "\u00851", "\v1\f", "\r\n1\r\n", "1\r\n// c\r\n",
	"'\n'", "'\r'", "\"\r\n\"", "\"a\\\r\nb\"", "\"a\\\rb\"", "\"\\\n\"", "\"\\\n  \t\n  x\"",
	"r\"a\r\nb\"", "é", "r#é", "(é: 1)", "(日本: 1)", "日本", "Ünï", "x́", "́x",
	"·", "a·b", "(a·b: 1)", "℘", "a℘", "ͺ", "゛", "ﾞ",
	"\U0001F600", "'\U0001F600'", "\"\U0001F600\"", "b\"\U0001F600\"", "br\"\U0001F600\"",
	"b'\U0001F600'", "b'é'", "֋", "a୓", "(֋: 1)", "r#֋",
}

func TestCompatCorpus(t *testing.T) {
	o := getOracle(t)
	cases := loadCorpus(t)
	for _, c := range rawCases {
		cases = append(cases, []byte(c))
	}
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, doc := range cases {
		msg, err := compareDoc(o, doc)
		if err != nil {
			t.Fatal(err)
		}
		if msg != "" {
			t.Errorf("document %q: %s", doc, msg)
		}
	}
	t.Logf("%d documents compared", len(cases))
}

func FuzzCompat(f *testing.F) {
	path := os.Getenv("RONRS_ORACLE")
	for _, s := range fuzzSeeds {
		f.Add([]byte(s))
	}
	for _, c := range rawCases {
		f.Add([]byte(c))
	}
	for _, c := range loadCorpus(f) {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if path == "" {
			t.Skip("RONRS_ORACLE not set")
		}
		o := getOracle(t)
		msg, err := compareDoc(o, data)
		if err != nil {
			t.Fatal(err)
		}
		if msg != "" {
			t.Fatalf("document %q: %s", data, msg)
		}
	})
}

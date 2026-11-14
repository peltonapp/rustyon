// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"strings"
	"testing"
)

const getDoc = `Game(
    config: (port: 8080, host: "localhost"),
    items: [(name: "sword", tags: ["a", "b"]), (name: "shield", tags: [])],
    players: {"alice": 10, "bob": 20, "we\"ird": 1},
    levels: {1: "one", 2: "two"},
    pair: (1, "x"),
    maybe: Some((deep: true)),
    none: None,
    colors: {Red: 1},
)`

func TestGet(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"items[0].name", "\"sword\""},
		{".config.port", "8080"},
		{"config.host", "\"localhost\""},
		{`players["alice"]`, "10"},
		{`.players["we\"ird"]`, "1"},
		{`players.bob`, "20"},
		{"items[-1].name", "\"shield\""},
		{"items[0].tags[1]", "\"b\""},
		{"levels[2]", "\"two\""},
		{"pair[1]", "\"x\""},
		{"maybe.deep", "true"},
		{"colors.Red", "1"},
		{`config["port"]`, "8080"},
		{"items[1].tags", "[]"},
		{"none", "None"},
		{"config", "(\n    port: 8080,\n    host: \"localhost\",\n)"},
	}
	for _, tt := range tests {
		code, out, errOut := runCLI(t, getDoc, "get", tt.path)
		if code != 0 || out != tt.want+"\n" {
			t.Errorf("%s: code %d, out %q, stderr %q, want %q", tt.path, code, out, errOut, tt.want)
		}
	}
	code, out, _ := runCLI(t, getDoc, "get", "-indent", "", "config")
	if code != 0 || out != "(port:8080,host:\"localhost\")\n" {
		t.Errorf("compact: code %d, out %q", code, out)
	}
	code, out, _ = runCLI(t, "[1, 2]", "get", ".")
	if code != 0 || out != "[\n    1,\n    2,\n]\n" {
		t.Errorf("whole document: code %d, out %q", code, out)
	}
}

func TestGetNotFound(t *testing.T) {
	for _, path := range []string{
		"items[2]", "items[-3]", "items[0].price", `players["carol"]`, "levels[3]",
		"none.x", "config.port.x", "config[0]", "pair[2]", "items.name",
	} {
		code, out, errOut := runCLI(t, getDoc, "get", path)
		if code != 1 || out != "" || !strings.Contains(errOut, "not found") {
			t.Errorf("%s: code %d, out %q, stderr %q", path, code, out, errOut)
		}
	}
}

func TestGetUsage(t *testing.T) {
	for _, path := range []string{"items[", "items[x]", `players["a`, `players["a"`, "a..b", "a.", "[0]]", "a]"} {
		if code, _, errOut := runCLI(t, getDoc, "get", path); code != 2 || !strings.Contains(errOut, "invalid path") {
			t.Errorf("%q: code %d, stderr %q", path, code, errOut)
		}
	}
	if code, _, _ := runCLI(t, getDoc, "get"); code != 2 {
		t.Errorf("no path: code %d", code)
	}
	if code, _, _ := runCLI(t, getDoc, "get", "a", "b", "c"); code != 2 {
		t.Errorf("too many arguments: code %d", code)
	}
	code, _, errOut := runCLI(t, "", "get", "-h")
	if code != 0 || !strings.Contains(errOut, "Path syntax") {
		t.Errorf("-h: code %d, stderr %q", code, errOut)
	}
}

func TestGetFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/g.ron"
	if err := os.WriteFile(path, []byte("(a: [1, 2])"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI(t, "", "get", "a[1]", path)
	if code != 0 || out != "2\n" {
		t.Errorf("code %d, out %q", code, out)
	}
	if code, _, _ := runCLI(t, "", "get", "a", dir+"/missing.ron"); code != 1 {
		t.Errorf("missing file: code %d", code)
	}
}

func TestParsePath(t *testing.T) {
	steps, err := parsePath(`.a.b[3]["c d"].e`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range steps {
		got = append(got, s.text)
	}
	if strings.Join(got, "") != `.a.b[3]["c d"].e` || len(steps) != 5 {
		t.Errorf("steps %v", got)
	}
	if steps, err := parsePath(""); err != nil || len(steps) != 0 {
		t.Errorf("empty: %v, %v", steps, err)
	}
}

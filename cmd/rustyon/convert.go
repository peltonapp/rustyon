// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"github.com/peltonapp/rustyon-go"
	"github.com/peltonapp/rustyon-go/ronjson"
)

// RON to YAML and TOML goes through the JSON mapping of package ronjson, so
// all three formats lose the same information (struct and variant names,
// chars and so on). JSON is valid YAML, which lets the YAML encoder keep the
// field order of the RON document.

// ronToYAML converts a RON document to block style YAML.
func ronToYAML(src []byte, indent int) ([]byte, error) {
	js, err := ronjson.ToJSON(src, "")
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(js, &doc); err != nil {
		return nil, err
	}
	clearStyle(&doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// clearStyle drops the flow and quoting styles inherited from the JSON
// source so the encoder picks the usual block style.
func clearStyle(n *yaml.Node) {
	n.Style = 0
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		// YAML 1.1 parsers read these as booleans, so keep them quoted.
		switch strings.ToLower(n.Value) {
		case "y", "yes", "n", "no", "on", "off":
			n.Style = yaml.DoubleQuotedStyle
		}
	}
	for _, c := range n.Content {
		clearStyle(c)
	}
}

// yamlToRON converts a YAML document to RON. Only the first document of a
// stream is converted; more than one is an error.
func yamlToRON(src []byte, opts ronjson.Options) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty YAML document")
		}
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, errors.New("YAML input has more than one document")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	var buf bytes.Buffer
	if err := writeYAMLNode(&buf, &doc, 0); err != nil {
		return nil, err
	}
	return ronjson.FromJSON(buf.Bytes(), opts)
}

// writeYAMLNode writes n as JSON, keeping mapping order.
func writeYAMLNode(buf *bytes.Buffer, n *yaml.Node, depth int) error {
	if depth > 10000 {
		return errors.New("YAML nesting is too deep")
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return writeYAMLNode(buf, n.Content[0], depth)
	case yaml.AliasNode:
		return writeYAMLNode(buf, n.Alias, depth+1)
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeYAMLNode(buf, c, depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case yaml.MappingNode:
		buf.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, err := yamlKey(n.Content[i])
			if err != nil {
				return err
			}
			kb, _ := json.Marshal(key)
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeYAMLNode(buf, n.Content[i+1], depth+1); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case yaml.ScalarNode:
		return writeYAMLScalar(buf, n)
	}
	return fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

func yamlKey(n *yaml.Node) (string, error) {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("line %d: only scalar mapping keys are supported", n.Line)
	}
	return n.Value, nil
}

func writeYAMLScalar(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.ShortTag() {
	case "!!null":
		buf.WriteString("null")
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return err
		}
		fmt.Fprintf(buf, "%t", b)
	case "!!int", "!!float":
		if n.ShortTag() == "!!float" && !isDigits(n.Value) {
			return writeYAMLFloat(buf, n)
		}
		// yaml.v3 resolves integers beyond 64 bits as floats, so plain
		// digits are read as integers either way.
		i, ok := new(big.Int).SetString(strings.ReplaceAll(n.Value, "_", ""), 0)
		if !ok {
			return fmt.Errorf("line %d: invalid integer %q", n.Line, n.Value)
		}
		buf.WriteString(i.String())
	default:
		// Strings, timestamps, binary data and unknown tags keep their text.
		b, _ := json.Marshal(n.Value)
		buf.Write(b)
	}
	return nil
}

func writeYAMLFloat(buf *bytes.Buffer, n *yaml.Node) error {
	var f float64
	if err := n.Decode(&f); err != nil {
		return err
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("line %d: %s cannot be converted", n.Line, n.Value)
	}
	b, _ := json.Marshal(f)
	buf.Write(b)
	return nil
}

// isDigits reports whether s is an optionally signed run of digits.
func isDigits(s string) bool {
	s = strings.TrimLeft(s, "+-")
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// ronToTOML converts a RON document to TOML. TOML has no null and needs a
// table at the top level.
func ronToTOML(src []byte) ([]byte, error) {
	js, err := ronjson.ToJSON(src, "")
	if err != nil {
		return nil, err
	}
	tree, err := decodeJSONTree(js)
	if err != nil {
		return nil, err
	}
	table, ok := tree.(map[string]any)
	if !ok {
		return nil, errors.New("TOML needs a struct or map at the top level")
	}
	if err := dropNulls(table, ""); err != nil {
		return nil, err
	}
	return toml.Marshal(table)
}

// decodeJSONTree decodes JSON into maps, slices, int64 and float64.
func decodeJSONTree(js []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalizeNumbers(v)
}

func normalizeNumbers(v any) (any, error) {
	switch v := v.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		if strings.ContainsAny(v.String(), ".eE") {
			f, err := v.Float64()
			return f, err
		}
		return nil, fmt.Errorf("integer %s does not fit into 64 bits, which TOML requires", v)
	case []any:
		for i := range v {
			x, err := normalizeNumbers(v[i])
			if err != nil {
				return nil, err
			}
			v[i] = x
		}
	case map[string]any:
		for k := range v {
			x, err := normalizeNumbers(v[k])
			if err != nil {
				return nil, err
			}
			v[k] = x
		}
	}
	return v, nil
}

// dropNulls removes null table entries, since TOML expresses None by
// leaving a key out. A null inside an array cannot be expressed.
func dropNulls(m map[string]any, path string) error {
	for k, v := range m {
		p := k
		if path != "" {
			p = path + "." + k
		}
		switch v := v.(type) {
		case nil:
			delete(m, k)
		case map[string]any:
			if err := dropNulls(v, p); err != nil {
				return err
			}
		case []any:
			if err := checkArray(v, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkArray(a []any, path string) error {
	for i, v := range a {
		p := fmt.Sprintf("%s[%d]", path, i)
		switch v := v.(type) {
		case nil:
			return fmt.Errorf("%s: TOML cannot represent null (None or unit) inside an array", p)
		case map[string]any:
			if err := dropNulls(v, p); err != nil {
				return err
			}
		case []any:
			if err := checkArray(v, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// tomlToRON converts a TOML document to RON. Table keys come out sorted,
// because TOML tables are unordered. Dates and times become strings.
func tomlToRON(src []byte, opts ronjson.Options) ([]byte, error) {
	var tree map[string]any
	if err := toml.Unmarshal(src, &tree); err != nil {
		return nil, err
	}
	js, err := json.Marshal(jsonSafe(tree))
	if err != nil {
		return nil, err
	}
	return ronjson.FromJSON(js, opts)
}

// jsonSafe replaces TOML date and time values with their text form.
func jsonSafe(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k := range v {
			v[k] = jsonSafe(v[k])
		}
	case []any:
		for i := range v {
			v[i] = jsonSafe(v[i])
		}
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case fmt.Stringer:
		return v.String()
	}
	return v
}

// valueText prints a RON value in the pretty style used by the CLI.
func valueText(v rustyon.Value, indent string) ([]byte, error) {
	if indent == "" {
		return rustyon.Marshal(v)
	}
	return rustyon.MarshalIndent(v, "", indent)
}

// Package test262 runs the tc39/test262 conformance suite against moejs.
// See README.md for the commands.
package test262

import (
	"errors"
	"strings"
)

// Meta is the part of a test's YAML frontmatter the runner uses.
type Meta struct {
	Includes []string
	Flags    []string
	Features []string
	Negative *Negative
}

// Negative is the frontmatter's negative: {phase, type}.
type Negative struct {
	Phase string // parse, resolution or runtime
	Type  string // the expected error constructor name
}

// HasFlag reports whether the frontmatter lists flag.
func (m *Meta) HasFlag(flag string) bool {
	for _, f := range m.Flags {
		if f == flag {
			return true
		}
	}
	return false
}

// ParseMeta extracts the frontmatter between "/*---" and "---*/" from a
// test's source. It understands the YAML subset test262 uses for the keys
// it reads: flow lists ([a, b], possibly over several lines), block lists
// (- a), and the two-key negative map. Other keys, including block scalars
// such as description and info, are skipped by their indentation.
func ParseMeta(src string) (*Meta, error) {
	start := strings.Index(src, "/*---")
	if start < 0 {
		return nil, errors.New("no frontmatter")
	}
	end := strings.Index(src[start:], "---*/")
	if end < 0 {
		return nil, errors.New("unterminated frontmatter")
	}
	// Any of CRLF, CR and LF ends a line (a few tests use CR alone).
	lines := strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(src[start+len("/*---"):start+end]), "\n")
	m := &Meta{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue // blank, nested under an earlier key, or a comment
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "includes", "flags", "features":
			var items []string
			var err error
			items, i, err = parseList(lines, i, value)
			if err != nil {
				return nil, errors.New(key + ": " + err.Error())
			}
			switch key {
			case "includes":
				m.Includes = items
			case "flags":
				m.Flags = items
			default:
				m.Features = items
			}
		case "negative":
			n := &Negative{}
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") {
				i++
				k, v, _ := strings.Cut(strings.TrimSpace(lines[i]), ":")
				switch k {
				case "phase":
					n.Phase = unquote(strings.TrimSpace(v))
				case "type":
					n.Type = unquote(strings.TrimSpace(v))
				}
			}
			if n.Phase == "" || n.Type == "" {
				return nil, errors.New("negative: phase and type are required")
			}
			m.Negative = n
		}
	}
	return m, nil
}

// parseList reads the list value of the key on lines[i]: a flow list that
// starts in value, or block items on the following lines. It returns the
// index of the last line it consumed.
func parseList(lines []string, i int, value string) ([]string, int, error) {
	if value == "" {
		var items []string
		for i+1 < len(lines) {
			t := strings.TrimSpace(lines[i+1])
			if !strings.HasPrefix(t, "- ") && t != "-" {
				break
			}
			i++
			if item := unquote(strings.TrimSpace(strings.TrimPrefix(t, "-"))); item != "" {
				items = append(items, item)
			}
		}
		return items, i, nil
	}
	if value[0] != '[' {
		return nil, i, errors.New("expected a list, got " + value)
	}
	for !strings.Contains(value, "]") {
		if i+1 >= len(lines) {
			return nil, i, errors.New("unterminated flow list")
		}
		i++
		value += " " + strings.TrimSpace(lines[i])
	}
	body := value[1:strings.Index(value, "]")]
	var items []string
	for _, item := range strings.Split(body, ",") {
		if item = unquote(strings.TrimSpace(item)); item != "" {
			items = append(items, item)
		}
	}
	return items, i, nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

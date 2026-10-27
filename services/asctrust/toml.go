// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Line-based reading and editing of the few flat keys of a policy file that the protection switches and the
// secret allowlist change: `[signing] require_signed`, `[secrets] block_on_push`, `[secrets] allow_paths`, and
// the keys the page only reads. The editor never rewrites a file: it replaces the lines of one key, or adds the
// key to its section, or adds the section, and leaves every other byte alone (comments and order included).
//
// This is not a TOML parser and it is not the authority. asc parses the result strictly in Policy.Validate and
// Policy.Propose; a file this code damages is refused there and never lands. The reader exists to draw the
// protection switches and the "what this means" lines for a draft that asc has not answered a field for.

// tomlKey is one `key = value` entry: the lines it covers (inclusive) and the section it is in.
type tomlKey struct {
	section    string
	name       string
	start, end int
	valueFrom  int // byte offset of the value in lines[start]
}

type tomlHeader struct {
	name string
	line int
}

type tomlDoc struct {
	lines   []string
	keys    []tomlKey
	headers []tomlHeader
}

var (
	headerPattern = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)
	keyPattern    = regexp.MustCompile(`^\s*("(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_.-]+)\s*=\s*`)
)

// normalizeSection makes `[ paths . "infra/**" ]` and `[paths."infra/**"]` the same name.
func normalizeSection(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func parseToml(text string) *tomlDoc {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	d := &tomlDoc{lines: strings.Split(text, "\n")}
	section := ""
	for i := 0; i < len(d.lines); i++ {
		line := d.lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := headerPattern.FindStringSubmatch(line); m != nil && !strings.HasPrefix(trimmed, "[[") {
			section = normalizeSection(m[1])
			d.headers = append(d.headers, tomlHeader{name: section, line: i})
			continue
		}
		loc := keyPattern.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		name := strings.Trim(line[loc[2]:loc[3]], `"'`)
		end := valueEnd(d.lines, i, loc[1])
		d.keys = append(d.keys, tomlKey{section: section, name: name, start: i, end: end, valueFrom: loc[1]})
		i = end
	}
	return d
}

// valueEnd returns the index of the last line of the value that starts at byte from of lines[i]. It follows
// strings, multi-line strings, comments and the nesting of arrays and tables.
func valueEnd(lines []string, i, from int) int {
	const (
		none = iota
		basic
		literal
		mlBasic
		mlLiteral
	)
	state, depth := none, 0
	for ; i < len(lines); i++ {
		line := lines[i]
		j := 0
		if from > 0 {
			j, from = from, 0
		}
		for ; j < len(line); j++ {
			c := line[j]
			switch state {
			case none:
				switch {
				case c == '#':
					j = len(line)
				case strings.HasPrefix(line[j:], `"""`):
					state, j = mlBasic, j+2
				case strings.HasPrefix(line[j:], `'''`):
					state, j = mlLiteral, j+2
				case c == '"':
					state = basic
				case c == '\'':
					state = literal
				case c == '[' || c == '{':
					depth++
				case c == ']' || c == '}':
					depth--
				}
			case basic:
				if c == '\\' {
					j++
				} else if c == '"' {
					state = none
				}
			case literal:
				if c == '\'' {
					state = none
				}
			case mlBasic:
				if c == '\\' {
					j++
				} else if strings.HasPrefix(line[j:], `"""`) {
					state, j = none, j+2
				}
			case mlLiteral:
				if strings.HasPrefix(line[j:], `'''`) {
					state, j = none, j+2
				}
			}
		}
		if state == basic || state == literal {
			state = none // a single-line string never continues; the file is malformed and asc refuses it
		}
		if state == none && depth <= 0 {
			return i
		}
	}
	return len(lines) - 1
}

func (d *tomlDoc) find(section, key string) (tomlKey, bool) {
	section = normalizeSection(section)
	for _, k := range d.keys {
		if k.section == section && k.name == key {
			return k, true
		}
	}
	return tomlKey{}, false
}

// raw returns the text of a key's value, comments removed on the single-line form.
func (d *tomlDoc) raw(k tomlKey) string {
	parts := make([]string, 0, k.end-k.start+1)
	for i := k.start; i <= k.end; i++ {
		line := d.lines[i]
		if i == k.start {
			line = line[k.valueFrom:]
		}
		parts = append(parts, line)
	}
	return stripComment(strings.Join(parts, "\n"))
}

// stripComment removes the `# ...` comments outside strings.
func stripComment(s string) string {
	var b strings.Builder
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			b.WriteByte(c)
			if c == '\\' && inStr == '"' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == inStr {
				inStr = 0
			}
		case c == '"' || c == '\'':
			inStr = c
			b.WriteByte(c)
		case c == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			if i < len(s) {
				b.WriteByte('\n')
			}
		default:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

// TomlBool reads a boolean key of a section ("" is the top level).
func TomlBool(text, section, key string) (value, found bool) {
	d := parseToml(text)
	k, ok := d.find(section, key)
	if !ok {
		return false, false
	}
	switch d.raw(k) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// TomlInt reads a non-negative integer key of a section.
func TomlInt(text, section, key string) (value uint64, found bool) {
	d := parseToml(text)
	k, ok := d.find(section, key)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.ReplaceAll(d.raw(k), "_", ""), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// TomlFloat reads a number key of a section.
func TomlFloat(text, section, key string) (value float64, found bool) {
	d := parseToml(text)
	k, ok := d.find(section, key)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(d.raw(k), "_", ""), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// TomlStrings reads an array of strings of a section. Only basic and literal strings are read.
func TomlStrings(text, section, key string) (values []string, found bool) {
	d := parseToml(text)
	k, ok := d.find(section, key)
	if !ok {
		return nil, false
	}
	raw := d.raw(k)
	if !strings.HasPrefix(raw, "[") {
		return nil, false
	}
	for i := 1; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			var b strings.Builder
			i++
			for ; i < len(raw) && raw[i] != '"'; i++ {
				if raw[i] == '\\' && i+1 < len(raw) {
					i++
					switch raw[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(raw[i])
					}
					continue
				}
				b.WriteByte(raw[i])
			}
			values = append(values, b.String())
		case '\'':
			j := strings.IndexByte(raw[i+1:], '\'')
			if j < 0 {
				return values, true
			}
			values = append(values, raw[i+1:i+1+j])
			i += j + 1
		}
	}
	return values, true
}

// tomlQuote returns s as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// setLine replaces the lines of key, or adds `key = rendered` to the section, or adds the section.
func setLine(text, section, key, rendered string) string {
	d := parseToml(text)
	section = normalizeSection(section)
	entry := key + " = " + rendered
	if k, ok := d.find(section, key); ok {
		lines := append(append(append([]string{}, d.lines[:k.start]...), entry), d.lines[k.end+1:]...)
		return strings.Join(lines, "\n")
	}
	if section == "" {
		// A top-level key goes before the first header.
		at := len(d.lines)
		if len(d.headers) > 0 {
			at = d.headers[0].line
		}
		for at > 0 && strings.TrimSpace(d.lines[at-1]) == "" {
			at--
		}
		lines := append(append(append([]string{}, d.lines[:at]...), entry), d.lines[at:]...)
		return strings.Join(lines, "\n")
	}
	for hi, h := range d.headers {
		if h.name != section {
			continue
		}
		end := len(d.lines)
		if hi+1 < len(d.headers) {
			end = d.headers[hi+1].line
		}
		at := end
		for at > h.line+1 && strings.TrimSpace(d.lines[at-1]) == "" {
			at--
		}
		lines := append(append(append([]string{}, d.lines[:at]...), entry), d.lines[at:]...)
		return strings.Join(lines, "\n")
	}
	out := strings.TrimRight(strings.Join(d.lines, "\n"), "\n")
	if out != "" {
		out += "\n\n"
	}
	return out + "[" + section + "]\n" + entry + "\n"
}

// TomlSetBool sets a boolean key of a section.
func TomlSetBool(text, section, key string, v bool) string {
	return setLine(text, section, key, strconv.FormatBool(v))
}

// TomlSetStrings sets an array of strings of a section, on one line. An empty list is written as `[]`.
func TomlSetStrings(text, section, key string, values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = tomlQuote(v)
	}
	return setLine(text, section, key, "["+strings.Join(quoted, ", ")+"]")
}

// Limits of a path pattern list (asc-core MAX_GLOBS, MAX_GLOB_LEN).
const (
	MaxGlobs   = 64
	MaxGlobLen = 256
)

// ValidatePathGlob checks one path pattern by the rules of asc-core's PathGlob::parse: relative, `/` separated,
// no empty, `.` or `..` segment, no control character, and `**` only as a whole segment. asc checks again.
func ValidatePathGlob(g string) error {
	if g == "" || len(g) > MaxGlobLen {
		return fmt.Errorf("%w: a path pattern is empty or longer than %d bytes", ErrBadInput, MaxGlobLen)
	}
	if strings.HasPrefix(g, "/") || strings.HasSuffix(g, "/") || strings.Contains(g, `\`) {
		return fmt.Errorf("%w: a path pattern is relative, uses / and has no trailing /", ErrBadInput)
	}
	for _, r := range g {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: a path pattern holds a control character", ErrBadInput)
		}
	}
	for _, seg := range strings.Split(g, "/") {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return fmt.Errorf("%w: a path pattern has an empty, . or .. segment", ErrBadInput)
		case seg != "**" && strings.Contains(seg, "**"):
			return fmt.Errorf("%w: ** must be a whole segment", ErrBadInput)
		}
	}
	return nil
}

// ParseGlobLines reads a textarea of path patterns, one per line, blank lines and surrounding space ignored. It
// refuses a list that is too long, a pattern that is not valid, and a repeated pattern is kept once.
func ParseGlobLines(text string) ([]string, error) {
	if len(text) > MaxGlobs*(MaxGlobLen+2) {
		return nil, fmt.Errorf("%w: the list is too long", ErrBadInput)
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		if err := ValidatePathGlob(line); err != nil {
			return nil, err
		}
		seen[line] = true
		out = append(out, line)
		if len(out) > MaxGlobs {
			return nil, fmt.Errorf("%w: at most %d patterns", ErrBadInput, MaxGlobs)
		}
	}
	return out, nil
}

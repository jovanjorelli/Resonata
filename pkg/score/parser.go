package score

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ParseJSON decodes a score from JSON data using encoding/json. Unknown
// fields are ignored; structural problems surface as decode errors.
func ParseJSON(data []byte) (*Score, error) {
	return decodeScore(data)
}

// ParseYAML decodes a score from YAML data. The supported subset covers
// what score files need:
//
//   - nested block mappings and sequences, indented or key-aligned
//   - single-line flow mappings {a: b} and sequences [1, 2]
//   - double/single-quoted and plain scalars with JSON-compatible types
//   - full-line and trailing comments, "---" start and "..." end markers
//
// Constructs outside the subset (tab indentation, anchors, aliases, tags,
// block scalars, multiple documents) are rejected with line-numbered
// errors. The subset is converted to JSON and decoded with encoding/json
// so both formats share identical semantics.
func ParseYAML(data []byte) (*Score, error) {
	root, err := yamlToValue(data)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	return decodeScore(encoded)
}

// ParseFile reads a score from path, choosing the parser by file
// extension: .yaml/.yml use YAML, everything else uses JSON.
func ParseFile(path string) (*Score, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return ParseYAML(data)
	default:
		return ParseJSON(data)
	}
}

// ConvertToSeconds converts a time expressed in beats to seconds at the
// given tempo. Non-positive tempos yield 0.
func ConvertToSeconds(noteTime float64, bpm float64) float64 {
	if bpm <= 0 {
		return 0
	}
	return noteTime * 60 / bpm
}

// decodeScore unmarshals JSON payload into a Score and applies the
// parse-time pipeline — transposition, timing offsets, swing, accents —
// so note pitches, times, and velocities are final before validation or
// rendering.
func decodeScore(data []byte) (*Score, error) {
	var s Score
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("score: %w", err)
	}
	s.ApplyTransposition()
	s.ApplyTimingOffsets()
	s.ApplySwing()
	s.ApplyAccents()
	return &s, nil
}

// yamlLine is one significant source line with its indentation resolved.
type yamlLine struct {
	indent int
	text   string
	num    int // 1-based source line number for error messages
}

// yamlParser walks the significant lines of a YAML document.
type yamlParser struct {
	lines []yamlLine
	pos   int
}

// yamlToValue parses the supported YAML subset into generic Go values
// (map[string]interface{}, []interface{}, string, int64, float64, bool,
// nil) ready for JSON encoding.
func yamlToValue(data []byte) (interface{}, error) {
	lines, err := splitYAMLLines(data)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	p := &yamlParser{lines: lines}
	v, err := p.parseBlock(lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		return nil, fmt.Errorf("yaml: line %d: unexpected content after end of block", ln.num)
	}
	return v, nil
}

// splitYAMLLines drops blank lines and comments, rejects tab indentation,
// and resolves the document start and end markers.
func splitYAMLLines(data []byte) ([]yamlLine, error) {
	var lines []yamlLine
	seenContent := false
	for i, raw := range strings.Split(string(data), "\n") {
		num := i + 1
		raw = strings.TrimRight(raw, "\r")
		indent := 0
		for indent < len(raw) && raw[indent] == ' ' {
			indent++
		}
		if indent < len(raw) && raw[indent] == '\t' {
			return nil, fmt.Errorf("yaml: line %d: tabs are not allowed in indentation", num)
		}
		text := strings.TrimSpace(stripComment(raw[indent:]))
		if text == "" || text[0] == '%' {
			continue // blank, comment-only, or directive line
		}
		switch text {
		case "---":
			if seenContent {
				return nil, fmt.Errorf("yaml: line %d: multiple documents are not supported", num)
			}
			continue
		case "...":
			return lines, nil
		}
		lines = append(lines, yamlLine{indent: indent, text: text, num: num})
		seenContent = true
	}
	return lines, nil
}

// stripComment removes a trailing "#" comment starting outside quotes. A
// "#" only opens a comment at the start of the text or after whitespace.
func stripComment(s string) string {
	var inSingle, inDouble bool
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case inDouble:
			if c == '\\' {
				i++
			} else if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '"':
			inDouble = true
		case c == '\'':
			inSingle = true
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

// isDashEntry reports whether a line starts a block sequence entry.
func isDashEntry(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "-\t")
}

// parseBlock parses the value starting at the current line, dispatching on
// its shape: sequence, mapping, or single scalar.
func (p *yamlParser) parseBlock(indent int) (interface{}, error) {
	ln := p.lines[p.pos]
	if isDashEntry(ln.text) {
		return p.parseSequence(indent)
	}
	if _, _, err := splitKey(ln.text); err == nil {
		return p.parseMapping(indent)
	}
	p.pos++
	return parseValueText(ln.text, ln.num)
}

// parseMapping consumes "key: value" lines at exactly the given indent.
func (p *yamlParser) parseMapping(indent int) (interface{}, error) {
	m := make(map[string]interface{})
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent < indent {
			break
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("yaml: line %d: unexpected indentation", ln.num)
		}
		if isDashEntry(ln.text) {
			return nil, fmt.Errorf("yaml: line %d: sequence entry where mapping key expected", ln.num)
		}
		key, rest, err := splitKey(ln.text)
		if err != nil {
			return nil, fmt.Errorf("yaml: line %d: %w", ln.num, err)
		}
		p.pos++
		var val interface{}
		if strings.TrimSpace(rest) == "" {
			val, err = p.parseNestedValue(indent)
		} else {
			val, err = parseValueText(rest, ln.num)
		}
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}

// parseSequence consumes "- item" lines at exactly the given indent. The
// content following a dash is rewritten as a line of its own so nested
// blocks parse with uniform indentation rules.
func (p *yamlParser) parseSequence(indent int) (interface{}, error) {
	seq := []interface{}{}
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent || !isDashEntry(ln.text) {
			break
		}
		rest := ln.text[1:]
		trimmed := strings.TrimLeft(rest, " \t")
		if trimmed == "" {
			// Bare dash: the item is a deeper block, or null.
			p.pos++
			var val interface{}
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				v, err := p.parseBlock(p.lines[p.pos].indent)
				if err != nil {
					return nil, err
				}
				val = v
			}
			seq = append(seq, val)
			continue
		}
		contentIndent := indent + 1 + len(rest) - len(trimmed)
		p.lines[p.pos] = yamlLine{indent: contentIndent, text: trimmed, num: ln.num}
		v, err := p.parseBlock(contentIndent)
		if err != nil {
			return nil, err
		}
		seq = append(seq, v)
	}
	return seq, nil
}

// parseNestedValue parses the block value of a key with no inline value:
// a deeper block, a sequence aligned with the key, or null.
func (p *yamlParser) parseNestedValue(parentIndent int) (interface{}, error) {
	if p.pos == len(p.lines) {
		return nil, nil
	}
	ln := p.lines[p.pos]
	if ln.indent > parentIndent || (ln.indent == parentIndent && isDashEntry(ln.text)) {
		return p.parseBlock(ln.indent)
	}
	return nil, nil
}

// splitKey splits "key: rest" at the first colon outside quotes and flow
// brackets that is followed by whitespace or end of line. Quoted keys are
// unquoted; non-string keys take their literal text form.
func splitKey(text string) (key, rest string, err error) {
	var inSingle, inDouble bool
	depth := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inDouble:
			if c == '\\' {
				i++
			} else if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '"':
			inDouble = true
		case c == '\'':
			inSingle = true
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		case c == ':' && depth == 0 && (i+1 == len(text) || text[i+1] == ' ' || text[i+1] == '\t'):
			key = strings.TrimSpace(text[:i])
			if key == "" {
				return "", "", errors.New("empty mapping key")
			}
			if v, kerr := parseScalarText(key, 0); kerr == nil && v != nil {
				if ks, ok := v.(string); ok {
					key = ks
				} else {
					key = fmt.Sprint(v)
				}
			}
			return key, text[i+1:], nil
		}
	}
	return "", "", errors.New("expected 'key: value'")
}

// parseValueText parses an inline mapping value or standalone scalar
// line, rejecting YAML constructs outside the supported subset.
func parseValueText(s string, lineNum int) (interface{}, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	switch s[0] {
	case '|', '>':
		return nil, fmt.Errorf("yaml: line %d: block scalars are not supported", lineNum)
	case '&', '*', '!':
		return nil, fmt.Errorf("yaml: line %d: anchors, aliases and tags are not supported", lineNum)
	case '[', '{':
		return parseFlow(s, lineNum)
	}
	return parseScalarText(s, lineNum)
}

// parseScalarText classifies a plain or quoted scalar as null, bool, int,
// float, or string, mirroring the types encoding/json produces.
func parseScalarText(s string, lineNum int) (interface{}, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	switch s[0] {
	case '"':
		v, err := strconv.Unquote(s)
		if err != nil {
			return nil, fmt.Errorf("yaml: line %d: invalid double-quoted string", lineNum)
		}
		return v, nil
	case '\'':
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return nil, fmt.Errorf("yaml: line %d: unterminated single-quoted string", lineNum)
		}
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	switch s {
	case "null", "Null", "NULL", "~":
		return nil, nil
	case "true", "True", "TRUE":
		return true, nil
	case "false", "False", "FALSE":
		return false, nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f, nil
	}
	return s, nil
}

// flowParser reads single-line flow collections ([seq], {map}) and the
// quoted or plain scalars inside them.
type flowParser struct {
	s    string
	i    int
	line int
}

// parseFlow parses a complete single-line flow value.
func parseFlow(s string, lineNum int) (interface{}, error) {
	f := &flowParser{s: s, line: lineNum}
	v, err := f.value()
	if err != nil {
		return nil, err
	}
	f.space()
	if f.i < len(f.s) {
		return nil, f.errorf("trailing content after flow value")
	}
	return v, nil
}

func (f *flowParser) errorf(format string, args ...interface{}) error {
	return fmt.Errorf("yaml: line %d: %s", f.line, fmt.Sprintf(format, args...))
}

func (f *flowParser) space() {
	for f.i < len(f.s) && (f.s[f.i] == ' ' || f.s[f.i] == '\t') {
		f.i++
	}
}

func (f *flowParser) peek(c byte) bool { return f.i < len(f.s) && f.s[f.i] == c }

// value parses one flow element: sequence, mapping, or scalar.
func (f *flowParser) value() (interface{}, error) {
	f.space()
	if f.i >= len(f.s) {
		return nil, f.errorf("unexpected end of flow value")
	}
	switch f.s[f.i] {
	case '[':
		f.i++
		seq := []interface{}{}
		f.space()
		if f.peek(']') {
			f.i++
			return seq, nil
		}
		for {
			v, err := f.value()
			if err != nil {
				return nil, err
			}
			seq = append(seq, v)
			f.space()
			switch {
			case f.peek(','):
				f.i++
			case f.peek(']'):
				f.i++
				return seq, nil
			default:
				return nil, f.errorf("expected ',' or ']' in flow sequence")
			}
		}
	case '{':
		f.i++
		m := map[string]interface{}{}
		f.space()
		if f.peek('}') {
			f.i++
			return m, nil
		}
		for {
			key, err := f.mappingKey()
			if err != nil {
				return nil, err
			}
			f.space()
			if !f.peek(':') {
				return nil, f.errorf("expected ':' after flow mapping key %q", key)
			}
			f.i++
			v, err := f.value()
			if err != nil {
				return nil, err
			}
			m[key] = v
			f.space()
			switch {
			case f.peek(','):
				f.i++
				f.space()
				if f.peek('}') { // tolerate a trailing comma
					f.i++
					return m, nil
				}
			case f.peek('}'):
				f.i++
				return m, nil
			default:
				return nil, f.errorf("expected ',' or '}' in flow mapping")
			}
		}
	default:
		return f.token()
	}
}

// token reads one quoted or plain scalar within a flow context. Plain
// tokens end at the first flow delimiter.
func (f *flowParser) token() (interface{}, error) {
	start := f.i
	if c := f.s[f.i]; c == '"' || c == '\'' {
		f.i++
		for f.i < len(f.s) {
			if c == '"' && f.s[f.i] == '\\' {
				f.i += 2
				continue
			}
			if f.s[f.i] == c {
				f.i++
				break
			}
			f.i++
		}
	} else {
		for f.i < len(f.s) && strings.IndexByte(",]}:", f.s[f.i]) < 0 {
			f.i++
		}
	}
	text := strings.TrimSpace(f.s[start:min(f.i, len(f.s))])
	return parseScalarText(text, f.line)
}

// mappingKey reads a flow mapping key (plain or quoted) up to its colon.
func (f *flowParser) mappingKey() (string, error) {
	f.space()
	if f.i >= len(f.s) {
		return "", f.errorf("unexpected end of flow mapping")
	}
	if c := f.s[f.i]; c == '"' || c == '\'' {
		v, err := f.token()
		if err != nil {
			return "", err
		}
		if v == nil {
			return "", f.errorf("empty flow mapping key")
		}
		if s, ok := v.(string); ok {
			return s, nil
		}
		return fmt.Sprint(v), nil
	}
	start := f.i
	for f.i < len(f.s) && f.s[f.i] != ':' {
		f.i++
	}
	key := strings.TrimSpace(f.s[start:f.i])
	if key == "" {
		return "", f.errorf("empty flow mapping key")
	}
	return key, nil
}

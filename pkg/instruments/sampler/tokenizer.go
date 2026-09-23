package sampler

import (
	"bytes"
	"io"
)

// Tokenizer is a state-machine byte scanner for SFZ text. It works
// directly on []byte subslices (no strings.Split, no regex): emitted
// keys and values alias the input, and the only buffer it owns is
// tokenBuf for lowercased dispatch keys.
//
// The scanner is deliberately fault-tolerant: comments in every common
// style are skipped, lone '/' typos and stray words are counted and
// dropped, and values may contain spaces because a value ends only at
// whitespace whose lookahead matches the next opcode, a header, a
// comment, or end of input.
type Tokenizer struct {
	data     []byte
	pos      int
	tokenBuf []byte // Pre-allocated buffer for token extraction

	// Diagnostics merged into ParseStats after scanning.
	Strays       int // stray bytes and word tokens skipped
	CommentSkips int // comment skips performed (lookahead rescans included)
}

// NewTokenizer returns a scanner over data. The slice must remain
// unmodified while scanning; returned tokens are subslices of it.
func NewTokenizer(data []byte) *Tokenizer {
	return &Tokenizer{data: data, tokenBuf: make([]byte, 0, 64)}
}

// Reset points the tokenizer at new data, reusing its buffer.
func (t *Tokenizer) Reset(data []byte) {
	t.data = data
	t.pos = 0
	t.tokenBuf = t.tokenBuf[:0]
	t.Strays = 0
	t.CommentSkips = 0
}

// NextOpcode extracts the next key=value pair, safely handling spaces in
// values via the opcode lookahead rule. Header tokens are returned with
// key set to the raw "<name>" text and a nil value. Stray bytes never
// produce errors; the scan ends with io.EOF.
func (t *Tokenizer) NextOpcode() (key []byte, value []byte, err error) {
	for {
		t.skipSpaceAndComments()
		if t.pos >= len(t.data) {
			return nil, nil, io.EOF
		}
		c := t.data[t.pos]
		switch {
		case c == '<':
			// Header token: consume through '>' (or to end of input).
			start := t.pos
			if i := bytes.IndexByte(t.data[t.pos:], '>'); i >= 0 {
				t.pos += i + 1
			} else {
				t.pos = len(t.data)
			}
			return t.data[start:t.pos], nil, nil
		case isKeyByte(c):
			start := t.pos
			for t.pos < len(t.data) && isKeyByte(t.data[t.pos]) {
				t.pos++
			}
			// Tolerate spaces around '=': "lokey = 60".
			scan := t.pos
			for scan < len(t.data) && (t.data[scan] == ' ' || t.data[scan] == '\t') {
				scan++
			}
			if scan < len(t.data) && t.data[scan] == '=' {
				key = t.data[start:t.pos]
				t.pos = scan + 1
				return key, t.scanValue(), nil
			}
			t.Strays++ // word without '=': drop it
		default:
			t.pos++ // stray byte (lone '/', punctuation, binary junk)
			t.Strays++
		}
	}
}

// LowerKey copies key into tokenBuf ASCII-lowercased and returns it. The
// result aliases tokenBuf and stays valid only until the next call;
// dispatch-map lookups via string(LowerKey(...)) do not allocate.
func (t *Tokenizer) LowerKey(key []byte) []byte {
	t.tokenBuf = append(t.tokenBuf[:0], key...)
	for i, c := range t.tokenBuf {
		if c >= 'A' && c <= 'Z' {
			t.tokenBuf[i] = c + 32
		}
	}
	return t.tokenBuf
}

// scanValue reads a value: everything up to whitespace whose lookahead
// (past further whitespace and comments) is another opcode, a header,
// or end of input. This keeps paths with spaces intact.
func (t *Tokenizer) scanValue() []byte {
	// Whitespace directly after '=' is not part of the value; if the
	// next meaningful token already ends a value, the value is empty.
	for t.pos < len(t.data) && isSpace(t.data[t.pos]) {
		t.pos++
	}
	if t.pos >= len(t.data) || t.data[t.pos] == '<' || t.atOpcodeStart() {
		return nil
	}
	start := t.pos
	end := t.pos
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		if isSpace(c) {
			save := t.pos
			t.skipSpaceAndComments()
			if t.pos >= len(t.data) || t.data[t.pos] == '<' || t.atOpcodeStart() {
				t.pos = save
				return t.data[start:end]
			}
			// The whitespace belongs to the value; continue.
			t.pos = save + 1
			continue
		}
		t.pos++
		end = t.pos
	}
	return t.data[start:end]
}

// atOpcodeStart reports whether the current position begins a valid
// opcode: [A-Za-z0-9_]+ followed by optional inline whitespace and '='.
func (t *Tokenizer) atOpcodeStart() bool {
	i := t.pos
	for i < len(t.data) && isKeyByte(t.data[i]) {
		i++
	}
	if i == t.pos {
		return false
	}
	for i < len(t.data) && (t.data[i] == ' ' || t.data[i] == '\t') {
		i++
	}
	return i < len(t.data) && t.data[i] == '='
}

// skipSpaceAndComments advances past whitespace and any comment style:
// "// line", "# line", "; line", "/* block */", and lone '/' typos.
// '#' and ';' only start comments at token boundaries, so they remain
// safe inside values such as "C#4 loud.wav".
func (t *Tokenizer) skipSpaceAndComments() {
	for t.pos < len(t.data) {
		c := t.data[t.pos]
		switch {
		case isSpace(c):
			t.pos++
		case c == '/' && t.pos+1 < len(t.data) && t.data[t.pos+1] == '/':
			t.skipLine()
			t.CommentSkips++
		case c == '/' && t.pos+1 < len(t.data) && t.data[t.pos+1] == '*':
			if i := bytes.Index(t.data[t.pos+2:], []byte("*/")); i >= 0 {
				t.pos += i + 4
			} else {
				t.pos = len(t.data) // unterminated block: swallow the rest
			}
			t.CommentSkips++
		case c == '#' || c == ';':
			t.skipLine()
			t.CommentSkips++
		case c == '/':
			t.pos++ // lone slash typo
			t.Strays++
		default:
			return
		}
	}
}

// skipLine advances past the end of the current line.
func (t *Tokenizer) skipLine() {
	for t.pos < len(t.data) && t.data[t.pos] != '\n' {
		t.pos++
	}
	if t.pos < len(t.data) {
		t.pos++
	}
}

// isSpace reports whether c is SFZ whitespace.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f'
}

// isKeyByte reports whether c may appear in an opcode key.
func isKeyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

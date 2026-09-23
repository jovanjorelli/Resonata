package score

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleJSON = `{
  "metadata": {"title": "Simple Melody", "bpm": 120, "time_signature": "4/4"},
  "tracks": [
    {
      "id": "track_1",
      "name": "Ocarina Solo",
      "instrument": {
        "type": "ocarina",
        "parameters": {"breath_noise": 0.3, "vibrato_rate": 5.0}
      },
      "pan": 0.0,
      "volume": 0.8,
      "reverb_send": 0.4,
      "eq": {
        "hpf": 80,
        "eq_low": {"freq": 150, "gain": 1.5, "q": 0.7},
        "eq_mid": {"freq": 300, "gain": -3, "q": 1.2},
        "eq_high": {"freq": 8000, "gain": 2, "q": 0.7}
      },
      "notes": [
        {"time": 0.0, "duration": 0.5, "pitch": 60, "velocity": 0.8},
        {"time": 0.5, "duration": 0.5, "pitch": 62, "velocity": 0.8},
        {"time": 1.0, "duration": 1.0, "pitch": 64, "velocity": 0.9,
         "articulation": {"type": "tenuto", "params": {"extra": 0.1}}}
      ]
    }
  ]
}`

const exampleYAML = `# Simple Melody
---
metadata:
  title: Simple Melody
  bpm: 120
  time_signature: 4/4 # common time

tracks:
  - id: track_1
    name: "Ocarina Solo"
    instrument:
      type: ocarina
      parameters: {breath_noise: 0.3, vibrato_rate: 5.0}
    pan: 0.0
    volume: 0.8
    reverb_send: 0.4
    eq:
      hpf: 80
      eq_low: {freq: 150, gain: 1.5, q: 0.7}
      eq_mid: {freq: 300, gain: -3, q: 1.2}
      eq_high: {freq: 8000, gain: 2, q: 0.7}
    notes:
      - {time: 0.0, duration: 0.5, pitch: 60, velocity: 0.8}
      - time: 0.5
        duration: 0.5
        pitch: 62
        velocity: 0.8
      - time: 1.0
        duration: 1.0
        pitch: 64
        velocity: 0.9
        articulation:
          type: tenuto
          params:
            extra: 0.1
...
`

// checkExample asserts the fully decoded content of the example score.
func checkExample(t *testing.T, s *Score) {
	t.Helper()
	if s.Metadata.Title != "Simple Melody" {
		t.Errorf("title = %q", s.Metadata.Title)
	}
	if s.Metadata.BPM != 120 {
		t.Errorf("bpm = %v", s.Metadata.BPM)
	}
	if s.Metadata.TimeSignature != "4/4" {
		t.Errorf("time_signature = %q", s.Metadata.TimeSignature)
	}
	if len(s.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(s.Tracks))
	}
	tr := s.Tracks[0]
	if tr.ID != "track_1" || tr.Name != "Ocarina Solo" {
		t.Errorf("track id/name = %q/%q", tr.ID, tr.Name)
	}
	if tr.Instrument.Type != "ocarina" {
		t.Errorf("instrument type = %q", tr.Instrument.Type)
	}
	if tr.Instrument.Parameters["breath_noise"] != 0.3 || tr.Instrument.Parameters["vibrato_rate"] != 5 {
		t.Errorf("parameters = %v", tr.Instrument.Parameters)
	}
	if tr.Pan != 0 || tr.Volume != 0.8 {
		t.Errorf("pan/volume = %v/%v", tr.Pan, tr.Volume)
	}
	if tr.ReverbSend != 0.4 {
		t.Errorf("reverb_send = %v, want 0.4", tr.ReverbSend)
	}
	if tr.EQ.HPF != 80 || tr.EQ.MidPeak.Freq != 300 || tr.EQ.MidPeak.Gain != -3 || tr.EQ.MidPeak.Q != 1.2 {
		t.Errorf("eq = %+v", tr.EQ)
	}
	if tr.EQ.LowShelf.Freq != 150 || tr.EQ.LowShelf.Gain != 1.5 || tr.EQ.HighShelf.Freq != 8000 || tr.EQ.HighShelf.Gain != 2 {
		t.Errorf("eq shelves = %+v", tr.EQ)
	}
	if len(tr.Notes) != 3 {
		t.Fatalf("notes = %d, want 3", len(tr.Notes))
	}
	n := tr.Notes
	if n[0].Time != 0 || n[0].Duration != 0.5 || n[0].Pitch != 60 || n[0].Velocity != 0.8 {
		t.Errorf("note 0 = %+v", n[0])
	}
	if n[1].Time != 0.5 || n[1].Pitch != 62 {
		t.Errorf("note 1 = %+v", n[1])
	}
	if n[2].Time != 1 || n[2].Duration != 1 || n[2].Pitch != 64 || n[2].Velocity != 0.9 {
		t.Errorf("note 2 = %+v", n[2])
	}
	if n[2].Articulation.Type != "tenuto" {
		t.Errorf("articulation = %q", n[2].Articulation.Type)
	}
	if v, ok := n[2].Articulation.Params["extra"].(float64); !ok || v != 0.1 {
		t.Errorf("articulation params = %v", n[2].Articulation.Params)
	}
	if s.TotalNotes() != 3 {
		t.Errorf("TotalNotes = %d", s.TotalNotes())
	}
	if s.Duration() != 2 {
		t.Errorf("Duration = %v", s.Duration())
	}
	if n[0].PitchName() != "C4" || n[2].PitchName() != "E4" {
		t.Errorf("PitchName = %q, %q", n[0].PitchName(), n[2].PitchName())
	}
}

func TestParseJSON(t *testing.T) {
	s, err := ParseJSON([]byte(exampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	checkExample(t, s)
	if err := Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestParseJSONInvalid(t *testing.T) {
	for _, bad := range []string{`{`, `[]`, `"x"`, `{"tracks": 5}`} {
		if _, err := ParseJSON([]byte(bad)); err == nil {
			t.Errorf("ParseJSON(%q) = nil error", bad)
		}
	}
}

func TestParseYAML(t *testing.T) {
	s, err := ParseYAML([]byte(exampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	checkExample(t, s)
	if err := Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestParseYAMLAlignedSequence covers sequences indented at the same level
// as their parent key, the other common YAML style.
func TestParseYAMLAlignedSequence(t *testing.T) {
	doc := `metadata:
  title: T
  bpm: 60
  time_signature: 3/4
tracks:
- id: a
  instrument:
    type: ocarina
  volume: 1
  notes:
  - time: 0
    duration: 1
    pitch: 72
    velocity: 1
`
	s, err := ParseYAML([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if s.Metadata.TimeSignature != "3/4" {
		t.Errorf("time_signature = %q", s.Metadata.TimeSignature)
	}
	if len(s.Tracks) != 1 || len(s.Tracks[0].Notes) != 1 {
		t.Fatalf("tracks/notes = %d/%d", len(s.Tracks), len(s.Tracks[0].Notes))
	}
	if s.Tracks[0].Notes[0].Pitch != 72 || s.Tracks[0].Volume != 1 {
		t.Errorf("note = %+v, volume = %v", s.Tracks[0].Notes[0], s.Tracks[0].Volume)
	}
}

// TestYAMLScalarTypes checks the plain-scalar type inference feeding the
// JSON bridge.
func TestYAMLScalarTypes(t *testing.T) {
	doc := "i: 42\nneg: -7\nf: 2.5\nexp: 1e3\nt: true\nf2: FALSE\nn: null\ntilde: ~\ns: hello world\nq: \"42\"\nsq: 'it''s'\nratio: 4/4\ninf: .inf\n"
	v, err := yamlToValue([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("root type %T, want map", v)
	}
	want := map[string]interface{}{
		"i": int64(42), "neg": int64(-7), "f": 2.5, "exp": float64(1000),
		"t": true, "f2": false, "n": nil, "tilde": nil,
		"s": "hello world", "q": "42", "sq": "it's", "ratio": "4/4", "inf": ".inf",
	}
	for k, w := range want {
		if got := m[k]; got != w {
			t.Errorf("%s = %#v (%T), want %#v", k, got, got, w)
		}
	}
}

// TestYAMLFlow covers single-line flow sequences and mappings, including
// nesting and quoted strings containing delimiters.
func TestYAMLFlow(t *testing.T) {
	doc := "seq: [1, 2.5, \"x,y\", null, [3]]\nmap: {a: 1, b: {c: true}, 'd e': []}\n"
	v, err := yamlToValue([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]interface{})
	seq, ok := m["seq"].([]interface{})
	if !ok || len(seq) != 5 {
		t.Fatalf("seq = %#v", m["seq"])
	}
	if seq[0] != int64(1) || seq[1] != 2.5 || seq[2] != "x,y" || seq[3] != nil {
		t.Errorf("seq items = %#v", seq)
	}
	inner, ok := seq[4].([]interface{})
	if !ok || len(inner) != 1 || inner[0] != int64(3) {
		t.Errorf("nested seq = %#v", seq[4])
	}
	fm, ok := m["map"].(map[string]interface{})
	if !ok {
		t.Fatalf("map = %#v", m["map"])
	}
	if fm["a"] != int64(1) {
		t.Errorf("map.a = %#v", fm["a"])
	}
	if bm, ok := fm["b"].(map[string]interface{}); !ok || bm["c"] != true {
		t.Errorf("map.b = %#v", fm["b"])
	}
	if de, ok := fm["d e"].([]interface{}); !ok || len(de) != 0 {
		t.Errorf("map['d e'] = %#v", fm["d e"])
	}
}

// TestYAMLBareDashAndNested covers bare-dash items and nested sequences.
func TestYAMLBareDashAndNested(t *testing.T) {
	doc := "list:\n  -\n    a: 1\n  -\n    b: 2\nnest:\n  - - 1\n    - 2\n"
	v, err := yamlToValue([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]interface{})
	list := m["list"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("list = %#v", list)
	}
	if list[0].(map[string]interface{})["a"] != int64(1) {
		t.Errorf("list[0] = %#v", list[0])
	}
	if list[1].(map[string]interface{})["b"] != int64(2) {
		t.Errorf("list[1] = %#v", list[1])
	}
	nest := m["nest"].([]interface{})
	if len(nest) != 1 {
		t.Fatalf("nest = %#v", nest)
	}
	row := nest[0].([]interface{})
	if len(row) != 2 || row[0] != int64(1) || row[1] != int64(2) {
		t.Errorf("nest[0] = %#v", row)
	}
}

func TestParseYAMLErrors(t *testing.T) {
	tests := []struct {
		name    string
		doc     string
		wantSub string
	}{
		{"tab indent", "a:\n\tb: 1\n", "tabs are not allowed"},
		{"block scalar", "a: |\n  text\n", "block scalars"},
		{"anchor", "a: &x 1\n", "anchors"},
		{"alias", "a: *x\n", "anchors"},
		{"multi doc", "a: 1\n---\nb: 2\n", "multiple documents"},
		{"unclosed flow", "a: [1, 2\n", "flow sequence"},
		{"bad indent", "a: 1\n  b: 2\n", "unexpected indentation"},
		{"dash in mapping", "a: 1\n- 2\n", "sequence entry where mapping key expected"},
		{"bad quote", "a: \"oops\n", "invalid double-quoted string"},
		{"trailing flow", "a: [1] x\n", "trailing content"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseYAML([]byte(tc.doc))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{
		"s.json": exampleJSON,
		"s.yaml": exampleYAML,
		"s.txt":  exampleJSON, // unknown extension falls back to JSON
	}
	for name, content := range paths {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := ParseFile(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkExample(t, s)
	}
	if _, err := ParseFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestConvertToSeconds(t *testing.T) {
	tests := []struct {
		beats, bpm, want float64
	}{
		{1, 120, 0.5},
		{2.5, 75, 2.0},
		{0.5, 60, 0.5},
		{4, 240, 1.0},
		{1, 0, 0},   // guarded: non-positive tempo
		{1, -60, 0}, // guarded: non-positive tempo
	}
	for _, tc := range tests {
		if got := ConvertToSeconds(tc.beats, tc.bpm); got != tc.want {
			t.Errorf("ConvertToSeconds(%v, %v) = %v, want %v", tc.beats, tc.bpm, got, tc.want)
		}
	}
}

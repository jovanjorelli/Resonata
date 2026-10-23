package score

import (
	"os"
	"path/filepath"
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

// TestParseJSON verifies JSON score decoding into structured score model.
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

// TestParseJSONInvalid verifies malformed JSON syntax returns descriptive parse errors.
func TestParseJSONInvalid(t *testing.T) {
	for _, bad := range []string{`{`, `[]`, `"x"`, `{"tracks": 5}`} {
		if _, err := ParseJSON([]byte(bad)); err == nil {
			t.Errorf("ParseJSON(%q) = nil error", bad)
		}
	}
}

// TestParseFile verifies reading and parsing score JSON files from disk.
func TestParseFile(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{
		"s.json": exampleJSON,
		"s.txt":  exampleJSON,
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
	for _, bad := range []string{"s.yaml", "s.yml", "s.YAML", "s.YML"} {
		p := filepath.Join(dir, bad)
		if err := os.WriteFile(p, []byte("title: x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseFile(p); err == nil {
			t.Fatalf("ParseFile(%s) = nil, want error", bad)
		}
	}
	if _, err := ParseFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("want error for missing file")
	}
}

// TestConvertToSeconds verifies musical beat to real seconds conversion at given BPM.
func TestConvertToSeconds(t *testing.T) {
	tests := []struct {
		beats, bpm, want float64
	}{
		{1, 120, 0.5},
		{2.5, 75, 2.0},
		{0.5, 60, 0.5},
		{4, 240, 1.0},
		{1, 0, 0},
		{1, -60, 0},
	}
	for _, tc := range tests {
		if got := ConvertToSeconds(tc.beats, tc.bpm); got != tc.want {
			t.Errorf("ConvertToSeconds(%v, %v) = %v, want %v", tc.beats, tc.bpm, got, tc.want)
		}
	}
}

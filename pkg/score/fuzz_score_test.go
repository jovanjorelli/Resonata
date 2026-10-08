package score

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzScoreJSON feeds arbitrary bytes to the JSON score parser. It must
// never panic; malformed input yields an error and valid input yields a
// score whose validation result is deterministic.
func FuzzScoreJSON(f *testing.F) {
	f.Add(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 69, "velocity": 0.8}]}]}`)
	f.Add(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": []}`)
	f.Add(``)
	f.Add(`{"metadata": `)
	f.Add("\x00\x01\x02\xff\xfe")
	if data, err := os.ReadFile(filepath.Join("..", "..", "examples", "simple_score.json")); err == nil {
		f.Add(string(data))
	}
	f.Fuzz(func(t *testing.T, data string) {
		s, err := ParseJSON([]byte(data))
		if err != nil {
			return // malformed input fails gracefully
		}
		if s == nil {
			t.Fatal("nil score with nil error")
		}
		_ = Validate(s) // validation may pass or fail, never panic
		_ = s.TotalNotes()
		_ = s.Duration()
	})
}

// FuzzScoreYAML feeds arbitrary bytes to the YAML subset parser with the
// same no-panic contract as the JSON target.
func FuzzScoreYAML(f *testing.F) {
	f.Add("metadata:\n  title: T\n  bpm: 120\n  time_signature: 4/4\ntracks:\n  - id: s\n    instrument: {type: ocarina}\n    volume: 0.8\n    notes:\n      - {time: 0, duration: 0.5, pitch: 69, velocity: 0.8}\n")
	f.Add("metadata:\n  title: T\n  bpm: 120\n  time_signature: 4/4\ntracks: []\n")
	f.Add(``)
	f.Add("metadata:\n\tbpm: 120\n")
	f.Add("a: [1\n")
	if data, err := os.ReadFile(filepath.Join("..", "..", "examples", "simple_score.yaml")); err == nil {
		f.Add(string(data))
	}
	f.Fuzz(func(t *testing.T, data string) {
		s, err := ParseYAML([]byte(data))
		if err != nil {
			return // malformed input fails gracefully
		}
		if s == nil {
			t.Fatal("nil score with nil error")
		}
		_ = Validate(s)
		_ = s.TotalNotes()
		_ = s.Duration()
	})
}

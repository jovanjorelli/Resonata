package score

import (
	"strings"
	"testing"
)

// TestRegPedalParse proves a valid pedal array parses and validates.
func TestRegPedalParse(t *testing.T) {
	s, err := ParseJSON([]byte(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": [{
		"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8,
		"pedal": [{"time": 0, "down": true}, {"time": 1.5, "down": false}],
		"notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := s.Tracks[0].Pedal
	if len(got) != 2 || !got[0].Down || got[0].Time != 0 || got[1].Down || got[1].Time != 1.5 {
		t.Fatalf("pedal = %+v", got)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestRegPedalInvalidTime proves bad pedal times join the violation set.
func TestRegPedalInvalidTime(t *testing.T) {
	s, err := ParseJSON([]byte(`{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": [{
		"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8,
		"pedal": [{"time": -1, "down": true}],
		"notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	err = Validate(s)
	if err == nil || !strings.Contains(err.Error(), "pedal[0].time") {
		t.Fatalf("want pedal time error, got %v", err)
	}
}

// TestRegPedalYAML proves pedal moves survive the YAML subset parser.
func TestRegPedalYAML(t *testing.T) {
	s, err := ParseYAML([]byte("metadata:\n  title: T\n  bpm: 120\n  time_signature: 4/4\ntracks:\n  - id: s\n    instrument: {type: ocarina}\n    volume: 0.8\n    pedal:\n      - {time: 0, down: true}\n    notes:\n      - {time: 0, duration: 0.5, pitch: 60, velocity: 0.8}\n"))
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if len(s.Tracks) != 1 || len(s.Tracks[0].Pedal) != 1 || !s.Tracks[0].Pedal[0].Down {
		t.Fatalf("pedal = %+v", s.Tracks)
	}
	if err := Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

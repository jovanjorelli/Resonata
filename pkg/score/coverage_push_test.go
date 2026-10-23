package score

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pushValidScore = `{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 69, "velocity": 0.8}]}]}`

// TestPushParseJSONEdges proves empty, truncated, and wrong-type inputs
// fail loudly while unknown fields are ignored.
func TestPushParseJSONEdges(t *testing.T) {
	if _, err := ParseJSON(nil); err == nil {
		t.Fatal("empty JSON accepted")
	}
	if _, err := ParseJSON([]byte(`{"metadata": `)); err == nil {
		t.Fatal("truncated JSON accepted")
	}
	s, err := ParseJSON([]byte(`{"zzz": 1, "metadata": {"title": "T", "bpm": 120, "time_signature": "4/4", "qq": true}, "tracks": [{"id": "s", "wobble": 2, "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 69, "velocity": 0.8, "frobnicate": "x"}]}]}`))
	if err != nil {
		t.Fatalf("unknown fields rejected: %v", err)
	}
	if s.Metadata.Title != "T" || s.TotalNotes() != 1 {
		t.Fatalf("parsed score = %+v", s)
	}
}

// TestPushParseFileEdges verifies rejection of unsupported file extensions such as YAML.
func TestPushParseFileEdges(t *testing.T) {
	if _, err := ParseFile(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
	yml := filepath.Join(t.TempDir(), "s.yml")
	if err := os.WriteFile(yml, []byte("title: Y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(yml); err == nil {
		t.Fatal("yml accepted")
	}
	if _, err := ParseFile(filepath.Join(t.TempDir(), "bad.yaml")); err == nil {
		t.Fatal("missing yaml accepted")
	}
}

// TestPushBeatDurationEdges proves unusable tempos and signatures yield
// zero so swing is skipped.
func TestPushBeatDurationEdges(t *testing.T) {
	for _, tc := range []struct {
		bpm float64
		ts  string
	}{
		{0, "4/4"}, {-120, "4/4"}, {math.NaN(), "4/4"}, {math.Inf(1), "4/4"},
		{120, "4/0"}, {120, "4/x"},
	} {
		if got := beatDuration(tc.bpm, tc.ts); got != 0 {
			t.Fatalf("beatDuration(%v, %q) = %v, want 0", tc.bpm, tc.ts, got)
		}
	}
	if got := beatDuration(120, "6/8"); got != 0.25 {
		t.Fatalf("beatDuration 6/8 = %v, want 0.25", got)
	}
	if got := beatDuration(120, "120"); got != 0.5 {
		t.Fatalf("beatDuration no-slash = %v, want 0.5", got)
	}
}

// TestPushSwingSkipped proves unusable beats leave note times untouched.
func TestPushSwingSkipped(t *testing.T) {
	s := &Score{Metadata: Metadata{Title: "T", BPM: 0, TimeSignature: "4/4", Swing: 0.8},
		Tracks: []Track{{ID: "s", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8,
			Notes: []NoteEvent{{Time: 0.25, Duration: 0.5, Pitch: 60, Velocity: 0.8}}}}}
	s.ApplySwing()
	if s.Tracks[0].Notes[0].Time != 0.25 {
		t.Fatalf("time moved to %v", s.Tracks[0].Notes[0].Time)
	}
}

// TestPushAccentNegative proves negative accents clamp velocity to zero.
func TestPushAccentNegative(t *testing.T) {
	s := &Score{Metadata: Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{{ID: "s", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8,
			Notes: []NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8, Accent: -2}}}}}
	s.ApplyAccents()
	if got := s.Tracks[0].Notes[0].Velocity; got != 0 {
		t.Fatalf("velocity = %v, want 0", got)
	}
}

// TestPushTimingNaN proves NaN offsets clamp the note to the piece start.
func TestPushTimingNaN(t *testing.T) {
	s := &Score{Metadata: Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{{ID: "s", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8,
			Notes: []NoteEvent{{Time: 1, Duration: 0.5, Pitch: 60, Velocity: 0.8, TimingOffsetMs: math.NaN()}}}}}
	s.ApplyTimingOffsets()
	if got := s.Tracks[0].Notes[0].Time; got != 0 {
		t.Fatalf("time = %v, want 0", got)
	}
}

// TestPushRoomUnmarshalEdges proves null, empty, bad JSON, and case
// folding decode as documented.
func TestPushRoomUnmarshalEdges(t *testing.T) {
	var r Room
	if err := r.UnmarshalJSON([]byte("null")); err != nil || r.Preset != "" || r.IsPreset {
		t.Fatalf("null = %+v %v", r, err)
	}
	if err := r.UnmarshalJSON([]byte("")); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := r.UnmarshalJSON([]byte(`"HALL"`)); err != nil || !r.IsPreset || r.Preset != "hall" {
		t.Fatalf("upper = %+v %v", r, err)
	}
	if err := r.UnmarshalJSON([]byte(`"abc`)); err == nil {
		t.Fatal("bad string accepted")
	}
	if err := r.UnmarshalJSON([]byte(`{bad}`)); err == nil {
		t.Fatal("bad object accepted")
	}
	var r2 Room
	if err := r2.UnmarshalJSON([]byte(`{"size": 0.5}`)); err != nil || r2.IsPreset || r2.Config.Size != 0.5 {
		t.Fatalf("object = %+v %v", r2, err)
	}
}

// TestPushTransposedPitchMessages proves above/below-range errors name
// the track and direction, with name fallbacks.
func TestPushTransposedPitchMessages(t *testing.T) {
	hi := &NoteEvent{Pitch: 200}
	tr := &Track{ID: "vln", Name: "Violin", Transpose: 80}
	err := checkTransposedPitch(hi, tr, 0, 0, 3)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") || !strings.Contains(err.Error(), "Violin") {
		t.Fatalf("high = %v", err)
	}
	lo := &NoteEvent{Pitch: -50}
	tr2 := &Track{ID: "cello", Transpose: -80}
	err = checkTransposedPitch(lo, tr2, 0, 1, 5)
	if err == nil || !strings.Contains(err.Error(), "below minimum") || !strings.Contains(err.Error(), "cello") {
		t.Fatalf("low = %v", err)
	}
	anon := &Track{Transpose: 80}
	err = checkTransposedPitch(hi, anon, 0, 2, 0)
	if err == nil || !strings.Contains(err.Error(), "tracks[2]") {
		t.Fatalf("anon = %v", err)
	}
	// In-range after shift and zero shift report nothing.
	if err := checkTransposedPitch(&NoteEvent{Pitch: 69}, &Track{Transpose: 5}, 0, 0, 0); err != nil {
		t.Fatalf("in-range: %v", err)
	}
	if err := checkTransposedPitch(&NoteEvent{Pitch: 200}, &Track{}, 0, 0, 0); err != nil {
		t.Fatalf("zero shift: %v", err)
	}
	if err := checkTransposedPitch(&NoteEvent{Pitch: 200}, &Track{Transpose: 300}, 0, 0, 0); err != nil {
		t.Fatalf("original out of range: %v", err)
	}
}

// TestPushValidateNonFinite proves NaN/Inf numerics are collected as
// violations, not panics.
func TestPushValidateNonFinite(t *testing.T) {
	s := &Score{Metadata: Metadata{Title: "T", BPM: math.NaN(), TimeSignature: "4/4", Swing: math.NaN(), BreathMs: math.Inf(1)},
		Tracks: []Track{{ID: "s", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8,
			Notes: []NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8}}}}}
	err := Validate(s)
	if err == nil {
		t.Fatal("non-finite accepted")
	}
	for _, want := range []string{"bpm", "swing", "breath"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

// TestPushValidateCombined proves every violation lands in one joined
// error naming each offender.
func TestPushValidateCombined(t *testing.T) {
	s := &Score{Metadata: Metadata{Title: "", BPM: -5, TimeSignature: "nope"},
		Tracks: []Track{
			{ID: "", Instrument: InstrumentDef{}, Volume: 2, Notes: []NoteEvent{{Time: -1, Duration: 0, Pitch: 999, Velocity: 9}}},
			{ID: "dup", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8, Notes: []NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8}}},
			{ID: "dup", Instrument: InstrumentDef{Type: "ocarina"}, Volume: 0.8, Notes: []NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8}}},
		}}
	err := Validate(s)
	if err == nil {
		t.Fatal("garbage accepted")
	}
	for _, want := range []string{"title", "bpm", "time_signature", "id is required", "duplicate track id", "volume", "pitch"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

package score

import (
	"math"
	"testing"
)

// TestScoreApplyTimingOffsetsBeyondEnd verifies note timing offsets extending past score duration.
func TestScoreApplyTimingOffsetsBeyondEnd(t *testing.T) {
	s := &Score{
		Tracks: []Track{
			{
				Notes: []NoteEvent{
					{Pitch: 60, Time: 0.5, Duration: 0.5, TimingOffsetMs: 1000},
				},
			},
		},
	}
	s.ApplyTimingOffsets()
	if s.Tracks[0].Notes[0].Time != 1.0 {
		t.Fatalf("expected clamped time 1.0, got %v", s.Tracks[0].Notes[0].Time)
	}
}

// TestScoreParseFileReadError verifies file system read errors are wrapped and returned cleanly.
func TestScoreParseFileReadError(t *testing.T) {
	_, err := ParseFile(".")
	if err == nil {
		t.Fatal("expected error reading directory as file")
	}
}

// TestScoreValidateTrackSwingAndBreath verifies validation of track-specific swing and breath bounds.
func TestScoreValidateTrackSwingAndBreath(t *testing.T) {
	s1 := &Score{
		Metadata: Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{
			{
				ID:         "t1",
				Instrument: InstrumentDef{Type: "ocarina"},
				Swing:      -0.5,
				Notes:      []NoteEvent{{Pitch: 60, Time: 0, Duration: 1}},
			},
		},
	}
	if err := Validate(s1); err == nil {
		t.Fatal("expected error for swing < 0")
	}

	s2 := &Score{
		Metadata: Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{
			{
				ID:         "t1",
				Instrument: InstrumentDef{Type: "ocarina"},
				BreathMs:   -10,
				Notes:      []NoteEvent{{Pitch: 60, Time: 0, Duration: 1}},
			},
		},
	}
	if err := Validate(s2); err == nil {
		t.Fatal("expected error for breath < 0")
	}

	s3 := &Score{
		Metadata: Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{
			{
				ID:         "t1",
				Instrument: InstrumentDef{Type: "ocarina"},
				BreathMs:   math.Inf(1),
				Notes:      []NoteEvent{{Pitch: 60, Time: 0, Duration: 1}},
			},
		},
	}
	if err := Validate(s3); err == nil {
		t.Fatal("expected error for infinite breath")
	}
}

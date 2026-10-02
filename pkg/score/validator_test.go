package score

import (
	"strings"
	"testing"
)

// baseScore builds a minimal valid score for mutation tests.
func baseScore() *Score {
	return &Score{
		Metadata: Metadata{Title: "Test", BPM: 120, TimeSignature: "4/4"},
		Tracks: []Track{{
			ID:         "t1",
			Name:       "Lead",
			Instrument: InstrumentDef{Type: "ocarina"},
			Pan:        0,
			Volume:     0.8,
			Notes: []NoteEvent{
				{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8},
			},
		}},
	}
}

func TestValidateOK(t *testing.T) {
	if err := Validate(baseScore()); err != nil {
		t.Fatalf("valid score rejected: %v", err)
	}
}

func TestValidateNil(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("want error for nil score")
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Score)
		wantSub string
	}{
		{"empty title", func(s *Score) { s.Metadata.Title = "  " }, "title is required"},
		{"zero bpm", func(s *Score) { s.Metadata.BPM = 0 }, "bpm"},
		{"negative bpm", func(s *Score) { s.Metadata.BPM = -1 }, "bpm"},
		{"huge bpm", func(s *Score) { s.Metadata.BPM = 5000 }, "bpm"},
		{"empty time signature", func(s *Score) { s.Metadata.TimeSignature = "" }, "time_signature"},
		{"no slash", func(s *Score) { s.Metadata.TimeSignature = "4" }, "time_signature"},
		{"zero numerator", func(s *Score) { s.Metadata.TimeSignature = "0/4" }, "time_signature"},
		{"zero denominator", func(s *Score) { s.Metadata.TimeSignature = "4/0" }, "time_signature"},
		{"non numeric", func(s *Score) { s.Metadata.TimeSignature = "a/b" }, "time_signature"},
		{"no tracks", func(s *Score) { s.Tracks = nil }, "at least one track"},
		{"empty track id", func(s *Score) { s.Tracks[0].ID = "" }, "id is required"},
		{"duplicate track id", func(s *Score) {
			dup := s.Tracks[0]
			s.Tracks = append(s.Tracks, dup)
		}, "duplicate track id"},
		{"empty instrument type", func(s *Score) { s.Tracks[0].Instrument.Type = "" }, "instrument.type"},
		{"sampler without file", func(s *Score) { s.Tracks[0].Instrument.Type = "sampler" }, "instrument.file"},
		{"pan too low", func(s *Score) { s.Tracks[0].Pan = -1.5 }, "pan"},
		{"pan too high", func(s *Score) { s.Tracks[0].Pan = 1.5 }, "pan"},
		{"volume negative", func(s *Score) { s.Tracks[0].Volume = -0.1 }, "volume"},
		{"volume too high", func(s *Score) { s.Tracks[0].Volume = 1.1 }, "volume"},
		{"negative time", func(s *Score) { s.Tracks[0].Notes[0].Time = -0.5 }, "time"},
		{"zero duration", func(s *Score) { s.Tracks[0].Notes[0].Duration = 0 }, "duration"},
		{"negative duration", func(s *Score) { s.Tracks[0].Notes[0].Duration = -1 }, "duration"},
		{"pitch below range", func(s *Score) { s.Tracks[0].Notes[0].Pitch = -1 }, "MIDI note"},
		{"pitch above range", func(s *Score) { s.Tracks[0].Notes[0].Pitch = 128 }, "MIDI note"},
		{"velocity too high", func(s *Score) { s.Tracks[0].Notes[0].Velocity = 1.2 }, "velocity"},
		{"negative velocity", func(s *Score) { s.Tracks[0].Notes[0].Velocity = -0.2 }, "velocity"},
		{"reverb send too high", func(s *Score) { s.Tracks[0].ReverbSend = 1.5 }, "reverb_send"},
		{"negative reverb send", func(s *Score) { s.Tracks[0].ReverbSend = -0.1 }, "reverb_send"},
		{"eq band without freq", func(s *Score) {
			s.Tracks[0].EQ.MidPeak = EQBand{Gain: -6}
		}, "eq_mid.freq"},
		{"eq gain out of range", func(s *Score) {
			s.Tracks[0].EQ.LowShelf = EQBand{Freq: 200, Gain: 48, Q: 0.7}
		}, "eq_low.gain"},
		{"eq q out of range", func(s *Score) {
			s.Tracks[0].EQ.HighShelf = EQBand{Freq: 8000, Gain: 2, Q: 99}
		}, "eq_high.q"},
		{"negative hpf", func(s *Score) { s.Tracks[0].EQ.HPF = -5 }, "eq.hpf"},
		{"unknown articulation", func(s *Score) {
			s.Tracks[0].Notes[0].Articulation = Articulation{Type: "marcato"}
		}, "unknown articulation type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := baseScore()
			tc.mutate(s)
			err := Validate(s)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestValidateBoundaries checks that valid edge values are accepted.
func TestValidateBoundaries(t *testing.T) {
	s := baseScore()
	s.Tracks[0].Pan = -1
	s.Tracks[0].Volume = 1
	s.Tracks[0].Notes[0].Pitch = 0
	s.Tracks[0].Notes[0].Velocity = 0
	if err := Validate(s); err != nil {
		t.Fatalf("lower boundaries rejected: %v", err)
	}
	s = baseScore()
	s.Tracks[0].Pan = 1
	s.Tracks[0].Volume = 0
	s.Tracks[0].ReverbSend = 1
	s.Tracks[0].EQ = TrackEQ{
		HPF:       80,
		LowShelf:  EQBand{Freq: 150, Gain: 36, Q: 50},
		MidPeak:   EQBand{Freq: 440, Gain: -36},
		HighShelf: EQBand{Freq: 8000, Gain: 3, Q: 0.7},
	}
	s.Tracks[0].Notes[0].Pitch = 127
	s.Tracks[0].Notes[0].Velocity = 1
	s.Metadata.BPM = 1000
	s.Metadata.TimeSignature = "6/8"
	if err := Validate(s); err != nil {
		t.Fatalf("upper boundaries rejected: %v", err)
	}
}

// TestValidateArticulations checks every known articulation type and the
// empty (unset) case.
func TestValidateArticulations(t *testing.T) {
	for _, kind := range append([]string{""}, KnownArticulations...) {
		s := baseScore()
		s.Tracks[0].Notes[0].Articulation = Articulation{Type: kind}
		if err := Validate(s); err != nil {
			t.Errorf("articulation %q rejected: %v", kind, err)
		}
	}
}

// TestValidateSamplerWithFile checks the sampler/file pairing passes.
func TestValidateSamplerWithFile(t *testing.T) {
	s := baseScore()
	s.Tracks[0].Instrument = InstrumentDef{Type: "sampler", File: "piano.sfz"}
	if err := Validate(s); err != nil {
		t.Fatalf("sampler with file rejected: %v", err)
	}
}

// TestValidateReportsAllIssues checks that violations are joined, not
// short-circuited.
func TestValidateReportsAllIssues(t *testing.T) {
	s := baseScore()
	s.Metadata.Title = ""
	s.Tracks[0].Pan = 2
	s.Tracks[0].Notes[0].Pitch = 200
	err := Validate(s)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if lines := strings.Split(err.Error(), "\n"); len(lines) != 3 {
		t.Fatalf("want 3 joined violations, got %d: %v", len(lines), err)
	}
}

func TestPitchName(t *testing.T) {
	tests := []struct {
		pitch int
		want  string
	}{
		{60, "C4"}, {62, "D4"}, {61, "C#4"}, {59, "B3"}, {0, "C-1"}, {127, "G9"}, {69, "A4"},
	}
	for _, tc := range tests {
		if got := (NoteEvent{Pitch: tc.pitch}).PitchName(); got != tc.want {
			t.Errorf("PitchName(%d) = %q, want %q", tc.pitch, got, tc.want)
		}
	}
}

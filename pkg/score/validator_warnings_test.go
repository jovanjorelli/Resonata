package score

import (
	"strings"
	"testing"
)

// TestValidatorDelayWetWarning verifies warning generation when delay wet send is set to zero.
func TestValidatorDelayWetWarning(t *testing.T) {
	s := baseScore()
	s.Tracks[0].Delay = &TrackDelay{
		Mode: "pingpong", Subdivision: "1/8d",
		Feedback: 0.5, DampingHz: 4000, Wet: 0,
	}
	if err := Validate(s); err != nil {
		t.Fatalf("muted delay rejected as error: %v", err)
	}
	warns := Warnings(s)
	if len(warns) != 1 {
		t.Fatalf("warnings = %v, want exactly 1", warns)
	}
	if !strings.Contains(warns[0], "inaudible") {
		t.Fatalf("warning %q does not contain %q", warns[0], "inaudible")
	}
	if !strings.Contains(warns[0], `"t1"`) {
		t.Fatalf("warning %q does not name the track", warns[0])
	}

	// Audible configurations and absent delays warn nothing.
	s.Tracks[0].Delay.Wet = 0.4
	if warns := Warnings(s); len(warns) != 0 {
		t.Fatalf("audible delay warned: %v", warns)
	}
	if warns := Warnings(baseScore()); len(warns) != 0 {
		t.Fatalf("delay-free score warned: %v", warns)
	}
	if warns := Warnings(nil); len(warns) != 0 {
		t.Fatalf("nil score warned: %v", warns)
	}
}

// TestValidatorDelayRanges verifies out-of-range delay parameters produce validation errors.
func TestValidatorDelayRanges(t *testing.T) {
	validDelay := func() *TrackDelay {
		return &TrackDelay{
			Mode: "stereo", Subdivision: "1/4",
			Feedback: 0.5, DampingHz: 4000, Wet: 0.5,
		}
	}
	tests := []struct {
		name   string
		mutate func(*Score)
		field  string
		value  string
	}{
		{"wet too high", func(s *Score) { s.Tracks[0].Delay = validDelay(); s.Tracks[0].Delay.Wet = 1.5 }, "delay.wet", "1.5"},
		{"wet negative", func(s *Score) { s.Tracks[0].Delay = validDelay(); s.Tracks[0].Delay.Wet = -0.1 }, "delay.wet", "-0.1"},
		{"feedback too high", func(s *Score) { s.Tracks[0].Delay = validDelay(); s.Tracks[0].Delay.Feedback = 0.99 }, "delay.feedback", "0.99"},
		{"feedback negative", func(s *Score) { s.Tracks[0].Delay = validDelay(); s.Tracks[0].Delay.Feedback = -0.1 }, "delay.feedback", "-0.1"},
		{"volume too high", func(s *Score) { s.Tracks[0].Volume = 1.5 }, "volume", "1.5"},
		{"volume negative", func(s *Score) { s.Tracks[0].Volume = -0.5 }, "volume", "-0.5"},
		{"pan too high", func(s *Score) { s.Tracks[0].Pan = 1.5 }, "pan", "1.5"},
		{"pan too low", func(s *Score) { s.Tracks[0].Pan = -1.5 }, "pan", "-1.5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := baseScore()
			tc.mutate(s)
			err := Validate(s)
			if err == nil {
				t.Fatal("want error, got nil")
			}
			msg := err.Error()
			for _, want := range []string{`"t1"`, tc.field, tc.value} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not name %q", msg, want)
				}
			}
		})
	}
}

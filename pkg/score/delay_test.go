package score

import "testing"

// TestValidateDelay checks the echo DSL ranges.
func TestValidateDelay(t *testing.T) {
	good := baseScore()
	good.Tracks[0].Delay = &TrackDelay{
		Mode: "pingpong", Subdivision: "1/8d",
		Feedback: 0.6, DampingHz: 4000, Wet: 0.4, Send: 0.5,
	}
	if err := Validate(good); err != nil {
		t.Fatalf("valid delay rejected: %v", err)
	}
	bad := baseScore()
	bad.Tracks[0].Delay = &TrackDelay{
		Mode: "slapback", Subdivision: "1/32",
		Seconds: -1, Feedback: 2, DampingHz: -5, Wet: 2, Send: -1,
	}
	if err := Validate(bad); err == nil {
		t.Fatal("invalid delay accepted")
	}
	// Nil delay stays valid (echo off).
	if err := Validate(baseScore()); err != nil {
		t.Fatalf("nil delay rejected: %v", err)
	}
}

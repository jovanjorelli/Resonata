package engine

import (
	"testing"

	"resonata/pkg/score"
)

// TestEnvelopeOverrideDispatch schedules per-note envelope overrides
// into the event stream and renders through the override path,
// including an EQ-wrapped track exercising eqInstrument forwarding.
func TestEnvelopeOverrideDispatch(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			{
				ID: "plain", Name: "Plain",
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Volume:     0.8,
				Notes:      []score.NoteEvent{{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8}},
			},
			{
				ID: "shaped", Name: "Shaped",
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Volume:     0.8,
				EQ:         score.TrackEQ{HPF: 80},
				Notes: []score.NoteEvent{{
					Time: 0, Duration: 0.5, Pitch: 72, Velocity: 0.9,
					AttackSec: 0.4, DecaySec: 0.1, ReleaseSec: 1.5,
				}},
			},
		},
	}
	e, err := New(s, 48000, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var plain, shaped *noteEvent
	for i := range e.events {
		ev := &e.events[i]
		if !ev.on {
			continue
		}
		if ev.track == 0 {
			plain = ev
		} else {
			shaped = ev
		}
	}
	if plain == nil || shaped == nil {
		t.Fatal("missing scheduled note-on events")
	}
	if plain.params.Env.HasAttack || plain.params.Env.HasDecay || plain.params.Env.HasRelease {
		t.Fatalf("plain note carries overrides: %+v", plain.params.Env)
	}
	if !shaped.params.Env.HasAttack || shaped.params.Env.Attack != 0.4 {
		t.Errorf("attack override = %+v, want 0.4 flagged", shaped.params.Env)
	}
	if !shaped.params.Env.HasDecay || shaped.params.Env.Decay != 0.1 {
		t.Errorf("decay override = %+v, want 0.1 flagged", shaped.params.Env)
	}
	if !shaped.params.Env.HasRelease || shaped.params.Env.Release != 1.5 {
		t.Errorf("release override = %+v, want 1.5 flagged", shaped.params.Env)
	}
	// Render through both the direct and EQ-forwarded override paths.
	e.ProcessFrames(e.TotalFrames(), nil)
}

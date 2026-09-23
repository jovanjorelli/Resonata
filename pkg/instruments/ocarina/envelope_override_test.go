package ocarina

import (
	"testing"

	"resonata/pkg/instruments"
)

// TestEnvelopeOverrideOcarina replaces the breath attack for one note
// while the remaining times keep their built-in values.
func TestEnvelopeOverrideOcarina(t *testing.T) {
	o := New(48000)
	o.NoteOnEx(69, 0.8, instruments.EnvelopeOverride{Attack: 0.3, HasAttack: true})
	if got := o.env.AttackTime(); got != 0.3 {
		t.Fatalf("attack = %v, want 0.3 (built-in 0.05)", got)
	}
	if got := o.env.DecayTime(); got != decayTime {
		t.Fatalf("decay = %v, want built-in %v", got, decayTime)
	}
	if got := o.env.ReleaseTime(); got != releaseTime {
		t.Fatalf("release = %v, want built-in %v", got, releaseTime)
	}

	plain := New(48000)
	plain.NoteOn(69, 0.8)
	if got := plain.env.AttackTime(); got != attackTime {
		t.Fatalf("plain attack = %v, want built-in %v", got, attackTime)
	}
}

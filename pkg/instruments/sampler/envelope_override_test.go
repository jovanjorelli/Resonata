package sampler

import (
	"testing"

	"resonata/pkg/instruments"
)

// egSampler builds a one-region sampler with an explicit amplitude
// envelope (attack, decay, sustain percent, release) over a constant
// sample, so envelope times are fully determined.
func egSampler(attack, decay, sustain, release float32) *Sampler {
	sd := &SampleData{Samples: make([]float32, 48000), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	rg := newRegion()
	rg.SamplePath, rg.Sample = "a.wav", sd
	rg.HasAmpEG = true
	rg.AmpEG = EGParams{Attack: attack, Decay: decay, Sustain: sustain, Release: release}
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{rg}})
	return s
}

// closeTime fails when got differs from want beyond float32 noise.
func closeTime(t *testing.T, got, want float64, what string) {
	t.Helper()
	diff := got - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-6 {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

// soundingVoice returns the first active voice, failing when none.
func soundingVoice(t *testing.T, s *Sampler) *Voice {
	t.Helper()
	for i := range s.voices {
		if s.voices[i].Active {
			return &s.voices[i]
		}
	}
	t.Fatal("no active voice")
	return nil
}

// TestEnvelopeOverrideAttack replaces the region attack for one note.
func TestEnvelopeOverrideAttack(t *testing.T) {
	s := egSampler(0.1, 0.2, 100, 0.3)
	s.NoteOnEx(60, 1.0, instruments.EnvelopeOverride{Attack: 0.5, HasAttack: true})
	v := soundingVoice(t, s)
	closeTime(t, v.Envelope.AttackTime(), 0.5, "attack")
}

// TestEnvelopeOverrideRelease replaces the region release for one note.
func TestEnvelopeOverrideRelease(t *testing.T) {
	s := egSampler(0.1, 0.2, 100, 0.3)
	s.NoteOnEx(60, 1.0, instruments.EnvelopeOverride{Release: 2.0, HasRelease: true})
	v := soundingVoice(t, s)
	s.NoteOff(60)
	closeTime(t, v.Envelope.ReleaseTime(), 2.0, "release")
}

// TestEnvelopeOverrideNone keeps the SFZ times without overrides.
func TestEnvelopeOverrideNone(t *testing.T) {
	s := egSampler(0.1, 0.2, 100, 0.3)
	s.NoteOn(60, 1.0)
	v := soundingVoice(t, s)
	closeTime(t, v.Envelope.AttackTime(), 0.1, "attack")
	closeTime(t, v.Envelope.DecayTime(), 0.2, "decay")
	closeTime(t, v.Envelope.ReleaseTime(), 0.3, "release")
}

// TestEnvelopeOverridePartial replaces only the flagged time.
func TestEnvelopeOverridePartial(t *testing.T) {
	s := egSampler(0.1, 0.2, 100, 0.3)
	s.NoteOnEx(60, 1.0, instruments.EnvelopeOverride{Release: 1.0, HasRelease: true})
	v := soundingVoice(t, s)
	closeTime(t, v.Envelope.AttackTime(), 0.1, "attack")
	closeTime(t, v.Envelope.DecayTime(), 0.2, "decay")
	closeTime(t, v.Envelope.ReleaseTime(), 1.0, "release")
	if sus := v.Envelope.SustainLevel(); sus != 1.0 {
		t.Fatalf("sustain = %v, want 1.0 (never overridden)", sus)
	}
}

// TestEnvelopeOverrideFastAttack peaks the envelope in a single frame
// when the frame spans the 1 ms override attack.
func TestEnvelopeOverrideFastAttack(t *testing.T) {
	s := egSampler(0.5, 0.2, 100, 0.3)
	s.NoteOnEx(60, 1.0, instruments.EnvelopeOverride{Attack: 0.001, HasAttack: true})
	v := soundingVoice(t, s)
	mono := make([]float32, 1)
	v.Process(mono, 0.001)
	if got := v.Envelope.Value(); got != 1.0 {
		t.Fatalf("envelope value after one 1 ms frame = %v, want peak 1.0", got)
	}
}

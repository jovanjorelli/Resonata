package synth

import (
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// TestDeepNewDefaults proves degenerate rates fall back to 48000.
func TestDeepNewDefaults(t *testing.T) {
	for _, sr := range []float64{0, -100} {
		s := New(sr)
		if s.sampleRate != 48000 {
			t.Fatalf("New(%v).rate = %v", sr, s.sampleRate)
		}
	}
}

// TestDeepRetuneClamps proves cutoff and resonance extremes clamp.
func TestDeepRetuneClamps(t *testing.T) {
	s := New(48000)
	s.SetParameters(map[string]float32{"cutoff": 1, "resonance": 9})
	if s.cutoff != minCutoff || s.resonance != 1 {
		t.Fatalf("clamps = %v %v", s.cutoff, s.resonance)
	}
	if s.filterAlpha <= 0 || s.filterAlpha > 1 {
		t.Fatalf("alpha = %v", s.filterAlpha)
	}
	s.SetParameters(map[string]float32{"cutoff": 1e9})
	if s.cutoff != maxCutoff {
		t.Fatalf("cutoff = %v", s.cutoff)
	}
}

// TestDeepVelocityClamp proves out-of-range velocities clamp silently.
func TestDeepVelocityClamp(t *testing.T) {
	s := New(48000)
	s.NoteOn(60, 5.0)
	s.NoteOn(61, -2.0)
	if got := s.ActiveVoices(); got != 2 {
		t.Fatalf("voices = %d, want 2", got)
	}
}

// TestDeepVibratoIgnored proves vibrato payloads are accepted quietly.
func TestDeepVibratoIgnored(t *testing.T) {
	s := New(48000)
	s.NoteOnParams(60, 0.8, instruments.NoteParams{
		HasVibrato: true, VibratoRate: 6, VibratoDepth: 0.05,
		HasExpression: true, Expression: 0.5,
	})
	buf := dsp.NewStereoBuffer(256)
	buf.SetLen(256)
	s.Process(buf, 1.0/48000)
	peak := float32(0)
	for _, v := range buf.Left {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		t.Fatal("vibrato note silent")
	}
}

// TestDeepProcessGuards proves nil/empty/negative-dt buffers are safe.
func TestDeepProcessGuards(t *testing.T) {
	s := New(48000)
	s.NoteOn(60, 0.8)
	s.Process(nil, 1.0/48000)
	empty := dsp.NewStereoBuffer(0)
	s.Process(empty, 1.0/48000)
	buf := dsp.NewStereoBuffer(64)
	buf.SetLen(64)
	s.Process(buf, 0)
	s.Process(buf, -1)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
}

// TestDeepReleaseUntriggered proves NoteOff on an idle voice is safe.
func TestDeepReleaseUntriggered(t *testing.T) {
	s := New(48000)
	s.NoteOff(60)
	if got := s.ActiveVoices(); got != 0 {
		t.Fatalf("voices = %d", got)
	}
}

// TestDeepMonoRender proves both channels carry identical mono audio.
func TestDeepMonoRender(t *testing.T) {
	s := New(48000)
	s.NoteOn(69, 0.9)
	buf := dsp.NewStereoBuffer(512)
	buf.SetLen(512)
	s.Process(buf, 1.0/48000)
	for i := range buf.Left {
		if buf.Left[i] != buf.Right[i] {
			t.Fatalf("frame %d stereo differs", i)
		}
	}
}

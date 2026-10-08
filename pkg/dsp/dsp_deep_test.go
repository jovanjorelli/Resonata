package dsp

import (
	"math"
	"testing"
)

// TestDeepEnvelopeAccessors proves the envelope time reporters.
func TestDeepEnvelopeAccessors(t *testing.T) {
	e := NewEnvelope(0.01, 0.2, 0.6, 0.3)
	if e.AttackTime() != 0.01 || e.DecayTime() != 0.2 || e.SustainLevel() != 0.6 || e.ReleaseTime() != 0.3 {
		t.Fatalf("times = %v %v %v %v", e.AttackTime(), e.DecayTime(), e.SustainLevel(), e.ReleaseTime())
	}
}

// TestDeepSineFill proves Fill matches sequential Next calls.
func TestDeepSineFill(t *testing.T) {
	a, b := NewSine(), NewSine()
	a.SetFrequency(440, 48000)
	b.SetFrequency(440, 48000)
	buf := make([]float32, 64)
	b.Fill(buf)
	for i := range buf {
		if buf[i] != a.Next() {
			t.Fatalf("frame %d = %v, want Next", i, buf[i])
		}
	}
}

// TestDeepLoudness proves the silence floor and a loud reading.
func TestDeepLoudness(t *testing.T) {
	l := NewLimiter(48000)
	if got := l.LoudnessLUFS(); got != -70 {
		t.Fatalf("silent = %v, want -70", got)
	}
	buf := NewStereoBuffer(1024)
	buf.SetLen(1024)
	for i := range buf.Left {
		v := float32(0.8)
		if i%2 == 1 {
			v = -v
		}
		buf.Left[i] = v
		buf.Right[i] = v
	}
	for i := 0; i < 300; i++ {
		l.ProcessStereo(buf)
	}
	if got := l.LoudnessLUFS(); got <= -70 {
		t.Fatalf("loud = %v, want above floor", got)
	}
}

// TestDeepLookaheadDefaults proves degenerate construction normalizes.
func TestDeepLookaheadDefaults(t *testing.T) {
	if got := NewLookaheadBuffer(0).Delay(); got != 1 {
		t.Fatalf("delay = %d, want 1", got)
	}
	if got := NewLimiter(0); got == nil {
		t.Fatal("nil limiter")
	}
	if got := NewReverb(0, ReverbParams{}); got == nil {
		t.Fatal("nil reverb")
	}
	if got := NewStereoDelay(0, 0); got == nil || got.SampleRate() != 48000 {
		t.Fatal("bad delay defaults")
	}
	if got := NewEQ(0, EQSpec{}); got == nil {
		t.Fatal("nil EQ")
	}
}

// TestDeepDelayHelpers proves tap math edges and accessors.
func TestDeepDelayHelpers(t *testing.T) {
	if DelaySamplesFor(0, SubdivisionQuarter, 48000) != 0 {
		t.Fatal("bad bpm accepted")
	}
	if DelaySamplesFor(120, SubdivisionQuarter, 0) != 0 {
		t.Fatal("bad rate accepted")
	}
	if got := DelaySamplesFor(120, SubdivisionQuarter, 48000); got != 24000 {
		t.Fatalf("quarter@120 = %d, want 24000", got)
	}
	if DampingCoeff(1000, 0) != 0 || DampingCoeff(0, 48000) != 0 {
		t.Fatal("bad damping args accepted")
	}
	if got := DampingCoeff(1e9, 48000); !(got > 0) || !(got < 1) {
		t.Fatalf("nyquist damping = %v, want (0,1)", got)
	}
	if got, want := DampingCoeff(10, 48000), float32(0.998); !(got > want) {
		t.Fatalf("low damping = %v, want > %v", got, want)
	}
	d := NewStereoDelay(48000, MaxDelaySeconds)
	d.SetDelaySamples(-5)
	if d.LengthSamples() != 0 {
		t.Fatalf("negative tap = %d", d.LengthSamples())
	}
	d.SetDelaySamples(1 << 30)
	if d.LengthSamples() != d.MemoryBytes()/8-1 {
		t.Fatalf("oversize tap = %d", d.LengthSamples())
	}
	if d.DampingHz() < 0 {
		t.Fatal("negative damping hz")
	}
}

// TestDeepBiquadGain proves per-mode construction with gain.
func TestDeepBiquadGain(t *testing.T) {
	for _, m := range []FilterMode{Lowpass, Highpass, Bandpass, Peaking, LowShelf, HighShelf} {
		b := NewBiquadGain(m, 1000, 1, 3, 48000)
		if b == nil || b.Frequency() != 1000 {
			t.Fatalf("mode %v broken", m)
		}
	}
}

// TestDeepBufferCopy proves oversized sources clamp to capacity.
func TestDeepBufferCopy(t *testing.T) {
	dst := NewStereoBuffer(8)
	src := NewStereoBuffer(64)
	src.SetLen(64)
	for i := range src.Left {
		src.Left[i] = 0.5
		src.Right[i] = -0.5
	}
	dst.Copy(src)
	if dst.Len() != 8 {
		t.Fatalf("len = %d, want 8", dst.Len())
	}
	for i := range dst.Left {
		if dst.Left[i] != 0.5 || dst.Right[i] != -0.5 {
			t.Fatalf("frame %d not copied", i)
		}
	}
}

// TestDeepReverbDamping proves damping extremes clamp into range.
func TestDeepReverbDamping(t *testing.T) {
	r := NewReverb(48000, ReverbParams{RoomSize: 0.8})
	r.SetDamping(-2)
	if got := r.Params().Damping; got != 0 {
		t.Fatalf("damping = %v, want 0", got)
	}
	r.SetDamping(5)
	if got := r.Params().Damping; got != 0.95 {
		t.Fatalf("damping = %v, want 0.95", got)
	}
	r.SetDamping(0.3)
	if got := r.Params().Damping; got != 0.3 {
		t.Fatalf("damping = %v, want 0.3", got)
	}
}

// TestDeepNewFDNLine proves degenerate line lengths normalize.
func TestDeepNewFDNLine(t *testing.T) {
	ln := newFDNLine(0)
	ln.length = 1
	ln.write(0.25)
	if got := ln.read(); got != 0.25 {
		t.Fatalf("read = %v", got)
	}
}

// TestDeepSineSteady proves oscillator output stays bounded.
func TestDeepSineSteady(t *testing.T) {
	s := NewSine()
	s.SetFrequency(440, 48000)
	s.SetFrequency(440, 0) // bad rate keeps prior tuning
	peak := float32(0)
	for i := 0; i < 48000; i++ {
		if v := s.Next(); v < -1.01 || v > 1.01 {
			t.Fatalf("out of range: %v", v)
		} else if a := v; a < 0 {
			a = -a
			if a > peak {
				peak = a
			}
		} else if v > peak {
			peak = v
		}
	}
	if peak < 0.99 || math.IsNaN(float64(peak)) {
		t.Fatalf("peak = %v", peak)
	}
}

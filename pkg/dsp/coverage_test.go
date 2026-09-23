package dsp

import (
	"math"
	"testing"
)

// TestEnvelopeLifecycle walks every ADSR stage and the Fill/Apply
// helpers, plus the clamp helpers and mid-note retunes.
func TestEnvelopeLifecycle(t *testing.T) {
	e := NewEnvelope(0.01, 0.02, 0.7, 0.03)
	if e.Stage() != StageIdle || e.Value() != 0 {
		t.Fatalf("fresh envelope = %v/%v, want idle/0", e.Stage(), e.Value())
	}
	e.Release() // idle release is a no-op
	if e.Stage() != StageIdle {
		t.Fatal("idle Release changed stage")
	}
	e.Trigger()
	if e.Stage() != StageAttack || e.Value() != 0 {
		t.Fatal("Trigger did not restart attack from zero")
	}
	const dt = 1.0 / 48000
	for e.Stage() == StageAttack {
		if v := e.Next(dt); v < 0 || v > 1 {
			t.Fatalf("attack value %v out of [0,1]", v)
		}
	}
	if e.Stage() != StageDecay {
		t.Fatalf("after attack: stage = %v, want decay", e.Stage())
	}
	e.SetParameters(0.005, 0.01, 0.6, 0.02) // mid-note retune keeps stage
	for e.Stage() == StageDecay {
		e.Next(dt)
	}
	if e.Stage() != StageSustain || math.Abs(e.Value()-0.6) > 1e-9 {
		t.Fatalf("sustain = %v/%v, want sustain/0.6", e.Stage(), e.Value())
	}
	e.Release()
	for e.Stage() != StageIdle {
		e.Next(dt)
	}
	if e.Value() != 0 {
		t.Fatalf("end value = %v, want 0", e.Value())
	}
	e.Release() // releasing idle stays idle

	buf := make([]float32, 64)
	e.Trigger()
	e.Fill(buf, dt) // advances one step per frame from zero
	if buf[0] <= 0 || buf[0] >= 0.01 || buf[63] <= buf[0] {
		t.Fatalf("Fill ramp = first %v last %v, want rising from ~0", buf[0], buf[63])
	}
	for i := range buf {
		buf[i] = 1
	}
	e.Apply(buf, dt)
	for _, v := range buf {
		if v < 0 || v > 1 {
			t.Fatalf("Apply value %v out of [0,1]", v)
		}
	}

	if clampMin(-5, 2) != 2 || clampMin(5, 2) != 5 {
		t.Fatal("clampMin wrong")
	}
	if clamp01(-1) != 0 || clamp01(2) != 1 || clamp01(0.5) != 0.5 {
		t.Fatal("clamp01 wrong")
	}
	if ClampF32(float32(math.NaN()), 0, 1) != 0 {
		t.Fatal("ClampF32 NaN should map to lo")
	}
	// Non-positive times behave as instantaneous segments.
	fast := NewEnvelope(0, 0, 1, 0)
	fast.Trigger()
	fast.Next(dt)
	if fast.Stage() != StageSustain && fast.Stage() != StageDecay {
		t.Fatalf("instant attack left stage %v", fast.Stage())
	}
}

// TestBufferHelpers covers the StereoBuffer reuse API.
func TestBufferHelpers(t *testing.T) {
	b := NewStereoBuffer(8)
	if b.Cap() != 8 || b.Len() != 0 {
		t.Fatalf("fresh buffer cap/len = %d/%d", b.Cap(), b.Len())
	}
	b.SetLen(4)
	if b.Len() != 4 {
		t.Fatalf("len = %d, want 4", b.Len())
	}
	for i := range b.Left {
		b.Left[i], b.Right[i] = 1, 2
	}
	c := NewStereoBuffer(8)
	c.Copy(b)
	if c.Len() != 4 || c.Left[0] != 1 || c.Right[3] != 2 {
		t.Fatalf("copy = %+v", c)
	}
	c.Clear()
	if c.Left[0] != 0 || c.Right[0] != 0 {
		t.Fatal("clear did not silence")
	}
	b.SetLen(2)
	c.SetLen(4)
	c.Mix(b) // mixes the overlapping 2 frames
	if c.Left[0] != 1 || c.Left[2] != 0 {
		t.Fatalf("mix = %v", c.Left)
	}
	c.Clear()
	c.MixGain(b, 0.5)
	if c.Left[0] != 0.5 {
		t.Fatalf("mixgain = %v", c.Left)
	}
	b.SetLen(100) // clamps to capacity
	if b.Len() != 8 {
		t.Fatalf("overlong SetLen = %d, want 8", b.Len())
	}
	b.SetLen(-3)
	if b.Len() != 0 {
		t.Fatalf("negative SetLen = %d, want 0", b.Len())
	}
	neg := NewStereoBuffer(-4)
	if neg.Cap() != 0 {
		t.Fatalf("negative capacity = %d", neg.Cap())
	}
	b.Reset()
	if b.Len() != 0 {
		t.Fatal("reset did not truncate")
	}
}

// TestSawOscillator checks the sawtooth shape, retune, and Fill.
func TestSawOscillator(t *testing.T) {
	s := NewSaw()
	s.SetFrequency(48000, 48000) // one cycle per frame: ramp restarts
	if got := s.Next(); got != -1 {
		t.Fatalf("saw start = %v, want -1", got)
	}
	if s.Phase() != 0 {
		t.Fatalf("wrapped phase = %v, want 0", s.Phase())
	}
	s.SetFrequency(0, 0) // non-positive rate keeps the increment
	buf := make([]float32, 16)
	s.Fill(buf)
	for i := 1; i < len(buf); i++ {
		if buf[i] < buf[i-1] && buf[i] != -1 {
			t.Fatalf("saw not ramping at %d: %v", i, buf)
		}
	}
	n := NewNoise(0)
	n.Fill(buf)
	seen := false
	for _, v := range buf {
		if v != 0 {
			seen = true
		}
		if v < -1 || v >= 1 {
			t.Fatalf("noise %v out of [-1,1)", v)
		}
	}
	if !seen {
		t.Fatal("noise Fill produced only zeros")
	}
}

// TestLimiterAccessors covers the setter/getter surface.
func TestLimiterAccessors(t *testing.T) {
	l := NewLimiter(48000)
	l.SetKneeDB(12)
	l.SetCeiling(0.9)
	if l.Ceiling() != 0.9 {
		t.Fatalf("ceiling = %v", l.Ceiling())
	}
	if l.AutoGainEnabled() != true {
		t.Fatal("auto-gain should default on")
	}
	l.SetAutoGain(false)
	if l.AutoGainEnabled() {
		t.Fatal("auto-gain disable ignored")
	}
	l.SetTargetLUFS(-20)
	if l.TargetLUFS() != -20 {
		t.Fatalf("target = %v", l.TargetLUFS())
	}
	l.SetAutoGain(true)
	if got := l.GainReductionDB(); got > 0 {
		t.Fatalf("fresh gain reduction = %v dB, want <= 0", got)
	}
	l.gain = 0 // force the -120 dB floor branch
	if got := l.GainReductionDB(); got != -120 {
		t.Fatalf("floored reduction = %v, want -120", got)
	}
	if got := l.LoudnessLUFS(); got != -70 {
		t.Fatalf("silent loudness = %v, want -70 floor", got)
	}
}

// TestFilterAccessors covers biquad retunes and reporting.
func TestFilterAccessors(t *testing.T) {
	f := NewBiquad(Peaking, 1000, 1, 48000)
	f.SetGainDB(6)
	if f.GainDB() != 6 {
		t.Fatalf("gain = %v", f.GainDB())
	}
	f.SetQ(2)
	if f.Q() != 2 {
		t.Fatalf("q = %v", f.Q())
	}
	f.SetQ(0) // falls back to Butterworth
	if f.Q() != defaultQ {
		t.Fatalf("zero q = %v, want %v", f.Q(), defaultQ)
	}
	if f.Frequency() != 1000 {
		t.Fatalf("freq = %v", f.Frequency())
	}
	f.SetFrequency(1e9) // clamps below Nyquist
	if f.Frequency() >= 24000 {
		t.Fatalf("unclamped freq = %v", f.Frequency())
	}
	f.SetFrequency(-5) // clamps above zero
	if f.Frequency() <= 0 {
		t.Fatalf("non-positive freq = %v", f.Frequency())
	}
}

// TestEQMisc covers reporting, reset, and the aliased copy path.
func TestEQMisc(t *testing.T) {
	e := NewEQ(48000, EQSpec{})
	if e.Bands() != 0 || e.Spec().HPF != 0 {
		t.Fatalf("empty EQ = %+v", e.Spec())
	}
	in := []float32{0.5, -0.5, 0.25}
	out := make([]float32, 3)
	e.Process(out, in) // bypass copies through
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("bypass[%d] = %v, want %v", i, out[i], in[i])
		}
	}
	e.Reset() // must not panic on idle stages
}

// TestDelayAccessors covers delay reporting helpers.
func TestDelayAccessors(t *testing.T) {
	d := NewStereoDelay(48000, MaxDelaySeconds)
	d.Configure(DelayConfig{Subdivision: SubdivisionQuarter, Feedback: 0.5, DampingHz: 4000, Wet: 0.5}, 120)
	if d.SampleRate() != 48000 || d.Mode() != DelayModeStereo {
		t.Fatalf("rate/mode = %d/%d", d.SampleRate(), d.Mode())
	}
	if got := d.DampingHz(); math.Abs(float64(got)-4000) > 5 {
		t.Fatalf("damping round trip = %v, want ~4000", got)
	}
	if d.MemoryBytes() <= 1500000 {
		t.Fatalf("memory = %d, want ~2 MiB", d.MemoryBytes())
	}
	quiet := NewStereoDelay(0, 0) // falls back to 48 kHz / max seconds
	if quiet.SampleRate() != 48000 {
		t.Fatalf("fallback rate = %d", quiet.SampleRate())
	}
}

// TestReverbParamsReporter covers the Params accessor.
func TestReverbParamsReporter(t *testing.T) {
	p := ReverbParams{RoomSize: 0.7, Damping: 0.3, Wet: 0.5, Dry: 0.5, Width: 0.8}
	r := NewReverb(48000, p)
	if got := r.Params(); got != p {
		t.Fatalf("params = %+v, want %+v", got, p)
	}
}

// TestOscillatorPhase covers the Phase reporters.
func TestOscillatorPhase(t *testing.T) {
	s := NewSine()
	if s.Phase() != 0 {
		t.Fatalf("phase = %v", s.Phase())
	}
	saw := NewSaw()
	saw.SetFrequency(12000, 48000)
	saw.Next()
	if saw.Phase() != 0.25 {
		t.Fatalf("saw phase = %v, want 0.25", saw.Phase())
	}
}

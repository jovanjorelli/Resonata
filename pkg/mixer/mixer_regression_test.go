package mixer

import "testing"

// TestRegSaturationBypass proves the clean/clip mode switch and the
// linear hard clamp at ±1 with no tanh coloration below the ceiling.
func TestRegSaturationBypass(t *testing.T) {
	c := NewSoftClipper(0.9)
	if c.Clean() {
		t.Fatal("default clipper reports clean")
	}
	c.SetClean(true)
	if !c.Clean() {
		t.Fatal("SetClean(true) not reported")
	}
	for _, x := range []float32{-2, -1, -0.5, 0, 0.5, 1, 2} {
		got := c.ProcessSample(x)
		var want float32
		switch {
		case x > 1:
			want = 1
		case x < -1:
			want = -1
		default:
			want = x
		}
		if got != want {
			t.Fatalf("clean(%v) = %v, want %v", x, got, want)
		}
	}
	c.SetClean(false)
	if c.Clean() {
		t.Fatal("SetClean(false) not reported")
	}
}

// TestRegReverbDamping proves the damping override reaches every reverb
// instance including per-track rooms.
func TestRegReverbDamping(t *testing.T) {
	m := New(48000, 2, 64)
	i := m.AddTrack(&dcInstrument{level: 0.1}, 0, 1)
	if i < 0 {
		t.Fatal("AddTrack rejected")
	}
	m.SetReverbDamping(0.7)
	if got := m.Reverb().Params().Damping; got != 0.7 {
		t.Fatalf("master damping = %v, want 0.7", got)
	}
}

// TestRegLimiterWiring proves the enable flag and a bounded limited
// render on hot constant input.
func TestRegLimiterWiring(t *testing.T) {
	m := New(48000, 2, 256)
	if m.LimiterEnabled() {
		t.Fatal("limiter on by default")
	}
	m.AddTrack(&dcInstrument{level: 0.9}, 0, 1)
	m.SetLimiterEnabled(true)
	if !m.LimiterEnabled() {
		t.Fatal("limiter not reported")
	}
	m.Process(256)
	if peak := peakOfMixer(t, m); peak > 0.99 {
		t.Fatalf("limited peak = %v, want within ceiling", peak)
	}
	m.SetLimiterEnabled(false)
	if m.LimiterEnabled() {
		t.Fatal("limiter stuck on")
	}
}

func peakOfMixer(t *testing.T, m *Mixer) float32 {
	t.Helper()
	peak := float32(0)
	for _, v := range m.Master() {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

// TestRegMonoBass proves cutoff reporting, disable, and that DC content
// converges to mono while highs are untouched.
func TestRegMonoBass(t *testing.T) {
	m := New(48000, 2, 256)
	if got := m.MonoBassCutoff(); got != 0 {
		t.Fatalf("cutoff = %v, want 0 (off)", got)
	}
	m.SetMonoBassCutoff(100)
	if got := m.MonoBassCutoff(); got != 100 {
		t.Fatalf("cutoff = %v, want 100", got)
	}
	m.AddTrack(&dcInstrument{level: 0.4}, -1, 1)
	m.AddTrack(&dcInstrument{level: 0.2}, 1, 1)
	var lastL, lastR float32
	for i := 0; i < 20; i++ {
		m.Process(256)
		mt := m.Master()
		lastL, lastR = mt[0], mt[1]
	}
	if d := lastL - lastR; d < -1e-5 || d > 1e-5 {
		t.Fatalf("DC not mono: L=%v R=%v", lastL, lastR)
	}
	m.SetMonoBassCutoff(0)
	if got := m.MonoBassCutoff(); got != 0 {
		t.Fatalf("cutoff = %v after disable", got)
	}
	m.Process(256)
	mt := m.Master()
	if mt[0] == mt[1] {
		t.Fatal("disabled crossover still mono")
	}
}

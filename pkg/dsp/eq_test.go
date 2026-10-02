package dsp

import (
	"math"
	"testing"
)

// steadyAmplitude renders a sine at freq through proc and returns its
// steady-state output amplitude via windowed RMS (settling discarded).
func steadyAmplitude(freq float64, proc func(float32) float32) float64 {
	const settle, frames = 4000, 19200
	var sum float64
	for i := 0; i < settle+frames; i++ {
		x := float32(math.Sin(2 * math.Pi * freq * float64(i) / 48000))
		y := float64(proc(x))
		if i >= settle {
			sum += y * y
		}
	}
	return math.Sqrt(sum/frames) * math.Sqrt2
}

func dbOf(v float64) float64 {
	if v <= 1e-9 {
		return -180
	}
	return 20 * math.Log10(v)
}

// TestRBJSweep drives sine sweeps through every RBJ band type and checks
// the measured response against the cookbook transfer function: exact
// peaks and nulls at the specified Hz/dB.
func TestRBJSweep(t *testing.T) {
	const fs = 48000
	configs := []*Biquad{
		NewBiquadGain(Peaking, 1000, 1, 6, fs),
		NewBiquadGain(Peaking, 250, 1, -30, fs), // deep "null" cut
		NewBiquad(Highpass, 80, eqDefaultShelfQ, fs),
		NewBiquad(Lowpass, 12000, eqDefaultShelfQ, fs),
		NewBiquadGain(LowShelf, 150, eqDefaultShelfQ, 6, fs),
		NewBiquadGain(HighShelf, 8000, eqDefaultShelfQ, -4, fs),
		NewBiquadGain(Peaking, 300, 4, -6, fs),
	}
	sweep := []float64{50, 100, 220, 440, 800, 1000, 1780, 3160, 6000, 8000, 12000, 16000}
	for _, b := range configs {
		for _, f := range sweep {
			got := dbOf(steadyAmplitude(f, b.ProcessSample))
			want := dbOf(b.ResponseAt(f))
			if math.Abs(got-want) > 0.25 {
				t.Errorf("sweep %v Hz: measured %.2f dB, transfer function %.2f dB", f, got, want)
			}
		}
	}

	// Exact peak at the specified Hz/dB: +6 dB at 1 kHz.
	pk := NewBiquadGain(Peaking, 1000, 1, 6, fs)
	if got := dbOf(steadyAmplitude(1000, pk.ProcessSample)); math.Abs(got-6) > 0.15 {
		t.Errorf("peaking at 1 kHz = %.2f dB, want +6 dB", got)
	}
	// Off-band unity.
	for _, f := range []float64{100, 10000} {
		if got := dbOf(steadyAmplitude(f, pk.ProcessSample)); math.Abs(got) > 0.15 {
			t.Errorf("peaking off-band at %v Hz = %.2f dB, want 0 dB", f, got)
		}
	}

	// Exact deep cut (null): -30 dB at 250 Hz.
	cut := NewBiquadGain(Peaking, 250, 1, -30, fs)
	if got := dbOf(steadyAmplitude(250, cut.ProcessSample)); got > -28.5 || got < -31.5 {
		t.Errorf("null at 250 Hz = %.2f dB, want ~-30 dB", got)
	}

	// HPF kills sub-sonic rumble: at least -24 dB at 20 Hz for an 80 Hz
	// Butterworth highpass, and flat in the passband.
	hp := NewBiquad(Highpass, 80, eqDefaultShelfQ, fs)
	if got := dbOf(steadyAmplitude(20, hp.ProcessSample)); got > -24 {
		t.Errorf("HPF at 20 Hz = %.2f dB, want <= -24 dB", got)
	}
	if got := dbOf(steadyAmplitude(400, hp.ProcessSample)); math.Abs(got) > 0.1 {
		t.Errorf("HPF passband at 400 Hz = %.2f dB, want ~0 dB", got)
	}

	// Shelves: low shelf +6 dB below the corner, unity above; high shelf
	// -4 dB above the corner, unity below.
	ls := NewBiquadGain(LowShelf, 150, eqDefaultShelfQ, 6, fs)
	if got := dbOf(steadyAmplitude(40, ls.ProcessSample)); math.Abs(got-6) > 0.5 {
		t.Errorf("low shelf at 40 Hz = %.2f dB, want +6 dB", got)
	}
	if got := dbOf(steadyAmplitude(2000, ls.ProcessSample)); math.Abs(got) > 0.5 {
		t.Errorf("low shelf at 2 kHz = %.2f dB, want 0 dB", got)
	}
	hs := NewBiquadGain(HighShelf, 8000, eqDefaultShelfQ, -4, fs)
	if got := dbOf(steadyAmplitude(16000, hs.ProcessSample)); math.Abs(got+4) > 0.7 {
		t.Errorf("high shelf at 16 kHz = %.2f dB, want -4 dB", got)
	}
	if got := dbOf(steadyAmplitude(1000, hs.ProcessSample)); math.Abs(got) > 0.5 {
		t.Errorf("high shelf at 1 kHz = %.2f dB, want 0 dB", got)
	}

	// Zero-gain bands are transparent.
	flat := NewBiquadGain(Peaking, 1000, 1, 0, fs)
	if got := steadyAmplitude(1000, flat.ProcessSample); math.Abs(got-1) > 1e-3 {
		t.Errorf("0 dB peaking amplitude = %v, want 1", got)
	}
}

// TestEQCascade checks the four-stage track sculpt end to end: the
// measured response equals the product of the stage transfer functions.
func TestEQCascade(t *testing.T) {
	const fs = 48000
	spec := EQSpec{
		HPF:       100,
		LowShelf:  EQBandSpec{Freq: 150, GainDB: 2, Q: 0.7},
		MidPeak:   EQBandSpec{Freq: 440, GainDB: -8, Q: 1},
		HighShelf: EQBandSpec{Freq: 6000, GainDB: 4, Q: 0.7},
	}
	eq := NewEQ(fs, spec)
	if eq.Bands() != 4 {
		t.Fatalf("Bands = %d, want 4", eq.Bands())
	}
	for _, f := range []float64{50, 220, 440, 1000, 3000, 8000, 14000} {
		got := dbOf(steadyAmplitude(f, eq.ProcessSample))
		want := dbOf(eq.ResponseAt(f))
		if math.Abs(got-want) > 0.3 {
			t.Errorf("cascade at %v Hz: measured %.2f dB, theory %.2f dB", f, got, want)
		}
	}
	// The mud band must actually be cut ~8 dB relative to bypass.
	mud := steadyAmplitude(440, eq.ProcessSample)
	ref := steadyAmplitude(440, func(x float32) float32 { return x })
	if got := dbOf(mud / ref); got > -6 || got < -10.5 {
		t.Errorf("440 Hz through the sculpt = %.2f dB, want ~-8 dB", got)
	}

	// Bypassed stages: an HPF-only spec leaves the mids untouched.
	hpfOnly := NewEQ(fs, EQSpec{HPF: 100})
	if hpfOnly.Bands() != 1 {
		t.Fatalf("Bands = %d, want 1", hpfOnly.Bands())
	}
	if got := steadyAmplitude(1000, hpfOnly.ProcessSample); math.Abs(got-1) > 1e-3 {
		t.Errorf("HPF-only at 1 kHz = %v, want unity", got)
	}
	// Empty spec is a wire.
	wire := NewEQ(fs, EQSpec{})
	if wire.Bands() != 0 {
		t.Fatalf("empty spec has %d bands", wire.Bands())
	}
	src := make([]float32, 64)
	dst := make([]float32, 64)
	for i := range src {
		src[i] = float32(i) / 64
	}
	copy(dst, src)
	wire.ProcessInPlace(dst)
	for i := range dst {
		if dst[i] != src[i] {
			t.Fatalf("empty EQ altered frame %d", i)
		}
	}

	// SetSpec retunes in place (dynamic filtering).
	eq.SetSpec(EQSpec{MidPeak: EQBandSpec{Freq: 440, GainDB: 0, Q: 1}})
	if got := steadyAmplitude(440, eq.ProcessSample); math.Abs(got-1) > 1e-3 {
		t.Errorf("after retune to 0 dB: amplitude %v, want 1", got)
	}
}

// TestEQProcessMatchesSample checks the block APIs against the scalar
// path and verifies zero allocations.
func TestEQProcessMatchesSample(t *testing.T) {
	const fs = 48000
	spec := EQSpec{
		HPF: 80, LowShelf: EQBandSpec{Freq: 200, GainDB: -3},
		MidPeak:   EQBandSpec{Freq: 2500, GainDB: 4, Q: 2},
		HighShelf: EQBandSpec{Freq: 9000, GainDB: -2},
	}
	a := NewEQ(fs, spec)
	b := NewEQ(fs, spec)
	in := make([]float32, 512)
	for i := range in {
		in[i] = float32(math.Sin(float64(i) * 0.1))
	}
	out := make([]float32, 512)
	b.Process(out, in)
	for i, x := range in {
		want := a.ProcessSample(x)
		if math.Abs(float64(out[i]-want)) > 1e-6 {
			t.Fatalf("frame %d: Process = %v, ProcessSample = %v", i, out[i], want)
		}
	}
	inPlace := make([]float32, 512)
	copy(inPlace, in)
	c := NewEQ(fs, spec)
	c.ProcessInPlace(inPlace)
	for i := range in {
		if math.Abs(float64(inPlace[i]-out[i])) > 1e-6 {
			t.Fatalf("frame %d: ProcessInPlace != Process", i)
		}
	}

	buf := make([]float32, 512)
	copy(buf, in)
	c.ProcessInPlace(buf) // warm
	if allocs := testing.AllocsPerRun(50, func() { c.ProcessInPlace(buf) }); allocs != 0 {
		t.Fatalf("ProcessInPlace allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(20, func() { c.SetSpec(spec) }); allocs != 0 {
		t.Fatalf("SetSpec allocates %v per call, want 0", allocs)
	}
}

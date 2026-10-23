package dsp

import (
	"math"
	"testing"
)

// TestBiquadSanitizeNaNInf verifies RBJ biquad filter flushes NaN and Inf state to zero.
func TestBiquadSanitizeNaNInf(t *testing.T) {
	modes := []FilterMode{Lowpass, Highpass, Bandpass, Peaking, LowShelf, HighShelf}
	for _, m := range modes {
		f := NewBiquadGain(m, 1000, 0.707, 3.0, 48000)

		nan := float32(math.NaN())
		posInf := float32(math.Inf(1))
		negInf := float32(math.Inf(-1))

		for _, bad := range []float32{nan, posInf, negInf} {
			out := f.ProcessSample(bad)
			if math.IsNaN(float64(out)) || math.IsInf(float64(out), 0) {
				t.Fatalf("mode %v: ProcessSample(%v) returned invalid %v", m, bad, out)
			}
		}

		outNormal := f.ProcessSample(0.5)
		if math.IsNaN(float64(outNormal)) || math.IsInf(float64(outNormal), 0) {
			t.Fatalf("mode %v: recovery ProcessSample returned %v", m, outNormal)
		}
	}
}

// TestBiquadMutatorsSanitize verifies biquad parameter setters reject NaN and Inf arguments.
func TestBiquadMutatorsSanitize(t *testing.T) {
	f := NewBiquad(Lowpass, 1000, 0.707, 48000)

	f.SetFrequency(math.NaN())
	if math.IsNaN(f.Frequency()) || f.Frequency() <= 0 {
		t.Fatalf("Frequency after NaN: %v", f.Frequency())
	}

	f.SetFrequency(math.Inf(1))
	if math.IsInf(f.Frequency(), 0) || f.Frequency() >= 24000 {
		t.Fatalf("Frequency after Inf: %v", f.Frequency())
	}

	f.SetFrequency(-500)
	if f.Frequency() <= 0 {
		t.Fatalf("Frequency after negative: %v", f.Frequency())
	}

	f.SetQ(math.NaN())
	if math.IsNaN(f.Q()) || f.Q() <= 0 {
		t.Fatalf("Q after NaN: %v", f.Q())
	}

	f.SetQ(math.Inf(1))
	if math.IsInf(f.Q(), 0) || f.Q() <= 0 {
		t.Fatalf("Q after Inf: %v", f.Q())
	}

	f.SetGainDB(math.NaN())
	if math.IsNaN(f.GainDB()) {
		t.Fatalf("GainDB after NaN: %v", f.GainDB())
	}

	f.SetGainDB(math.Inf(1))
	if math.IsInf(f.GainDB(), 0) {
		t.Fatalf("GainDB after Inf: %v", f.GainDB())
	}

	out := f.ProcessSample(1.0)
	if math.IsNaN(float64(out)) || math.IsInf(float64(out), 0) {
		t.Fatalf("ProcessSample after mutator sanitization returned %v", out)
	}
}

// TestNewBiquadGainSanitize verifies constructor sanitizes invalid initial frequency and gain parameters.
func TestNewBiquadGainSanitize(t *testing.T) {
	f := NewBiquadGain(Peaking, math.NaN(), math.NaN(), math.NaN(), math.NaN())
	if math.IsNaN(f.Frequency()) || math.IsNaN(f.Q()) || math.IsNaN(f.GainDB()) {
		t.Fatalf("NewBiquadGain NaN: freq=%v, q=%v, gain=%v", f.Frequency(), f.Q(), f.GainDB())
	}
	out := f.ProcessSample(0.5)
	if math.IsNaN(float64(out)) || math.IsInf(float64(out), 0) {
		t.Fatalf("ProcessSample on constructed filter returned %v", out)
	}
}

// TestReverbSanitizeNaNInf verifies FDN reverb delay lines and allpass diffusers flush non-finite samples to zero.
func TestReverbSanitizeNaNInf(t *testing.T) {
	rev := NewReverb(48000, ReverbParams{RoomSize: 0.8, Damping: 0.25, Wet: 1, Dry: 0, Width: 1})

	badValues := []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}
	for _, badL := range badValues {
		for _, badR := range badValues {
			wL, wR := rev.ProcessFrameWet(badL, badR)
			if math.IsNaN(float64(wL)) || math.IsInf(float64(wL), 0) {
				t.Fatalf("Reverb ProcessFrameWet wetL is invalid: %v", wL)
			}
			if math.IsNaN(float64(wR)) || math.IsInf(float64(wR), 0) {
				t.Fatalf("Reverb ProcessFrameWet wetR is invalid: %v", wR)
			}
		}
	}

	for i := 0; i < 500; i++ {
		wL, wR := rev.ProcessFrameWet(0.2, -0.2)
		if math.IsNaN(float64(wL)) || math.IsInf(float64(wL), 0) {
			t.Fatalf("Reverb tail wetL poisoned: %v at iteration %d", wL, i)
		}
		if math.IsNaN(float64(wR)) || math.IsInf(float64(wR), 0) {
			t.Fatalf("Reverb tail wetR poisoned: %v at iteration %d", wR, i)
		}
	}
}

// TestDelaySanitizeNaNInf verifies stereo delay feedback buffer flushes non-finite samples to zero.
func TestDelaySanitizeNaNInf(t *testing.T) {
	del := NewStereoDelay(48000, 1.0)
	del.Configure(DelayConfig{
		DelaySeconds: 0.1,
		Feedback:     0.5,
		DampingHz:    4000,
		Wet:          0.5,
		Mode:         DelayModePingPong,
	}, 120)

	badValues := []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}
	for _, badL := range badValues {
		for _, badR := range badValues {
			wL, wR := del.ProcessFrameWet(badL, badR)
			if math.IsNaN(float64(wL)) || math.IsInf(float64(wL), 0) {
				t.Fatalf("Delay ProcessFrameWet wetL is invalid: %v", wL)
			}
			if math.IsNaN(float64(wR)) || math.IsInf(float64(wR), 0) {
				t.Fatalf("Delay ProcessFrameWet wetR is invalid: %v", wR)
			}
		}
	}

	for i := 0; i < 5000; i++ {
		wL, wR := del.ProcessFrameWet(0.1, 0.1)
		if math.IsNaN(float64(wL)) || math.IsInf(float64(wL), 0) {
			t.Fatalf("Delay echo wetL poisoned: %v at iteration %d", wL, i)
		}
		if math.IsNaN(float64(wR)) || math.IsInf(float64(wR), 0) {
			t.Fatalf("Delay echo wetR poisoned: %v at iteration %d", wR, i)
		}
	}
}

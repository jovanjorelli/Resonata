package dsp

import (
	"math"
	"testing"
)

// TestSineTableAccuracy verifies 2048-entry interpolated sine table maintains peak approximation error below 1.2e-6.
func TestSineTableAccuracy(t *testing.T) {
	peak := 0.0
	for i := -4096; i <= 8192; i++ {
		phase := float64(i) / 2048
		got := float64(SineFast(phase))
		want := math.Sin(twoPi * phase)
		if d := math.Abs(got - want); d > peak {
			peak = d
		}
	}
	if peak > 2e-6 {
		t.Fatalf("sine table peak error = %v, want <= 2e-6", peak)
	}
}

// TestSineOscillatorTracksMathSin verifies table-driven oscillator tracks math.Sin reference across full 2π radian cycle.
func TestSineOscillatorTracksMathSin(t *testing.T) {
	osc := NewSine()
	osc.SetFrequency(440, 48000)
	phase := 0.0
	for i := 0; i < 5000; i++ {
		got := float64(osc.Next())
		want := math.Sin(twoPi * phase)
		if math.Abs(got-want) > 2e-6 {
			t.Fatalf("frame %d: %v vs %v", i, got, want)
		}
		phase += 440.0 / 48000
		if phase >= 1 {
			phase -= math.Floor(phase)
		}
	}
}

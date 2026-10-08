package dsp

import (
	"math"
	"testing"
)

// TestFinalLoudnessBands proves all three loudness branches: silence
// floor, sub-floor estimate, and a live reading.
func TestFinalLoudnessBands(t *testing.T) {
	l := NewLimiter(48000)
	if got := l.LoudnessLUFS(); got != -70 {
		t.Fatalf("silent = %v", got)
	}
	l.loudMS = 1e-9 // nonzero yet below any musical floor
	if got := l.LoudnessLUFS(); got != -70 {
		t.Fatalf("trace = %v, want floor", got)
	}
	buf := NewStereoBuffer(1024)
	buf.SetLen(1024)
	for i := range buf.Left {
		v := float32(0.00002)
		if i%2 == 1 {
			v = -v
		}
		buf.Left[i] = v
		buf.Right[i] = v
	}
	l.ProcessStereo(buf) // brief whisper: estimate lifts off zero but stays sub-floor
	if got := l.LoudnessLUFS(); got != -70 {
		t.Fatalf("whisper = %v, want floor", got)
	}
	for i := range buf.Left {
		v := float32(0.7)
		if i%2 == 1 {
			v = -v
		}
		buf.Left[i] = v
		buf.Right[i] = v
	}
	for i := 0; i < 300; i++ {
		l.ProcessStereo(buf)
	}
	if got := l.LoudnessLUFS(); got <= -70 || math.IsNaN(float64(got)) {
		t.Fatalf("live = %v", got)
	}
}

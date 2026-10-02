package dsp

import "testing"

// BenchmarkDelayProcess measures the ping-pong echo hot loop with a
// dotted-eighth tape setting. The loop must stay allocation-free.
func BenchmarkDelayProcess(b *testing.B) {
	d := NewStereoDelay(48000, 4.0)
	d.Configure(DelayConfig{
		Mode:        DelayModePingPong,
		Subdivision: SubdivisionEighthD,
		Feedback:    0.6,
		DampingHz:   4000,
		Wet:         0.4,
	}, 120)

	buf := make([]float32, 1024*2) // 1024 frames stereo
	for i := range buf {
		buf[i] = float32(i%17) / 17
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		d.ProcessInterleaved(buf)
	}
}

package dsp

import "testing"

// BenchmarkSine1024 measures raw oscillator throughput: 1024 frames per
// op. Target: under 500 ns for the block on a modern desktop CPU.
func BenchmarkSine1024(b *testing.B) {
	osc := NewSine()
	osc.SetFrequency(440, 48000)
	buf := make([]float32, 1024)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		osc.Fill(buf)
	}
}

// BenchmarkBiquad1024 measures single-stage biquad throughput in place:
// 1024 frames per op. Target: under 800 ns for the block.
func BenchmarkBiquad1024(b *testing.B) {
	f := NewBiquadGain(Peaking, 1000, 1, 3, 48000)
	buf := make([]float32, 1024)
	for i := range buf {
		buf[i] = float32(i%64) / 64
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		f.ProcessInPlace(buf)
	}
}

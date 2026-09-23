package mixer

import "testing"

// BenchmarkMixer8Track measures a full 8-strip mix block: 1024 frames
// per op through instruments, panning, reverb send, and soft clipping.
// Target: under 5 us for the block.
func BenchmarkMixer8Track(b *testing.B) {
	m := New(48000, 8, 1024)
	m.SetReverbWet(0.25)
	for i := 0; i < 8; i++ {
		m.AddTrack(&dcInstrument{level: 0.1}, 0, 0.8)
		m.SetSend(i, 0.3)
	}
	m.Process(1024) // warm every path
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Process(1024)
	}
}

package mixer

import (
	"testing"

	"resonata/pkg/dsp"
)

// parFixture builds a mixer with constant, echo, reverb-send, and muted
// strips: every mix stage except the clipper exercises both paths.
func parFixture() *Mixer {
	m := New(48000, 4, 256)
	m.SetReverbWet(0.5)
	a := m.AddTrack(&dcInstrument{level: 0.2}, -0.5, 0.8)
	m.SetSend(a, 0.4)
	b := m.AddTrack(&dcInstrument{level: -0.3}, 0.5, 0.7)
	m.SetTrackDelay(b, dsp.DelayConfig{DelaySeconds: 0.05, Feedback: 0.4, Wet: 0.5}, 120)
	c := m.AddTrack(&dcInstrument{level: 0.1}, 0, 1)
	m.SetMuted(c, true)
	return m
}

// snap renders blocks and records the master after each one.
func snap(t *testing.T, m *Mixer, blocks int) [][]float32 {
	t.Helper()
	var out [][]float32
	for i := 0; i < blocks; i++ {
		m.Process(256)
		out = append(out, append([]float32{}, m.Master()...))
	}
	return out
}

// TestParallelIdentical proves parallel voicing is bit-identical to the
// sequential path across summing, sends, echo, and mute handling.
func TestParallelIdentical(t *testing.T) {
	seq, par := parFixture(), parFixture()
	par.SetParallel(true)
	if !par.Parallel() || seq.Parallel() {
		t.Fatal("parallel flag misreported")
	}
	a, b := snap(t, seq, 8), snap(t, par, 8)
	for i := range a {
		if len(a[i]) != len(b[i]) {
			t.Fatalf("block %d lens %d/%d", i, len(a[i]), len(b[i]))
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("block %d frame %d: %v vs %v", i, j, a[i][j], b[i][j])
			}
		}
	}
}

// TestParallelZeroAlloc proves the parallel block allocates nothing
// after pool warmup: jobs, acks, and scratches are all pre-owned.
func TestParallelZeroAlloc(t *testing.T) {
	m := parFixture()
	m.SetParallel(true)
	m.Process(256) // warmup: starts the shared pool
	if allocs := testing.AllocsPerRun(30, func() { m.Process(256) }); allocs != 0 {
		t.Fatalf("parallel Process allocs = %v", allocs)
	}
}

// TestParallelToggle proves flipping the flag mid-stream keeps both
// paths exact: toggle points render identically either way.
func TestParallelToggle(t *testing.T) {
	a, b := parFixture(), parFixture()
	b.SetParallel(true)
	for i := 0; i < 4; i++ {
		a.Process(256)
		b.Process(256)
	}
	a.SetParallel(true)
	b.SetParallel(false)
	for i := 0; i < 4; i++ {
		a.Process(256)
		b.Process(256)
		am, bm := a.Master(), b.Master()
		for j := range am {
			if am[j] != bm[j] {
				t.Fatalf("toggled block %d frame %d differs", i, j)
			}
		}
	}
}

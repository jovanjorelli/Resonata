package engine

import (
	"math"
	"testing"

	"resonata/pkg/score"
)

// delayAuthorityScore builds a single-ocarina score with an explicit wet
// level and full send. Reverb is off so the delay bus is the only effect.
// At 120 BPM the 1/8 subdivision lands echoes every 0.25 s inside the
// 1 s note.
func delayAuthorityScore(wet float32) *score.Score {
	return &score.Score{
		Metadata: score.Metadata{Title: "Authority", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID: "solo", Name: "Solo", Pan: 0, Volume: 0.8,
			Instrument: score.InstrumentDef{Type: "ocarina",
				Parameters: map[string]float32{"breath_noise": 0.2}},
			Delay: &score.TrackDelay{
				Mode: "stereo", Subdivision: "1/8",
				Feedback: 0.5, DampingHz: 4000, Wet: wet, Send: 1,
			},
			Notes: []score.NoteEvent{{Time: 0, Duration: 1.0, Pitch: 69, Velocity: 0.9}},
		}},
	}
}

// renderFull renders the whole score and returns a copy of the
// interleaved stereo master.
func renderFull(t *testing.T, s *score.Score) []float32 {
	t.Helper()
	eng, err := New(s, 48000, 1024)
	if err != nil {
		t.Fatal(err)
	}
	eng.Mixer().SetReverbWet(0)
	var out []float32
	remaining := eng.TotalFrames()
	for remaining > 0 {
		n := eng.BlockSize()
		if n > remaining {
			n = remaining
		}
		eng.ProcessFrames(n, func(master []float32) {
			out = append(out, master...)
		})
		remaining -= n
	}
	return out
}

// maxAbsDiff returns the largest sample-wise magnitude between two renders.
func maxAbsDiff(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	peak := 0.0
	for i := 0; i < n; i++ {
		if d := math.Abs(float64(a[i] - b[i])); d > peak {
			peak = d
		}
	}
	return peak
}

// dryReference renders the same musical content with no delay block.
func dryReference(t *testing.T) []float32 {
	t.Helper()
	s := delayAuthorityScore(0)
	s.Tracks[0].Delay = nil
	return renderFull(t, s)
}

// TestDelayWetZeroIsDry proves JSON authority: wet 0 with send 1 renders
// bit-identical (within float epsilon) to the dry signal.
func TestDelayWetZeroIsDry(t *testing.T) {
	muted := renderFull(t, delayAuthorityScore(0))
	dry := dryReference(t)
	if len(muted) != len(dry) {
		t.Fatalf("lengths differ: %d vs %d", len(muted), len(dry))
	}
	if diff := maxAbsDiff(muted, dry); diff > 1e-6 {
		t.Fatalf("wet=0 differs from dry by %v, want <= 1e-6", diff)
	}
	if got := len(muted); got == 0 {
		t.Fatal("empty render")
	}
}

// TestDelayWetOneDiffers proves the echo path is live: wet 1 must add
// audible repeats that the dry signal lacks.
func TestDelayWetOneDiffers(t *testing.T) {
	wet := renderFull(t, delayAuthorityScore(1))
	dry := dryReference(t)
	if len(wet) != len(dry) {
		t.Fatalf("lengths differ: %d vs %d", len(wet), len(dry))
	}
	if diff := maxAbsDiff(wet, dry); diff < 1e-3 {
		t.Fatalf("wet=1 differs from dry by only %v, want >= 1e-3", diff)
	}
}

package engine

import (
	"testing"

	"resonata/pkg/score"
)

// stressScore builds an 8-track ocarina score spanning pan, EQ, delay,
// and reverb sends so every mix stage runs under the stress test.
func stressScore() *score.Score {
	tracks := make([]score.Track, 0, 8)
	for i := 0; i < 8; i++ {
		pan := -0.75 + 0.25*float32(i%4)
		tr := score.Track{
			ID:         string(rune('a' + i)),
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Pan:        pan,
			Volume:     0.7,
			ReverbSend: 0.3,
			Notes: []score.NoteEvent{
				{Time: 0, Duration: 0.8, Pitch: 60 + i, Velocity: 0.8},
				{Time: 1.0, Duration: 0.8, Pitch: 64 + i, Velocity: 0.7},
			},
		}
		if i%2 == 0 {
			tr.EQ = score.TrackEQ{HPF: 80}
		}
		if i%3 == 0 {
			tr.Delay = &score.TrackDelay{Seconds: 0.05, Feedback: 0.4, Wet: 0.3}
		}
		tracks = append(tracks, tr)
	}
	return &score.Score{
		Metadata: score.Metadata{Title: "Stress", BPM: 120, TimeSignature: "4/4"},
		Tracks:   tracks,
	}
}

// renderAll renders the whole score and returns every master frame.
func renderAll(t *testing.T, parallel bool) []float32 {
	t.Helper()
	s := stressScore()
	if err := score.Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
	e, err := New(s, 48000, DefaultBlockSize)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.Mixer().SetParallel(parallel)
	var got []float32
	for done := 0; done < e.TotalFrames(); {
		n := e.BlockSize()
		if e.TotalFrames()-done < n {
			n = e.TotalFrames() - done
		}
		e.ProcessFrames(n, func(master []float32) {
			got = append(got, master...)
		})
		done += n
	}
	return got
}

// TestParallelRaceStress renders a multi-track score with parallel
// voicing ten times and demands byte-identical output every run: shared
// worker pool, per-track state, and sequential summing must hold under
// the race detector across repeated runs.
func TestParallelRaceStress(t *testing.T) {
	ref := renderAll(t, false)
	for i := 0; i < 10; i++ {
		got := renderAll(t, true)
		if len(got) != len(ref) {
			t.Fatalf("run %d lens %d/%d", i, len(got), len(ref))
		}
		for j := range ref {
			if got[j] != ref[j] {
				t.Fatalf("run %d frame %d differs", i, j)
			}
		}
	}
}

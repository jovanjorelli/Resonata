package engine

import (
	"testing"

	"resonata/pkg/score"
)

// largeScore builds a 50-track, 60-second deterministic stress score:
// ocarina voices with staggered sustained notes across the full minute.
func largeScore() *score.Score {
	s := &score.Score{Metadata: score.Metadata{Title: "Large", BPM: 120, TimeSignature: "4/4"}}
	for t := 0; t < 50; t++ {
		tr := score.Track{
			ID:         string(rune('a'+t/26)) + string(rune('a'+t%26)),
			Name:       "Voice",
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Volume:     0.5,
		}
		if t%2 == 0 {
			tr.Pan = -0.3
		} else {
			tr.Pan = 0.3
		}
		for n := 0; n < 12; n++ {
			start := float64(n*5 + t%5)
			tr.Notes = append(tr.Notes, score.NoteEvent{
				Time:     start,
				Duration: 4.5,
				Pitch:    48 + (t+n)%36,
				Velocity: 0.7,
			})
		}
		s.Tracks = append(s.Tracks, tr)
	}
	return s
}

// BenchmarkEngineFullRender renders the full 50-track minute once per
// op. Run with -benchtime=1x: each op is seconds of audio synthesis.
// Target: under 3 s per render on a modern desktop CPU.
func BenchmarkEngineFullRender(b *testing.B) {
	s := largeScore()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		eng, err := New(s, 48000, DefaultBlockSize)
		if err != nil {
			b.Fatal(err)
		}
		eng.Mixer().SetReverbWet(0)
		total, done := eng.TotalFrames(), 0
		for done < total {
			n := eng.BlockSize()
			if total-done < n {
				n = total - done
			}
			eng.ProcessFrames(n, nil)
			done += n
		}
	}
}

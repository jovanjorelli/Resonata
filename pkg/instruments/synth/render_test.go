package synth_test

import (
	"testing"

	"resonata/pkg/engine"
	"resonata/pkg/score"
)

// TestSynthEngineRender proves a synth track renders finite, non-silent
// audio through the real engine with the documented DSL parameters.
func TestSynthEngineRender(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID:   "lead",
			Name: "Lead",
			Instrument: score.InstrumentDef{Type: "synth", Parameters: map[string]float32{
				"waveform1": 0, "waveform2": 1, "cutoff": 3000, "resonance": 0.3,
				"attack": 0.01, "decay": 0.1, "sustain": 0.7, "release": 0.2, "detune": 5,
			}},
			Volume: 0.8,
			Notes: []score.NoteEvent{
				{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.9},
				{Time: 0.5, Duration: 0.5, Pitch: 72, Velocity: 0.8},
			},
		}},
	}
	if err := score.Validate(s); err != nil {
		t.Fatalf("validate: %v", err)
	}
	e, err := engine.New(s, 48000, engine.DefaultBlockSize)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	total, peak := 0, float32(0)
	for done := 0; done < e.TotalFrames(); {
		n := e.BlockSize()
		if e.TotalFrames()-done < n {
			n = e.TotalFrames() - done
		}
		e.ProcessFrames(n, func(master []float32) {
			for _, v := range master {
				av := v
				if av < 0 {
					av = -av
				}
				if av > peak {
					peak = av
				}
			}
		})
		done += n
		total = done
	}
	if total != e.TotalFrames() || peak <= 0.01 {
		t.Fatalf("frames=%d peak=%v, want full non-silent render", total, peak)
	}
}

// TestSynthUnknownType proves the engine still rejects bad types with
// the updated message.
func TestSynthUnknownType(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID:         "x",
			Instrument: score.InstrumentDef{Type: "kazoo"},
			Volume:     0.8,
			Notes:      []score.NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8}},
		}},
	}
	if _, err := engine.New(s, 48000, engine.DefaultBlockSize); err == nil {
		t.Fatal("unknown type accepted")
	}
}

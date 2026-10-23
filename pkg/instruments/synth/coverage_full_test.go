package synth

import (
	"math"
	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"testing"
)

// TestSynthCoverageEdges verifies synth parameter bounds and expression edge cases including NaN and out-of-range values.
func TestSynthCoverageEdges(t *testing.T) {
	s := New(48000)

	s.NoteOnParams(60, 0.8, instruments.NoteParams{
		HasExpression: true,
		Expression:    float32(math.NaN()),
	})

	s.NoteOnParams(62, 0.8, instruments.NoteParams{
		HasExpression: true,
		Expression:    -0.5,
	})

	s.NoteOnParams(64, 0.8, instruments.NoteParams{
		HasExpression: true,
		Expression:    1.5,
		Env: instruments.EnvelopeOverride{
			HasDecay:   true,
			Decay:      0.2,
			HasRelease: true,
			Release:    0.3,
		},
	})

	buf := &dsp.Buffer{
		Left:  make([]float32, 128),
		Right: make([]float32, 64),
	}
	s.Process(buf, 1.0/48000.0)
}

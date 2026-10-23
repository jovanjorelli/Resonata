package ocarina

import (
	"math"
	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"testing"
)

// TestOcarinaCoverageEdges verifies parameter edge cases and envelope override handling on the ocarina model.
func TestOcarinaCoverageEdges(t *testing.T) {
	o := New(48000)

	o.NoteOnParams(60, 0.8, instruments.NoteParams{
		Env: instruments.EnvelopeOverride{
			HasAttack:  true,
			Attack:     0.05,
			HasDecay:   true,
			Decay:      0.1,
			HasRelease: true,
			Release:    0.2,
		},
		HasVibrato:    true,
		VibratoRate:   5.0,
		VibratoDepth:  1.0,
		HasExpression: true,
		Expression:    float32(math.NaN()),
	})

	o.NoteOnParams(62, 0.8, instruments.NoteParams{
		HasExpression: true,
		Expression:    -0.5,
	})

	o.NoteOnParams(64, 0.8, instruments.NoteParams{
		HasExpression: true,
		Expression:    1.5,
	})

	o.Process(nil, 1.0/48000.0)

	buf := &dsp.Buffer{
		Left:  make([]float32, 64),
		Right: make([]float32, 64),
	}
	o.Process(buf, 0)
}

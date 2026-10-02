package ocarina

import (
	"math"
	"testing"

	"resonata/pkg/instruments"
)

// TestVibratoOcarina replaces the default LFO rate and depth with score
// vibrato (semitones become fractional modulation) and scales output by
// expression.
func TestVibratoOcarina(t *testing.T) {
	o := New(48000)
	o.NoteOnParams(69, 0.8, instruments.NoteParams{
		HasVibrato: true, VibratoRate: 6.0, VibratoDepth: 0.05,
		Expression: 0.5, HasExpression: true,
	})
	if got := o.VibratoRate(); got != 6.0 {
		t.Fatalf("vibrato rate = %v, want 6.0", got)
	}
	wantDepth := float32(math.Pow(2, 0.05/12) - 1) // fractional, not raw semitones
	if got := o.VibratoDepth(); got != wantDepth || got <= 0 || got >= 0.01 {
		t.Fatalf("vibrato depth = %v, want converted %v in (0, 0.01)", got, wantDepth)
	}
	if o.expr != 0.5 {
		t.Fatalf("expression = %v, want 0.5", o.expr)
	}

	plain := New(48000)
	plain.NoteOn(69, 0.8)
	if plain.VibratoRate() != defaultVibratoRate || plain.VibratoDepth() != defaultVibratoDepth {
		t.Fatalf("plain vibrato = %v/%v, want defaults", plain.VibratoRate(), plain.VibratoDepth())
	}
	if plain.expr != 1 {
		t.Fatalf("plain expression = %v, want 1.0", plain.expr)
	}
}

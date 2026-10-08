package engine

import (
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// gapPlain is a minimal Instrument without Params/Envelope support.
type gapPlain struct {
	ons    int
	last   int
	vel    float32
	offs   int
	params map[string]float32
}

func (g *gapPlain) Process(*dsp.Buffer, float64)       {}
func (g *gapPlain) NoteOn(p int, v float32)            { g.ons++; g.last = p; g.vel = v }
func (g *gapPlain) NoteOff(p int)                      { g.offs++ }
func (g *gapPlain) SetParameters(p map[string]float32) { g.params = p }

// gapFull supports both optional payload interfaces.
type gapFull struct {
	gapPlain
	paramsCalls int
	exCalls     int
	lastEnv     instruments.EnvelopeOverride
	lastParams  instruments.NoteParams
}

func (g *gapFull) NoteOnParams(p int, v float32, np instruments.NoteParams) {
	g.paramsCalls++
	g.lastParams = np
}
func (g *gapFull) NoteOnEx(p int, v float32, e instruments.EnvelopeOverride) {
	g.exCalls++
	g.lastEnv = e
}

var (
	_ instruments.Instrument     = (*gapPlain)(nil)
	_ instruments.ParamsVoicer   = (*gapFull)(nil)
	_ instruments.EnvelopeVoicer = (*gapFull)(nil)
)

// TestGapEqShimFallback proves the EQ wrapper falls back to plain NoteOn
// when the inner voice lacks payload support.
func TestGapEqShimFallback(t *testing.T) {
	inner := &gapPlain{}
	w := &eqInstrument{inner: inner, eqL: dsp.NewEQ(48000, dsp.EQSpec{}), eqR: dsp.NewEQ(48000, dsp.EQSpec{})}
	w.NoteOn(60, 0.5)
	if inner.ons != 1 || inner.last != 60 {
		t.Fatalf("fallback NoteOn not forwarded: %+v", inner)
	}
	w.NoteOnParams(61, 0.6, instruments.NoteParams{HasExpression: true, Expression: 0.5})
	if inner.ons != 2 || inner.last != 61 {
		t.Fatalf("NoteOnParams fallback not forwarded: %+v", inner)
	}
	w.NoteOnEx(62, 0.7, instruments.EnvelopeOverride{Attack: 0.1, HasAttack: true})
	if inner.ons != 3 || inner.last != 62 {
		t.Fatalf("NoteOnEx fallback not forwarded: %+v", inner)
	}
	w.NoteOff(60)
	if inner.offs != 1 {
		t.Fatalf("NoteOff not forwarded: %+v", inner)
	}
	m := map[string]float32{"x": 1}
	w.SetParameters(m)
	if inner.params["x"] != 1 {
		t.Fatalf("SetParameters not forwarded: %+v", inner.params)
	}
}

// TestGapEqShimForward proves the EQ wrapper prefers payload methods when
// the inner voice implements them.
func TestGapEqShimForward(t *testing.T) {
	inner := &gapFull{}
	w := &eqInstrument{inner: inner, eqL: dsp.NewEQ(48000, dsp.EQSpec{}), eqR: dsp.NewEQ(48000, dsp.EQSpec{})}
	np := instruments.NoteParams{HasExpression: true, Expression: 0.4}
	w.NoteOnParams(64, 0.8, np)
	if inner.paramsCalls != 1 || inner.lastParams.Expression != 0.4 {
		t.Fatalf("NoteOnParams not preferred: %+v", inner.lastParams)
	}
	env := instruments.EnvelopeOverride{Release: 0.9, HasRelease: true}
	w.NoteOnEx(65, 0.8, env)
	if inner.exCalls != 1 || !inner.lastEnv.HasRelease {
		t.Fatalf("NoteOnEx not preferred: %+v", inner.lastEnv)
	}
	if inner.ons != 0 {
		t.Fatalf("plain NoteOn called %d times, want 0 when payload supported", inner.ons)
	}
}

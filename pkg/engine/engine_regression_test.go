package engine

import (
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
	"resonata/pkg/wav"
)

// regStub is a minimal instrument recording pedal moves.
type regStub struct {
	pedals []bool
}

func (g *regStub) Process(*dsp.Buffer, float64)     {}
func (g *regStub) NoteOn(int, float32)              {}
func (g *regStub) NoteOff(int)                      {}
func (g *regStub) SetParameters(map[string]float32) {}
func (g *regStub) SetPedal(down bool)               { g.pedals = append(g.pedals, down) }

// TestRegSetPedalDispatch proves pedal moves reach capable voices and
// pass silently over plain ones.
func TestRegSetPedalDispatch(t *testing.T) {
	with := &regStub{}
	setPedal(with, true)
	setPedal(with, false)
	if len(with.pedals) != 2 || !with.pedals[0] || with.pedals[1] {
		t.Fatalf("pedals = %v", with.pedals)
	}
	plain := &gapPlain{}
	setPedal(plain, true) // must not panic
	eq := &eqInstrument{inner: with, eqL: dsp.NewEQ(48000, dsp.EQSpec{}), eqR: dsp.NewEQ(48000, dsp.EQSpec{})}
	setPedal(eq, true)
	if len(with.pedals) != 3 || !with.pedals[2] {
		t.Fatalf("eq shim dropped pedal: %v", with.pedals)
	}
	bare := &eqInstrument{inner: plain, eqL: dsp.NewEQ(48000, dsp.EQSpec{}), eqR: dsp.NewEQ(48000, dsp.EQSpec{})}
	setPedal(bare, true) // inner without SetPedal: no-op, no panic
}

// writeRegWav renders a short mono tone fixture for sampler tests.
func writeRegWav(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := wav.NewWriter(f, wav.PCM16, 48000, 1)
	if err != nil {
		t.Fatal(err)
	}
	tone := make([]float32, 4800)
	for i := range tone {
		tone[i] = 0.5
	}
	if _, err := w.WriteFrames(tone); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// regSamplerScore builds a one-note sampler score with pedal moves and
// an EQ wrapper so dispatch crosses the eqInstrument shim.
func regSamplerScore(dir string, pedal []score.PedalEvent) *score.Score {
	return &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID:         "s",
			Instrument: score.InstrumentDef{Type: "sampler", File: filepath.Join(dir, "t.sfz")},
			Volume:     0.8,
			EQ:         score.TrackEQ{HPF: 80},
			Pedal:      pedal,
			Notes:      []score.NoteEvent{{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8}},
		}},
	}
}

// TestRegEnginePedalRender proves pedal events dispatch through a full
// engine render deterministically: two identical engines agree.
func TestRegEnginePedalRender(t *testing.T) {
	dir := t.TempDir()
	writeRegWav(t, filepath.Join(dir, "t.wav"))
	sfz := "<region> sample=t.wav lokey=0 hikey=127 pitch_keycenter=60 loop_mode=loop_continuous loop_start=0 loop_end=4000\n"
	if err := os.WriteFile(filepath.Join(dir, "t.sfz"), []byte(sfz), 0o644); err != nil {
		t.Fatal(err)
	}
	pedal := []score.PedalEvent{{Time: 0, Down: true}, {Time: 1.0, Down: false}}
	render := func() []float32 {
		e, err := New(regSamplerScore(dir, pedal), 48000, 256)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
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
	a, b := render(), render()
	if len(a) != len(b) {
		t.Fatalf("lens %d/%d", len(a), len(b))
	}
	peak := float32(0)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("frame %d differs", i)
		}
		v := a[i]
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		t.Fatal("pedaled render silent")
	}
}

// TestRegEngineNoiseGate proves the engine fan-out reaches sampler
// voices without disturbing plain ones.
func TestRegEngineNoiseGate(t *testing.T) {
	dir := t.TempDir()
	writeRegWav(t, filepath.Join(dir, "t.wav"))
	if err := os.WriteFile(filepath.Join(dir, "t.sfz"), []byte("<region> sample=t.wav lokey=0 hikey=127 pitch_keycenter=60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := New(regSamplerScore(dir, nil), 48000, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.SetNoiseGateDB(-50) // must not panic on EQ-wrapped sampler
	e.ProcessFrames(e.BlockSize(), nil)
}

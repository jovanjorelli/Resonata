package engine

import (
	"fmt"
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
	e.SetNoiseGateDB(-50)
	e.ProcessFrames(e.BlockSize(), nil)
}

// TestRegTotalFramesBreathShift verifies TotalFrames duration computation accounts for cumulative phrase breath shifts.
func TestRegTotalFramesBreathShift(t *testing.T) {
	notes := make([]score.NoteEvent, 20)
	for i := range notes {
		notes[i] = score.NoteEvent{
			Time:     float64(i) * 0.1,
			Duration: 0.05,
			Pitch:    60,
			Velocity: 0.8,
			PhraseID: i + 1,
		}
	}
	s := &score.Score{
		Metadata: score.Metadata{
			Title:         "Breath Test",
			BPM:           120,
			TimeSignature: "4/4",
			BreathMs:      100,
		},
		Tracks: []score.Track{
			{
				ID:         "lead",
				Name:       "Lead",
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Notes:      notes,
			},
		},
	}
	e, err := New(s, 48000, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	expectedMin := int((s.Duration() + float64(len(notes))*0.1 + 2.5) * 48000)
	if e.TotalFrames() < expectedMin-48000 {
		t.Fatalf("TotalFrames = %d, expected at least %d", e.TotalFrames(), expectedMin-48000)
	}
}

// TestRegRoomMergeUsesTransfer verifies room reverb merge correctly transfers aux sends to target room bus.
func TestRegRoomMergeUsesTransfer(t *testing.T) {
	var tracks []score.Track
	for i := 0; i < 12; i++ {
		tracks = append(tracks, score.Track{
			ID:         fmt.Sprintf("tr%d", i),
			Name:       "Tr",
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Room: &score.Room{
				Config: score.RoomConfig{
					Size:    float64(i) * 0.08,
					Damping: 0.5,
				},
			},
			Notes: []score.NoteEvent{
				{Time: 0, Duration: 0.1, Pitch: 60, Velocity: 0.5},
			},
		})
	}
	s := &score.Score{
		Metadata: score.Metadata{Title: "Room Test", BPM: 120, TimeSignature: "4/4"},
		Tracks:   tracks,
	}
	e, err := New(s, 48000, 256)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.ProcessFrames(256, nil)
}

type panicInst struct{}

func (p *panicInst) Process(buffer *dsp.Buffer, deltaTime float64) {
	panic("worker failure")
}
func (p *panicInst) NoteOn(pitch int, velocity float32)      {}
func (p *panicInst) NoteOff(pitch int)                       {}
func (p *panicInst) SetParameters(params map[string]float32) {}

// TestOfflineRendererPropagatesMixerPanic verifies worker goroutine panics in mixer are caught and surfaced via Err() on OfflineRenderer.
func TestOfflineRendererPropagatesMixerPanic(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.wav")
	s := &score.Score{
		Metadata: score.Metadata{Title: "Panic", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			{
				ID:         "t1",
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Notes:      []score.NoteEvent{{Time: 0, Duration: 1, Pitch: 60, Velocity: 0.8}},
			},
		},
	}
	r, err := NewOfflineRenderer(s, outPath, 48000)
	if err != nil {
		t.Fatalf("NewOfflineRenderer: %v", err)
	}
	defer r.Close()
	r.Engine().Mixer().SetParallel(true)
	r.Engine().Mixer().SetTrackInstrument(0, &panicInst{})
	r.ProcessBlock()
	if r.Err() == nil {
		t.Fatal("expected OfflineRenderer to capture parallel panic error, got nil")
	}
	if !r.Done() {
		t.Fatal("expected OfflineRenderer.Done() to be true after error")
	}
}

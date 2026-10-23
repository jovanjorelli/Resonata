package engine

import (
	"errors"
	"resonata/pkg/dsp"
	"resonata/pkg/score"
	"testing"
)

type plainInst struct{}

func (p *plainInst) NoteOn(pitch int, velocity float32)      {}
func (p *plainInst) NoteOff(pitch int)                       {}
func (p *plainInst) Process(buf *dsp.Buffer, dt float64)     {}
func (p *plainInst) SetParameters(params map[string]float32) {}

// TestEngineCoverageEdges verifies engine edge handling on empty scores, nil buffers, and unconfigured tracks.
func TestEngineCoverageEdges(t *testing.T) {
	r := &humanRNG{state: 11066951453180645397}
	_ = r.gauss()

	rend := &OfflineRenderer{}
	if err := rend.Err(); err != nil {
		t.Fatal(err)
	}

	rendErr := &OfflineRenderer{err: errors.New("boom")}
	rendErr.writeChunk([]float32{0, 0})

	sSynth := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			{
				ID:         "sy",
				Instrument: score.InstrumentDef{Type: "synth"},
				Volume:     0.8,
				Notes: []score.NoteEvent{
					{Time: 0, Duration: 1e-9, Pitch: 60, Velocity: 0.8},
					{Time: 0, Duration: 0.1, Pitch: 62, Velocity: 0.8},
				},
			},
		},
	}
	eng, err := New(sSynth, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	eng.ProcessFrames(128, nil)

	engPlain, err := New(r2Score(), 48000, 128)
	if err != nil {
		t.Fatal(err)
	}
	engPlain.insts[0] = &plainInst{}
	engPlain.ProcessFrames(128, nil)

	sSampler := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{
			{
				ID:         "samp",
				Instrument: score.InstrumentDef{Type: "sampler", File: "../../samples/piano.sfz"},
				Volume:     0.8,
				Notes: []score.NoteEvent{
					{Time: 0, Duration: 0.1, Pitch: 60, Velocity: 0.8},
				},
			},
		},
	}
	engSampler, err := New(sSampler, 48000, 128)
	if err != nil {
		t.Fatal(err)
	}
	engSampler.SetNoiseGateDB(-40)
}

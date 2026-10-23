package mixer

import (
	"resonata/pkg/dsp"
	"testing"
)

type dummyInstrument struct{}

func (d *dummyInstrument) NoteOn(pitch int, velocity float32)         {}
func (d *dummyInstrument) NoteOff(pitch int)                          {}
func (d *dummyInstrument) Process(buf *dsp.Buffer, deltaTime float64) {}
func (d *dummyInstrument) SetParameters(params map[string]float32)    {}

// TestMixerCoverageEdges verifies mixer edge conditions on unallocated strips and invalid track indices.
func TestMixerCoverageEdges(t *testing.T) {
	m := New(48000, 2, 128)
	inst1 := &dummyInstrument{}
	inst2 := &dummyInstrument{}

	idx := m.AddTrack(inst1, 0, 0.8)
	if idx != 0 {
		t.Fatalf("expected track index 0, got %d", idx)
	}

	m.SetTrackInstrument(idx, inst2)
	m.SetTrackInstrument(-1, inst2)
	m.SetTrackInstrument(idx, nil)

	m.SetVolume(-1, 0.5)
	m.SetPan(-1, 0.5)
	m.SetMuted(-1, true)
	m.SetSend(-1, 0.5)

	if m.TrackSend(-1) != 0 {
		t.Fatal("expected 0 for invalid track send")
	}
	if m.Muted(-1) {
		t.Fatal("expected false for invalid track muted")
	}
	if m.TrackPeak(-1) != 0 {
		t.Fatal("expected 0 for invalid track peak")
	}
	if m.TrackDelay(-1) != nil {
		t.Fatal("expected nil for invalid track delay")
	}
	if _, ok := m.TrackRoom(-1); ok {
		t.Fatal("expected false for invalid track room")
	}

	m.SetMonoBassCutoff(0)
	m.SetMonoBassCutoff(-100)
	m.SetMonoBassCutoff(120)
	if m.MonoBassCutoff() != 120 {
		t.Fatalf("expected 120 Hz, got %g", m.MonoBassCutoff())
	}
}

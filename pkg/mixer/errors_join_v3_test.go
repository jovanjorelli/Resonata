package mixer

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"resonata/pkg/dsp"
)

type multiPanicInstrument struct {
	msg string
}

func (p *multiPanicInstrument) Process(b *dsp.Buffer, dt float64) {
	panic(p.msg)
}

func (p *multiPanicInstrument) NoteOn(pitch int, velocity float32) {}

func (p *multiPanicInstrument) NoteOff(pitch int) {}

func (p *multiPanicInstrument) SetParameters(params map[string]float32) {}

// TestMixerSetParallelErrJoin verifies joining worker errors across concurrent render threads.
func TestMixerSetParallelErrJoin(t *testing.T) {
	m := New(48000, 4, 256)
	err1 := errors.New("err one")
	err2 := errors.New("err two")
	err3 := errors.New("err three")

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		m.setParallelErr(err1)
	}()
	go func() {
		defer wg.Done()
		m.setParallelErr(err2)
	}()
	go func() {
		defer wg.Done()
		m.setParallelErr(err3)
	}()
	wg.Wait()

	joined := m.Err()
	if joined == nil {
		t.Fatal("expected joined error, got nil")
	}

	for _, want := range []error{err1, err2, err3} {
		if !errors.Is(joined, want) {
			t.Fatalf("joined error missing target: %v", want)
		}
	}
}

// TestMixerParallelPanicsAggregated verifies worker panic isolation and error aggregation using errors.Join.
func TestMixerParallelPanicsAggregated(t *testing.T) {
	m := New(48000, 2, 256)
	m.SetParallel(true)

	m.AddTrack(&multiPanicInstrument{msg: "crash_track_0"}, 0, 1.0)
	m.AddTrack(&multiPanicInstrument{msg: "crash_track_1"}, 0, 1.0)

	m.Process(64)

	err := m.Err()
	if err == nil {
		t.Fatal("expected aggregated panic errors, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "crash_track_0") {
		t.Fatalf("error missing crash_track_0: %s", errMsg)
	}
	if !strings.Contains(errMsg, "crash_track_1") {
		t.Fatalf("error missing crash_track_1: %s", errMsg)
	}
	if !strings.Contains(errMsg, "track 0 panic") {
		t.Fatalf("error missing track 0 label: %s", errMsg)
	}
	if !strings.Contains(errMsg, "track 1 panic") {
		t.Fatalf("error missing track 1 label: %s", errMsg)
	}
}

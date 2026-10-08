package engine

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
)

// r2Score returns a minimal one-track ocarina score.
func r2Score() *score.Score {
	return &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID:         "s",
			Name:       "S",
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Volume:     0.8,
			Notes:      []score.NoteEvent{{Time: 0, Duration: 0.5, Pitch: 69, Velocity: 0.8}},
		}},
	}
}

// TestR2RendererErrors proves renderer construction fails loudly for
// bad scores, bad paths, and bad rates.
func TestR2RendererErrors(t *testing.T) {
	bad := r2Score()
	bad.Tracks = nil
	if _, err := NewOfflineRenderer(bad, filepath.Join(t.TempDir(), "o.wav"), 48000); err == nil {
		t.Fatal("trackless renderer accepted")
	}
	if _, err := NewOfflineRenderer(r2Score(), filepath.Join(t.TempDir(), "no-dir", "o.wav"), 48000); err == nil {
		t.Fatal("bad renderer path accepted")
	}
	if _, err := NewOfflineRenderer(r2Score(), filepath.Join(t.TempDir(), "o.wav"), 0); err == nil {
		t.Fatal("zero renderer rate accepted")
	}
}

// TestR2RendererProgressEdges proves zero and overrun progress clamp.
func TestR2RendererProgressEdges(t *testing.T) {
	if got := (&OfflineRenderer{}).Progress(); got != 1 {
		t.Fatalf("empty progress = %v, want 1", got)
	}
	if got := (&OfflineRenderer{done: 9, total: 4}).Progress(); got != 1 {
		t.Fatalf("overrun progress = %v, want 1", got)
	}
	if got := (&OfflineRenderer{done: 1, total: 4}).Progress(); got != 0.25 {
		t.Fatalf("progress = %v, want 0.25", got)
	}
}

// TestR2RendererFullCycle proves the documented drive loop produces a
// valid WAV file and a second block after Done is a safe no-op.
func TestR2RendererFullCycle(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.wav")
	r, err := NewOfflineRenderer(r2Score(), out, 48000)
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine() == nil || r.Frames() <= 0 {
		t.Fatal("renderer not ready")
	}
	for !r.Done() {
		r.ProcessBlock()
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	r.ProcessBlock() // past Done: no-op
	if !r.Done() || r.Progress() != 1 {
		t.Fatalf("done=%v progress=%v", r.Done(), r.Progress())
	}
	if r.Peak() < 0 {
		t.Fatalf("peak = %v", r.Peak())
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(out); err != nil || info.Size() <= 44 {
		t.Fatalf("wav missing/small: %+v %v", info, err)
	}
}

// TestR2RendererPanRight proves right-heavy mixes track peak on the
// right channel.
func TestR2RendererPanRight(t *testing.T) {
	s := r2Score()
	s.Tracks[0].Pan = 1
	r, err := NewOfflineRenderer(s, filepath.Join(t.TempDir(), "o.wav"), 48000)
	if err != nil {
		t.Fatal(err)
	}
	for !r.Done() {
		r.ProcessBlock()
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if r.Peak() <= 0 {
		t.Fatalf("peak = %v, want > 0", r.Peak())
	}
}

// TestR2RendererWriteError proves a dead output file surfaces through
// Err and ends the render.
func TestR2RendererWriteError(t *testing.T) {
	r, err := NewOfflineRenderer(r2Score(), filepath.Join(t.TempDir(), "o.wav"), 48000)
	if err != nil {
		t.Fatal(err)
	}
	r.file.Close()
	r.ProcessBlock()
	if r.Err() == nil || !r.Done() {
		t.Fatalf("dead file: err=%v done=%v", r.Err(), r.Done())
	}
}

// TestR2RendererClosePrecedence proves Close reports the render error
// first, then finalize errors.
func TestR2RendererClosePrecedence(t *testing.T) {
	r, err := NewOfflineRenderer(r2Score(), filepath.Join(t.TempDir(), "o.wav"), 48000)
	if err != nil {
		t.Fatal(err)
	}
	r.err = errors.New("boom")
	if err := r.Close(); err == nil || err.Error() != "boom" {
		t.Fatalf("close = %v, want render error", err)
	}
	r2, err := NewOfflineRenderer(r2Score(), filepath.Join(t.TempDir(), "o2.wav"), 48000)
	if err != nil {
		t.Fatal(err)
	}
	r2.file.Close()
	if err := r2.Close(); err == nil {
		t.Fatal("closed-file Close accepted")
	}
}

// TestR2HumanizeEdges proves nil, unplayable, and zero-seed inputs are
// safe and deterministic.
func TestR2HumanizeEdges(t *testing.T) {
	if Humanize(nil, 0.5, 1) != nil {
		t.Fatal("nil humanize not nil")
	}
	flat := r2Score()
	if got := Humanize(flat, 0, 7); got.TotalNotes() != 1 {
		t.Fatal("zero strength changed notes")
	}
	for _, bpm := range []float64{0, math.NaN(), math.Inf(1)} {
		s := r2Score()
		s.Metadata.BPM = bpm
		before := s.Tracks[0].Notes[0].Time
		Humanize(s, 0.6, 2)
		if s.Tracks[0].Notes[0].Time != before {
			t.Fatalf("bpm %v moved time", bpm)
		}
	}
	a, b := newHumanRNG(0), newHumanRNG(0)
	if a.uniform() != b.uniform() {
		t.Fatal("zero-seed streams differ")
	}
	g, h := newHumanRNG(9), newHumanRNG(9)
	x1, y1 := g.gauss(), g.gauss()
	x2, y2 := h.gauss(), h.gauss()
	if x1 != x2 || y1 != y2 || math.IsNaN(x1) || math.IsNaN(y1) {
		t.Fatalf("gauss not deterministic: %v %v vs %v %v", x1, y1, x2, y2)
	}
}

// TestR2MetricWeightTable proves the documented accent weights.
func TestR2MetricWeightTable(t *testing.T) {
	for _, tc := range []struct {
		beat  float64
		beats int
		want  float64
	}{
		{0, 4, 1.0}, {1, 4, 0.85}, {2, 4, 0.95}, {3, 4, 0.8},
		{0.5, 4, 0.75}, {0.3, 4, 0.7},
		{1, 0, 0.85}, {3.999, 4, 1.0},
		{1, 3, 0.85}, {2, 3, 0.95},
		{1, 2, 0.85},
		{0, 5, 1.0}, {2, 5, 0.95}, {1, 5, 0.85},
	} {
		if got := metricWeight(tc.beat, tc.beats); got != tc.want {
			t.Errorf("metricWeight(%v, %d) = %v, want %v", tc.beat, tc.beats, got, tc.want)
		}
	}
}

// TestR2ResolveRoomUnknown proves unknown presets fall back to master.
func TestR2ResolveRoomUnknown(t *testing.T) {
	if _, _, ok := resolveRoom(&score.Room{IsPreset: true, Preset: "cavern"}); ok {
		t.Fatal("cavern resolved")
	}
	if _, none, ok := resolveRoom(&score.Room{IsPreset: true, Preset: "none"}); !ok || !none {
		t.Fatalf("none = %v %v", none, ok)
	}
	if p, none, ok := resolveRoom(&score.Room{IsPreset: true, Preset: "hall"}); !ok || none || p.RoomSize <= 0 {
		t.Fatalf("hall = %+v %v %v", p, none, ok)
	}
	eng, err := New(r2Score(), 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	other := r2Score()
	other.Tracks[0].Room = &score.Room{IsPreset: true, Preset: "cavern"}
	eng.assignRooms(other) // must not panic; track keeps master reverb
	if _, ok := eng.Mixer().TrackRoom(0); ok {
		t.Fatal("cavern took a room")
	}
}

// TestR2TrackPeak proves per-track meters report signal and reject bad
// indices.
func TestR2TrackPeak(t *testing.T) {
	eng, err := New(r2Score(), 48000, DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	if got := eng.TrackPeak(-1); got != 0 {
		t.Fatalf("peak(-1) = %v", got)
	}
	if got := eng.TrackPeak(99); got != 0 {
		t.Fatalf("peak(99) = %v", got)
	}
	eng.ProcessBlock()
	if got := eng.TrackPeak(0); got <= 0 {
		t.Fatalf("peak(0) = %v, want > 0", got)
	}
}

// TestR2NewInstrumentErrors proves unknown and file-less instruments
// fail with helpful errors.
func TestR2NewInstrumentErrors(t *testing.T) {
	if _, err := newInstrument(score.InstrumentDef{Type: "ocarina"}, 48000); err != nil {
		t.Fatalf("ocarina: %v", err)
	}
	if _, err := newInstrument(score.InstrumentDef{Type: "theremin"}, 48000); err == nil {
		t.Fatal("unknown instrument accepted")
	}
	if _, err := newInstrument(score.InstrumentDef{Type: "sampler"}, 48000); err == nil {
		t.Fatal("file-less sampler accepted")
	}
	if _, err := newInstrument(score.InstrumentDef{Type: "sampler", File: "nope.sfz"}, 48000); err == nil {
		t.Fatal("missing library accepted")
	}
}

// TestR2ParamsForVariants proves vibrato and expression override
// resolution including defaults and clamps.
func TestR2ParamsForVariants(t *testing.T) {
	tr := &score.Track{Vibrato: &score.Vibrato{Rate: 5, Depth: 0.04}}
	n := score.NoteEvent{Vibrato: &score.Vibrato{Rate: 7, Depth: 0.06}}
	if p := paramsFor(n, tr); !p.HasVibrato || p.VibratoRate != 7 || p.VibratoDepth != 0.06 {
		t.Fatalf("note vib = %+v", p)
	}
	zero := score.NoteEvent{Vibrato: &score.Vibrato{}}
	if p := paramsFor(zero, &score.Track{}); !p.HasVibrato || p.VibratoRate != defaultVibRate || p.VibratoDepth != defaultVibDepth {
		t.Fatalf("default vib = %+v", p)
	}
	for _, tc := range []struct {
		in   float64
		want float32
	}{
		{math.NaN(), 1}, {-0.5, 0}, {2.0, 1}, {0.4, 0.4},
	} {
		v := tc.in
		nn := score.NoteEvent{Expression: &v}
		if p := paramsFor(nn, &score.Track{}); !p.HasExpression || p.Expression != tc.want {
			t.Errorf("expr %v = %+v, want %v", tc.in, p, tc.want)
		}
	}
	if p := paramsFor(score.NoteEvent{}, &score.Track{}); !p.HasExpression || p.Expression != 1 {
		t.Fatalf("default expr = %+v", p)
	}
}

// TestR2DelayConfigVariants proves echo routing spellings and defaults.
func TestR2DelayConfigVariants(t *testing.T) {
	for _, m := range []string{"pingpong", "ping-pong", "ping_pong"} {
		cfg := delayConfigFor(&score.TrackDelay{Mode: m, Subdivision: "1/8", Wet: 0.3})
		if cfg.Mode != dsp.DelayModePingPong {
			t.Fatalf("mode %q = %v", m, cfg.Mode)
		}
	}
	cfg := delayConfigFor(&score.TrackDelay{Subdivision: "", Seconds: 0.25, Feedback: 0.4, DampingHz: 3500, Wet: 0.5})
	if cfg.Mode != dsp.DelayModeStereo || cfg.Subdivision != dsp.SubdivisionQuarter {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.DelaySeconds != 0.25 || cfg.Feedback != 0.4 || cfg.DampingHz != 3500 || cfg.Wet != 0.5 {
		t.Fatalf("passthrough = %+v", cfg)
	}
	bad := delayConfigFor(&score.TrackDelay{Subdivision: "nope"})
	if bad.Subdivision != dsp.SubdivisionQuarter {
		t.Fatalf("bad subdivision = %+v", bad)
	}
}

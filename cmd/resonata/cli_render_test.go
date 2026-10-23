package main

import (
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/engine"
	"resonata/pkg/score"
)

func minimalScore() *score.Score {
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

// TestRenderUnknownMode verifies error handling when unrecognized render mode is requested.
func TestRenderUnknownMode(t *testing.T) {
	cfg := &CLIConfig{RenderMode: "realtime", BitDepth: "24", Channels: 2, SampleRate: 48000,
		OutputPath: filepath.Join(t.TempDir(), "o.wav"), ReverbPreset: "none"}
	if _, err := renderToFile(cfg, minimalScore(), nil); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

// TestRenderBadBitDepth verifies error handling when unsupported bit depth is specified.
func TestRenderBadBitDepth(t *testing.T) {
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "64", Channels: 2, SampleRate: 48000,
		OutputPath: filepath.Join(t.TempDir(), "o.wav"), ReverbPreset: "none"}
	if _, err := renderToFile(cfg, minimalScore(), nil); err == nil {
		t.Fatal("bad bit depth accepted")
	}
}

// TestRenderWritesWAV verifies rendering score to disk generates valid non-empty WAV file.
func TestRenderWritesWAV(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.wav")
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 1, SampleRate: 44100,
		OutputPath: out, ReverbPreset: "none", MasterGain: 1.0}
	stats, err := renderToFile(cfg, minimalScore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.frames <= 0 || stats.seconds <= 0 {
		t.Fatalf("bad stats %+v", stats)
	}
	info, err := os.Stat(out)
	if err != nil || info.Size() <= 44 {
		t.Fatalf("wav missing/small: %+v %v", info, err)
	}
}

// TestMasterGainClamp verifies master gain parameter clamping to valid [0, 1] range.
func TestMasterGainClamp(t *testing.T) {
	// Gains outside [0,1] clamp instead of failing; peak must stay bounded.
	for _, g := range []float64{2.0, -1.0} {
		out := filepath.Join(t.TempDir(), "o.wav")
		cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 2,
			SampleRate: 48000, OutputPath: out, ReverbPreset: "none", MasterGain: g}
		stats, err := renderToFile(cfg, minimalScore(), nil)
		if err != nil {
			t.Fatalf("gain %v: %v", g, err)
		}
		if stats.peak < 0 || stats.peak > 1.0 {
			t.Fatalf("gain %v peak %v out of range", g, stats.peak)
		}
	}
}

// TestApplyReverbBypass verifies bypassing master reverb when preset is set to none or dry.
func TestApplyReverbBypass(t *testing.T) {
	for _, name := range []string{"none", "off", "dry"} {
		eng, err := engine.New(minimalScore(), 48000, engine.DefaultBlockSize)
		if err != nil {
			t.Fatal(err)
		}
		applyReverbPreset(eng, name)
		if got := eng.Mixer().ReverbWet(); got != 0 {
			t.Fatalf("preset %q wet = %v, want 0", name, got)
		}
	}
}

// TestApplyReverbUnknownKeepsDefault verifies unrecognized reverb preset falls back to default space.
func TestApplyReverbUnknownKeepsDefault(t *testing.T) {
	eng, err := engine.New(minimalScore(), 48000, engine.DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	before := eng.Mixer().ReverbWet()
	applyReverbPreset(eng, "cavern")
	if got := eng.Mixer().ReverbWet(); got != before {
		t.Fatalf("unknown preset changed wet %v -> %v", before, got)
	}
}

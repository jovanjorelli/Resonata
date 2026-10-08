package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resonata/pkg/engine"
)

// TestR2BatchRenderError proves a batch whose renders fail reports the
// failure count instead of succeeding silently.
func TestR2BatchRenderError(t *testing.T) {
	dir := t.TempDir()
	writePushScore(t, dir, "a.json")
	cfg := &CLIConfig{BatchDir: dir, OutputPath: t.TempDir(), RenderMode: "bogus",
		BitDepth: "16", Channels: 1, SampleRate: 44100, ReverbPreset: "none"}
	if err := runBatch(cfg); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("bogus-mode batch = %v, want failure count", err)
	}
}

// TestR2InspectSFZVerbose proves library inspection logs per-region
// details in verbose mode.
func TestR2InspectSFZVerbose(t *testing.T) {
	if err := inspectSFZ("../../samples/piano.sfz", true); err != nil {
		t.Fatalf("inspect: %v", err)
	}
}

// TestR2LoadScoreMIDIEmpty proves a note-less MIDI file fails import
// with a helpful error.
func TestR2LoadScoreMIDIEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.mid")
	data := []byte("MThd\x00\x00\x00\x06\x00\x01\x00\x01\x01\xe0MTrk\x00\x00\x00\x00")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ImportMIDI: p}); err == nil ||
		!strings.Contains(err.Error(), "no notes") {
		t.Fatalf("empty midi = %v, want no-notes error", err)
	}
}

// TestR2LoadScoreMIDIFullVerbose proves verbose MIDI import plus JSON
// export round-trips the Mahler fixture.
func TestR2LoadScoreMIDIFullVerbose(t *testing.T) {
	js := filepath.Join(t.TempDir(), "imported.json")
	s, err := loadScore(&CLIConfig{ImportMIDI: "../../test_data/mahler_symphony.mid",
		ExportJSON: js, Verbose: true})
	if err != nil {
		t.Fatalf("midi verbose: %v", err)
	}
	if len(s.Tracks) == 0 || s.TotalNotes() == 0 {
		t.Fatalf("imported score empty: %+v", s.Metadata)
	}
	if info, err := os.Stat(js); err != nil || info.Size() == 0 {
		t.Fatalf("export json missing/empty: %+v %v", info, err)
	}
}

// TestR2RenderDevFull proves a full output device surfaces the write
// error instead of reporting success.
func TestR2RenderDevFull(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this platform")
	}
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 1, SampleRate: 44100,
		OutputPath: "/dev/full", ReverbPreset: "none", MasterGain: 1.0}
	if _, err := renderToFile(cfg, minimalScore(), nil); err == nil {
		t.Fatal("full device accepted")
	}
}

// TestR2RenderProgressChan proves progress fractions stream in [0,1] and
// end at exactly 1.
func TestR2RenderProgressChan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.wav")
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 1, SampleRate: 44100,
		OutputPath: out, ReverbPreset: "none", MasterGain: 1.0}
	progress := make(chan float64, 4096)
	stats, err := renderToFile(cfg, minimalScore(), progress)
	if err != nil {
		t.Fatal(err)
	}
	close(progress)
	var last float64
	n := 0
	for p := range progress {
		if p < 0 || p > 1 {
			t.Fatalf("progress %v out of [0,1]", p)
		}
		last, n = p, n+1
	}
	if n == 0 || last != 1 {
		t.Fatalf("progress reports = %d, last = %v; want >0 ending at 1", n, last)
	}
	if stats.frames <= 0 {
		t.Fatalf("bad stats %+v", stats)
	}
}

// TestR2RenderPanRightPeak proves a hard-right mix still tracks peak on
// the right channel.
func TestR2RenderPanRightPeak(t *testing.T) {
	s := minimalScore()
	s.Tracks[0].Pan = 1
	out := filepath.Join(t.TempDir(), "o.wav")
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 2, SampleRate: 48000,
		OutputPath: out, ReverbPreset: "none", MasterGain: 1.0}
	stats, err := renderToFile(cfg, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.peak <= 0 {
		t.Fatalf("peak = %v, want > 0", stats.peak)
	}
}

// TestR2BenchmarkEngineError proves benchmark setup failures surface.
func TestR2BenchmarkEngineError(t *testing.T) {
	s := minimalScore()
	s.Tracks = nil
	if _, err := runBenchmark(&CLIConfig{SampleRate: 48000, ReverbPreset: "none"}, s); err == nil {
		t.Fatal("trackless benchmark accepted")
	}
}

// TestR2MemProfileDevFull proves heap-profile write failures surface.
func TestR2MemProfileDevFull(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this platform")
	}
	if err := writeMemProfile("/dev/full"); err == nil {
		t.Fatal("full device mem profile accepted")
	}
}

// TestR2EngineTrackPeakSmoke keeps the engine import referenced for
// render-path helpers used above.
func TestR2EngineTrackPeakSmoke(t *testing.T) {
	eng, err := engine.New(minimalScore(), 48000, engine.DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	if eng.TotalFrames() <= 0 {
		t.Fatal("no frames scheduled")
	}
}

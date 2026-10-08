package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/engine"
)

// writePushScore writes minimalScore as JSON and returns its path.
func writePushScore(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	data, err := json.Marshal(minimalScore())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPushRenderEmptyModeDefaultsOffline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o.wav")
	cfg := &CLIConfig{RenderMode: "", BitDepth: "24", Channels: 2, SampleRate: 48000,
		OutputPath: out, ReverbPreset: "none", MasterGain: 1.0}
	stats, err := renderToFile(cfg, minimalScore(), nil)
	if err != nil {
		t.Fatalf("empty mode: %v", err)
	}
	if stats.path != out || stats.frames <= 0 {
		t.Fatalf("bad stats %+v", stats)
	}
}

func TestPushRenderEmptyOutputDefaults(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	}()
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "16", Channels: 1, SampleRate: 44100,
		OutputPath: "   ", ReverbPreset: "none", MasterGain: 1.0, Verbose: true}
	stats, err := renderToFile(cfg, minimalScore(), nil)
	if err != nil {
		t.Fatalf("empty output: %v", err)
	}
	if stats.path != "out.wav" {
		t.Fatalf("path = %q, want out.wav", stats.path)
	}
	if info, err := os.Stat(filepath.Join(dir, "out.wav")); err != nil || info.Size() <= 44 {
		t.Fatalf("default wav missing/small: %+v %v", info, err)
	}
}

func TestPushRenderCreateError(t *testing.T) {
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "24", Channels: 2, SampleRate: 48000,
		OutputPath: filepath.Join(t.TempDir(), "no-such-dir", "o.wav"), ReverbPreset: "none"}
	if _, err := renderToFile(cfg, minimalScore(), nil); err == nil {
		t.Fatal("bad output dir accepted")
	}
}

func TestPushRenderEngineError(t *testing.T) {
	empty := minimalScore()
	empty.Tracks = nil
	cfg := &CLIConfig{RenderMode: "offline", BitDepth: "24", Channels: 2, SampleRate: 48000,
		OutputPath: filepath.Join(t.TempDir(), "o.wav"), ReverbPreset: "none"}
	if _, err := renderToFile(cfg, empty, nil); err == nil {
		t.Fatal("trackless score accepted by renderToFile")
	}
}

func TestPushRenderMonoNoProgress(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mono.wav")
	cfg := &CLIConfig{RenderMode: "stream", BitDepth: "16", Channels: 1, SampleRate: 48000,
		OutputPath: out, ReverbPreset: "none", MasterGain: 0.5, Verbose: true}
	stats, err := renderToFile(cfg, minimalScore(), nil)
	if err != nil {
		t.Fatalf("stream mono: %v", err)
	}
	if stats.peak < 0 || stats.peak > 1 {
		t.Fatalf("peak %v out of range", stats.peak)
	}
}

func TestPushLoadScoreVerbose(t *testing.T) {
	p := writePushScore(t, t.TempDir(), "s.json")
	s, err := loadScore(&CLIConfig{ScorePath: p, Verbose: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 1 {
		t.Fatalf("tracks = %d, want 1", len(s.Tracks))
	}
}

func TestPushLoadScoreExportJSONBadPath(t *testing.T) {
	cfg := &CLIConfig{ImportMIDI: "../../test_data/mahler_symphony.mid",
		ExportJSON: filepath.Join(t.TempDir(), "no-such-dir", "out.json")}
	if _, err := loadScore(cfg); err == nil {
		t.Fatal("bad export-json path accepted")
	}
}

func TestPushLoadScoreMIDIValidateError(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "empty.mid")
	if err := os.WriteFile(bad, []byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ImportMIDI: bad}); err == nil {
		t.Fatal("empty midi accepted")
	}
}

func TestPushInspectSFZMissing(t *testing.T) {
	if err := inspectSFZ(filepath.Join(t.TempDir(), "missing.sfz"), false); err == nil {
		t.Fatal("missing sfz accepted")
	}
}

func TestPushExportMIDIWriteError(t *testing.T) {
	if err := exportMIDIFile(minimalScore(), filepath.Join(t.TempDir(), "no-such-dir", "o.mid")); err == nil {
		t.Fatal("bad midi output accepted")
	}
}

func TestPushImportMIDIBatchMissing(t *testing.T) {
	if _, err := importMIDIScoreForBatch(filepath.Join(t.TempDir(), "missing.mid")); err == nil {
		t.Fatal("missing batch midi accepted")
	}
}

func TestPushApplyReverbEmptyKeepsDefault(t *testing.T) {
	eng, err := engine.New(minimalScore(), 48000, engine.DefaultBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	before := eng.Mixer().ReverbWet()
	applyReverbPreset(eng, "   ")
	if got := eng.Mixer().ReverbWet(); got != before {
		t.Fatalf("empty preset changed wet %v -> %v", before, got)
	}
}

func TestPushBatchBadDir(t *testing.T) {
	if err := runBatch(&CLIConfig{BatchDir: filepath.Join(t.TempDir(), "no-such-dir")}); err == nil {
		t.Fatal("bad batch dir accepted")
	}
}

func TestPushBatchSubdirSkippedVerbose(t *testing.T) {
	dir := t.TempDir()
	writePushScore(t, dir, "a.json")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	cfg := &CLIConfig{BatchDir: dir, OutputPath: outDir, Verbose: true, ReverbPreset: "none",
		BitDepth: "16", Channels: 1, SampleRate: 44100}
	if err := runBatch(cfg); err != nil {
		t.Fatalf("verbose batch with subdir: %v", err)
	}
	if info, err := os.Stat(filepath.Join(outDir, "a.wav")); err != nil || info.Size() <= 44 {
		t.Fatalf("batched wav missing/small: %+v %v", info, err)
	}
}

func TestPushBatchMIDIImportError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.mid"), []byte("not a midi file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runBatch(&CLIConfig{BatchDir: dir}); err == nil {
		t.Fatal("corrupt batch midi accepted")
	}
}

func TestPushBatchHumanize(t *testing.T) {
	dir := t.TempDir()
	writePushScore(t, dir, "h.json")
	cfg := &CLIConfig{BatchDir: dir, OutputPath: t.TempDir(), Humanize: 0.5,
		ReverbPreset: "none", BitDepth: "16", Channels: 2, SampleRate: 48000}
	if err := runBatch(cfg); err != nil {
		t.Fatalf("humanized batch: %v", err)
	}
}

func TestPushProfileErrors(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no-such-dir", "cpu.prof")
	if err := startCPUProfile(bad); err == nil {
		t.Fatal("bad cpu profile path accepted")
	}
	dir := t.TempDir()
	cpu := filepath.Join(dir, "cpu.prof")
	if err := startCPUProfile(cpu); err != nil {
		t.Fatal(err)
	}
	defer stopCPUProfile()
	if err := startCPUProfile(filepath.Join(dir, "cpu2.prof")); err == nil {
		t.Fatal("double cpu profile accepted")
	}
	if err := writeMemProfile(filepath.Join(t.TempDir(), "no-such-dir", "mem.prof")); err == nil {
		t.Fatal("bad mem profile path accepted")
	}
}

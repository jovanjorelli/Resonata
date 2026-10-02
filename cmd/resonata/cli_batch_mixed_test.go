package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchMixedValidInvalid(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeMinimalScoreFile(t, in, "good.json")
	if err := os.WriteFile(filepath.Join(in, "corrupt.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in, "bad.json"), []byte(
		`{"metadata":{"title":"B","bpm":0,"time_signature":"4/4"},"tracks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &CLIConfig{BatchDir: in, OutputPath: out, ReverbPreset: "none",
		BitDepth: "16", Channels: 1, SampleRate: 44100, RenderMode: "offline", MasterGain: 1.0}
	err := runBatch(cfg)
	if err == nil {
		t.Fatal("mixed batch accepted without error")
	}
	info, statErr := os.Stat(filepath.Join(out, "good.wav"))
	if statErr != nil || info.Size() <= 44 {
		t.Fatalf("valid input not rendered: %+v %v", info, statErr)
	}
}

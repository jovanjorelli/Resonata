package main

import (
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/midi"
	"resonata/pkg/score"
)

func writeScore(t *testing.T, dir, name string) string {
	t.Helper()
	doc := `{"metadata":{"title":"B","bpm":120,"time_signature":"4/4"},` +
		`"tracks":[{"id":"s","name":"S","instrument":{"type":"ocarina"},` +
		`"pan":0,"volume":0.8,"notes":[{"time":0,"duration":0.25,"pitch":69,"velocity":0.8}]}]}`
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBatchIsolation(t *testing.T) {
	in := t.TempDir()
	out := t.TempDir()
	writeScore(t, in, "a.json")
	writeScore(t, in, "b.json")
	cfg := &CLIConfig{BatchDir: in, OutputPath: out, ReverbPreset: "none",
		BitDepth: "16", Channels: 1, SampleRate: 44100, RenderMode: "offline", MasterGain: 1.0}
	if err := runBatch(cfg); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.wav", "b.wav"} {
		info, err := os.Stat(filepath.Join(out, n))
		if err != nil || info.Size() <= 44 {
			t.Fatalf("%s missing/small: %+v %v", n, info, err)
		}
	}
}

func TestBatchEmptyDirFails(t *testing.T) {
	cfg := &CLIConfig{BatchDir: t.TempDir(), OutputPath: t.TempDir()}
	if err := runBatch(cfg); err == nil {
		t.Fatal("empty batch dir accepted")
	}
}

func TestImportMIDIScoreForBatch(t *testing.T) {
	s, err := importMIDIScoreForBatch("../../test_data/mahler_symphony.mid")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 5 {
		t.Fatalf("tracks = %d, want 5", len(s.Tracks))
	}
	if err := score.Validate(s); err != nil {
		t.Fatalf("imported invalid: %v", err)
	}
	if _, err := importMIDIScoreForBatch(filepath.Join(t.TempDir(), "no.mid")); err == nil {
		t.Fatal("missing midi accepted")
	}
}

func TestExportMIDIFileRoundTrip(t *testing.T) {
	out := filepath.Join(t.TempDir(), "e.mid")
	if err := exportMIDIFile(minimalScore(), out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		t.Fatalf("export missing: %v", err)
	}
	f, err := midi.ReadFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if f.Header.Format != 1 {
		t.Fatalf("format = %d", f.Header.Format)
	}
	back, err := midi.ToScore(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Tracks) != 1 || len(back.Tracks[0].Notes) != 1 || back.Tracks[0].Notes[0].Pitch != 69 {
		t.Fatalf("round trip mismatch: %+v", back.Tracks)
	}
}

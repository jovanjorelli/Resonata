package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeMinimalScoreFile(t *testing.T, dir, name string) string {
	t.Helper()
	doc := `{"metadata":{"title":"C","bpm":120,"time_signature":"4/4"},` +
		`"tracks":[{"id":"s","name":"S","instrument":{"type":"ocarina"},` +
		`"pan":0,"volume":0.8,"notes":[{"time":0,"duration":0.25,"pitch":69,"velocity":0.8}]}]}`
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadScoreValidFile(t *testing.T) {
	p := writeMinimalScoreFile(t, t.TempDir(), "s.json")
	s, err := loadScore(&CLIConfig{ScorePath: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 1 || s.Tracks[0].Notes[0].Pitch != 69 {
		t.Fatalf("unexpected score %+v", s)
	}
}

func TestLoadScoreMissingPath(t *testing.T) {
	if _, err := loadScore(&CLIConfig{}); err == nil {
		t.Fatal("empty input accepted")
	}
}

func TestLoadScoreMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "no.json")
	if _, err := loadScore(&CLIConfig{ScorePath: p}); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestLoadScoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ScorePath: p}); err == nil {
		t.Fatal("corrupt file accepted")
	}
}

func TestLoadScoreValidationError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	doc := `{"metadata":{"title":"C","bpm":120,"time_signature":"4/4"},` +
		`"tracks":[{"id":"s","name":"S","instrument":{"type":"ocarina"},` +
		`"pan":0,"volume":2.5,"notes":[{"time":0,"duration":0.25,"pitch":69,"velocity":0.8}]}]}`
	if err := os.WriteFile(p, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ScorePath: p}); err == nil {
		t.Fatal("invalid score accepted")
	}
}

func TestLoadScoreMIDIImport(t *testing.T) {
	s, err := loadScore(&CLIConfig{ImportMIDI: "../../test_data/mahler_symphony.mid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 5 {
		t.Fatalf("tracks = %d, want 5", len(s.Tracks))
	}
}

func TestLoadScoreMIDIExportJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imp.json")
	s, err := loadScore(&CLIConfig{
		ImportMIDI: "../../test_data/mahler_symphony.mid",
		ExportJSON: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil || len(data) == 0 {
		t.Fatalf("export missing: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("export not JSON: %v", err)
	}
	if len(s.Tracks) != 5 {
		t.Fatalf("tracks = %d", len(s.Tracks))
	}
}

func TestLoadScoreMIDIMissing(t *testing.T) {
	if _, err := loadScore(&CLIConfig{ImportMIDI: filepath.Join(t.TempDir(), "no.mid")}); err == nil {
		t.Fatal("missing MIDI accepted")
	}
}

func TestLoadScoreMIDICorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.mid")
	if err := os.WriteFile(p, []byte("not a midi file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ImportMIDI: p}); err == nil {
		t.Fatal("corrupt MIDI accepted")
	}
}

func TestInspectSFZPaths(t *testing.T) {
	if err := inspectSFZ("../../samples/piano.sfz", false); err != nil {
		t.Fatalf("valid SFZ rejected: %v", err)
	}
	if err := inspectSFZ(filepath.Join(t.TempDir(), "no.sfz"), false); err == nil {
		t.Fatal("missing SFZ accepted")
	}
}

func TestRunBenchmarkPaths(t *testing.T) {
	avg, err := runBenchmark(&CLIConfig{SampleRate: 48000, ReverbPreset: "none"}, minimalScore())
	if err != nil || avg <= 0 {
		t.Fatalf("benchmark = %v, %v", avg, err)
	}
	if _, err := runBenchmark(&CLIConfig{SampleRate: 22050}, minimalScore()); err == nil {
		t.Fatal("bad sample rate accepted")
	}
}

func TestProfileHelpers(t *testing.T) {
	dir := t.TempDir()
	cpu := filepath.Join(dir, "cpu.prof")
	if err := startCPUProfile(cpu); err != nil {
		t.Fatal(err)
	}
	stopCPUProfile()
	if info, err := os.Stat(cpu); err != nil {
		t.Fatalf("cpu profile missing: %v", err)
	} else if info.Size() == 0 {
		t.Fatal("cpu profile empty")
	}
	mem := filepath.Join(dir, "mem.prof")
	if err := writeMemProfile(mem); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(mem); err != nil || info.Size() == 0 {
		t.Fatalf("mem profile missing/empty: %+v %v", info, err)
	}
}

func TestVersionPinned(t *testing.T) {
	if Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", Version)
	}
}

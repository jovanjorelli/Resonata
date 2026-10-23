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

// TestLoadScoreValidFile verifies loading and decoding a valid score JSON file.
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

// TestLoadScoreMissingPath verifies error reporting when score path argument is omitted.
func TestLoadScoreMissingPath(t *testing.T) {
	if _, err := loadScore(&CLIConfig{}); err == nil {
		t.Fatal("empty input accepted")
	}
}

// TestLoadScoreMissingFile verifies error reporting when target score file does not exist.
func TestLoadScoreMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "no.json")
	if _, err := loadScore(&CLIConfig{ScorePath: p}); err == nil {
		t.Fatal("missing file accepted")
	}
}

// TestLoadScoreCorruptFile verifies error reporting when score JSON file contains corrupt syntax.
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

// TestLoadScoreValidationError verifies error reporting when score violates validation schema.
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

// TestLoadScoreMIDIImport verifies importing score structure from standard MIDI file input.
func TestLoadScoreMIDIImport(t *testing.T) {
	s, err := loadScore(&CLIConfig{ImportMIDI: "../../test_data/mahler_symphony.mid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Tracks) != 5 {
		t.Fatalf("tracks = %d, want 5", len(s.Tracks))
	}
}

// TestLoadScoreMIDIExportJSON verifies exporting imported MIDI score to intermediate JSON file.
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

// TestLoadScoreMIDIMissing verifies error reporting when imported MIDI file is not found.
func TestLoadScoreMIDIMissing(t *testing.T) {
	if _, err := loadScore(&CLIConfig{ImportMIDI: filepath.Join(t.TempDir(), "no.mid")}); err == nil {
		t.Fatal("missing MIDI accepted")
	}
}

// TestLoadScoreMIDICorrupt verifies error reporting when imported MIDI file is malformed.
func TestLoadScoreMIDICorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.mid")
	if err := os.WriteFile(p, []byte("not a midi file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScore(&CLIConfig{ImportMIDI: p}); err == nil {
		t.Fatal("corrupt MIDI accepted")
	}
}

// TestInspectSFZPaths verifies inspecting and pre-validating SFZ instrument sample paths.
func TestInspectSFZPaths(t *testing.T) {
	if err := inspectSFZ("../../samples/piano.sfz", false); err != nil {
		t.Fatalf("valid SFZ rejected: %v", err)
	}
	if err := inspectSFZ(filepath.Join(t.TempDir(), "no.sfz"), false); err == nil {
		t.Fatal("missing SFZ accepted")
	}
}

// TestRunBenchmarkPaths verifies running multi-iteration performance benchmark mode.
func TestRunBenchmarkPaths(t *testing.T) {
	avg, err := runBenchmark(&CLIConfig{SampleRate: 48000, ReverbPreset: "none"}, minimalScore())
	if err != nil || avg <= 0 {
		t.Fatalf("benchmark = %v, %v", avg, err)
	}
	if _, err := runBenchmark(&CLIConfig{SampleRate: 22050}, minimalScore()); err == nil {
		t.Fatal("bad sample rate accepted")
	}
}

// TestProfileHelpers verifies CPU and memory profiling activation and file output.
func TestProfileHelpers(t *testing.T) {
	dir := t.TempDir()
	cpu := filepath.Join(dir, "cpu.prof")
	if err := startCPUProfile(cpu); err != nil {
		t.Fatal(err)
	}
	if err := startCPUProfile(filepath.Join(dir, "cpu2.prof")); err == nil {
		t.Fatal("expected error on duplicate startCPUProfile")
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

// TestVersionPinned verifies version string constant matches expected major release.
func TestVersionPinned(t *testing.T) {
	if Version != "3" {
		t.Fatalf("Version = %q, want 3", Version)
	}
}

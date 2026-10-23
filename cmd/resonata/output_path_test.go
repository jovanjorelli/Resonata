package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOutputPathForBatchNextToInput verifies batch output WAV path derives from input score basename.
func TestOutputPathForBatchNextToInput(t *testing.T) {
	got := outputPathForBatch(filepath.Join("scores", "piece.json"), "")
	want := filepath.Join("scores", "piece.wav")
	if got != want {
		t.Fatalf("outputPathForBatch = %q, want %q", got, want)
	}
}

// TestOutputPathForBatchMIDIBase verifies batch output WAV path derives from input MIDI basename.
func TestOutputPathForBatchMIDIBase(t *testing.T) {
	got := outputPathForBatch(filepath.Join("scores", "song.mid"), "")
	want := filepath.Join("scores", "song.wav")
	if got != want {
		t.Fatalf("outputPathForBatch midi = %q, want %q", got, want)
	}
}

// TestOutputPathForBatchExistingDir verifies output path resolution when output specifies existing directory.
func TestOutputPathForBatchExistingDir(t *testing.T) {
	dir := t.TempDir()
	got := outputPathForBatch(filepath.Join("scores", "piece.json"), dir)
	want := filepath.Join(dir, "piece.wav")
	if got != want {
		t.Fatalf("outputPathForBatch dir = %q, want %q", got, want)
	}
}

// TestOutputPathForBatchFilePath verifies output path resolution when explicit output file is given.
func TestOutputPathForBatchFilePath(t *testing.T) {
	// outDir naming a non-directory is used verbatim as the file path.
	got := outputPathForBatch(filepath.Join("scores", "a.json"),
		filepath.Join("out", "custom.wav"))
	want := filepath.Join("out", "custom.wav")
	if got != want {
		t.Fatalf("outputPathForBatch file = %q, want %q", got, want)
	}
	_ = os.MkdirTemp
}

// TestAbs32 verifies absolute value function for 32-bit floating point numbers.
func TestAbs32(t *testing.T) {
	if got := abs32(-1.5); got != 1.5 {
		t.Fatalf("abs32(-1.5) = %v", got)
	}
	if got := abs32(2.25); got != 2.25 {
		t.Fatalf("abs32(2.25) = %v", got)
	}
	if got := abs32(0); got != 0 {
		t.Fatalf("abs32(0) = %v", got)
	}
}

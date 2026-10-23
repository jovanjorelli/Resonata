package score

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScoreSizeLimit verifies enforcing the strict 64 MiB score file size limit.
func TestScoreSizeLimit(t *testing.T) {
	hugeSlice := make([]byte, maxScoreFileSize+1)
	if _, err := ParseJSON(hugeSlice); err == nil || !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("ParseJSON oversized data: want size error, got %v", err)
	}

	sparsePath := filepath.Join(t.TempDir(), "oversized.json")
	f, err := os.Create(sparsePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxScoreFileSize + 10); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	if _, err := ParseFile(sparsePath); err == nil || !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("ParseFile oversized file: want size error, got %v", err)
	}
}

// TestParseFileAutoValidation verifies automatic score validation during file parsing.
func TestParseFileAutoValidation(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "invalid.json")
	badContent := `{"metadata": {"title": "T", "bpm": -50, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 69, "velocity": 0.8}]}]}`
	if err := os.WriteFile(badPath, []byte(badContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := ParseFile(badPath); err == nil || !strings.Contains(err.Error(), "bpm") {
		t.Fatalf("ParseFile with invalid bpm: want validation error, got %v", err)
	}

	goodPath := filepath.Join(dir, "valid.json")
	goodContent := `{"metadata": {"title": "Valid Score", "bpm": 120, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 69, "velocity": 0.8}]}]}`
	if err := os.WriteFile(goodPath, []byte(goodContent), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := ParseFile(goodPath)
	if err != nil {
		t.Fatalf("ParseFile valid score failed: %v", err)
	}
	if s.Metadata.Title != "Valid Score" || len(s.Tracks) != 1 {
		t.Fatalf("unexpected parsed score: %+v", s)
	}
}

// TestParseJSONAutoValidation verifies automatic score validation during JSON decoding.
func TestParseJSONAutoValidation(t *testing.T) {
	bad := `{"metadata": {"title": "T", "bpm": -10, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`
	if _, err := ParseJSON([]byte(bad)); err == nil || !strings.Contains(err.Error(), "bpm") {
		t.Fatalf("ParseJSON with invalid bpm: want validation error, got %v", err)
	}

	good := `{"metadata": {"title": "T", "bpm": 120, "time_signature": "4/4"}, "tracks": [{"id": "s", "instrument": {"type": "ocarina"}, "volume": 0.8, "notes": [{"time": 0, "duration": 0.5, "pitch": 60, "velocity": 0.8}]}]}`
	s, err := ParseJSON([]byte(good))
	if err != nil {
		t.Fatalf("ParseJSON valid score failed: %v", err)
	}
	if s.Metadata.Title != "T" {
		t.Fatalf("unexpected title: %s", s.Metadata.Title)
	}
}

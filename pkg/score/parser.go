package score

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxScoreFileSize = 64 * 1024 * 1024

// ParseJSON parses and validates score JSON data up to the 64 MiB limit.
func ParseJSON(data []byte) (*Score, error) {
	return decodeScore(data)
}

// ParseFile reads and validates a score JSON file up to the 64 MiB limit.
func ParseFile(path string) (*Score, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		return nil, fmt.Errorf("unsupported score format: %s", ext)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxScoreFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxScoreFileSize {
		return nil, errors.New("score: file exceeds maximum allowed size (64 MiB)")
	}
	return ParseJSON(data)
}

// ConvertToSeconds converts quarter-note musical time noteTime to real time in seconds at bpm.
func ConvertToSeconds(noteTime float64, bpm float64) float64 {
	if bpm <= 0 {
		return 0
	}
	return noteTime * 60 / bpm
}

func decodeScore(data []byte) (*Score, error) {
	if len(data) > maxScoreFileSize {
		return nil, errors.New("score: data exceeds maximum allowed size (64 MiB)")
	}
	var s Score
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("score: %w", err)
	}
	s.ApplyTransposition()
	s.ApplyTimingOffsets()
	s.ApplySwing()
	s.ApplyAccents()
	if err := Validate(&s); err != nil {
		return nil, fmt.Errorf("score validation: %w", err)
	}
	return &s, nil
}

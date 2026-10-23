// Batch rendering for the headless engine renders every score file in a directory
// with one shared configuration. Each input keeps its own engine instance so
// tracks, instruments, and reverb tails never leak between scores.

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"resonata/pkg/engine"
	"resonata/pkg/score"
)

func runBatch(cfg *CLIConfig) error {
	dir := strings.TrimSpace(cfg.BatchDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("batch dir: %w", err)
	}
	inputs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		switch ext {
		case ".json":
			inputs = append(inputs, filepath.Join(dir, e.Name()))
		case ".mid", ".midi":
			inputs = append(inputs, filepath.Join(dir, e.Name()))
		}
	}
	if len(inputs) == 0 {
		return fmt.Errorf("batch dir %s: no score or MIDI files found", dir)
	}
	if cfg.Verbose {
		log.Printf("batch: %d files in %s", len(inputs), dir)
	}
	failures := 0
	start := time.Now()
	for _, in := range inputs {
		if err := renderOneBatched(cfg, in); err != nil {
			log.Printf("batch %s: %v", in, err)
			failures++
			continue
		}
	}
	elapsed := time.Since(start)
	fmt.Printf("batch: %d ok, %d failed, %d total in %v\n",
		len(inputs)-failures, failures, len(inputs), elapsed.Round(time.Millisecond))
	if failures > 0 {
		return fmt.Errorf("batch: %d of %d files failed", failures, len(inputs))
	}
	return nil
}

func renderOneBatched(cfg *CLIConfig, inPath string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during render: %v\nstack:\n%s", r, debug.Stack())
		}
	}()
	ext := strings.ToLower(filepath.Ext(inPath))
	var s *score.Score
	if ext == ".mid" || ext == ".midi" {
		s, err = importMIDIScoreForBatch(inPath)
		if err != nil {
			return fmt.Errorf("midi import: %w", err)
		}
	} else {
		s, err = score.ParseFile(inPath)
		if err != nil {
			return fmt.Errorf("score parse: %w", err)
		}
	}
	if err := score.Validate(s); err != nil {
		return fmt.Errorf("invalid score: %w", err)
	}
	if cfg.Humanize > 0 {
		s = engine.Humanize(s, float32(cfg.Humanize), 1)
	}
	out := outputPathForBatch(inPath, cfg.OutputPath)
	single := *cfg
	single.ScorePath = inPath
	single.OutputPath = out
	single.BatchDir = ""
	stats, err := renderToFile(&single, s, nil)
	if err != nil {
		return err
	}
	fmt.Printf("batch %s -> %s (%.2f s, peak %.3f, %v)\n",
		inPath, stats.path, stats.seconds, stats.peak,
		stats.elapsed.Round(time.Millisecond))
	return nil
}

package main

import (
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
)

var (
	cpuMu          sync.Mutex
	cpuProfileFile *os.File
)

func startCPUProfile(path string) error {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if cpuProfileFile != nil {
		return fmt.Errorf("cpu profile already active")
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create cpu profile: %w", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return fmt.Errorf("start cpu profile: %w", err)
	}
	cpuProfileFile = f
	return nil
}

func stopCPUProfile() {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	pprof.StopCPUProfile()
	if cpuProfileFile != nil {
		cpuProfileFile.Close()
		cpuProfileFile = nil
	}
}

// writeMemProfile writes a heap profile snapshot to path.
func writeMemProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create mem profile: %w", err)
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		return fmt.Errorf("write mem profile: %w", err)
	}
	return nil
}

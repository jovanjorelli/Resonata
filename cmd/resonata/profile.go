// Profiling helpers for the headless renderer.
//
// CPU and memory profiles use the standard library runtime/pprof so no
// extra dependencies are required. Profiles are written to the paths
// given by --profile-cpu and --profile-mem.
package main

import (
	"fmt"
	"os"
	"runtime/pprof"
)

// cpuProfileFile holds the active CPU profile output.
var cpuProfileFile *os.File

// startCPUProfile begins CPU profiling, writing to path.
func startCPUProfile(path string) error {
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

// stopCPUProfile stops CPU profiling and closes the output file.
func stopCPUProfile() {
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

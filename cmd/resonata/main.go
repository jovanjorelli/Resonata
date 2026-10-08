// Command resonata is the headless Resonata rendering engine.
//
// It renders score files (JSON or YAML) to WAV audio without any GUI
// dependencies. All audio processing uses pre-allocated buffers so the
// render loop performs no allocations.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"resonata/pkg/encoder"
	"resonata/pkg/engine"
	"resonata/pkg/instruments/sampler"
	"resonata/pkg/midi"
	"resonata/pkg/score"
)

// Version is the Resonata release version, pinned at 2.
const Version = "2"

// CLIConfig holds all command line options for the headless renderer.
type CLIConfig struct {
	// Core operations
	ScorePath  string // -score, --score: input JSON/YAML score
	OutputPath string // -o, --output: output WAV path

	// Rendering modes
	RenderMode string // --mode=offline|stream (default: offline)
	SampleRate int    // --sample-rate=44100|48000|96000
	BitDepth   string // --bit-depth=16|24|32|32f (32f is 32-bit float)
	Channels   int    // --channels=1|2

	// Audio processing
	Humanize      float64 // --humanize=0.0..1.0
	ReverbPreset  string  // --reverb=cathedral|hall|chapel|room|plate|none
	ReverbDamping float64 // --reverb-damping=0.0..1.0, -1 keeps preset
	NoiseGate     float64 // --noise-gate dBFS, 0 disables, negative sets threshold
	MasterGain    float64 // --master-gain=0.0..1.0
	Saturation    string  // --saturation=tape|clean (default: tape)

	// MIDI interoperability
	ImportMIDI string // --import-midi=in.mid
	ExportMIDI string // --export-midi=out.mid
	ExportJSON string // --export-json=out.json (used with --import-midi)

	// Diagnostics
	ProfileCPU string // --profile-cpu=cpu.prof
	ProfileMem string // --profile-mem=mem.prof
	Verbose    bool   // -v, --verbose
	Benchmark  bool   // --benchmark (renders 10x and reports avg time)
	Limiter    bool   // --limiter (mastering limiter after soft clipper)
	Parallel   bool   // --parallel (voice tracks concurrently, sum sequentially)
	MonoBass   string // --mono-bass=off|Hz (default: off)

	// Batch processing
	BatchDir string // --batch-dir=./scores (render all score files)

	// Sample library inspection
	LoadSFZ string // --load-sfz=path.sfz (validate an SFZ library, then render)

	// Release information
	ShowVersion bool // --version: print Resonata v2 and exit
}

// parseFlags reads command line flags into a CLIConfig.
func parseFlags() *CLIConfig {
	cfg := &CLIConfig{}
	// --score string, default "": input score file (JSON or YAML);
	// empty falls back to --import-midi, else the run errors.
	flag.StringVar(&cfg.ScorePath, "score", "", "input score file (JSON or YAML)")
	// -s: shorthand alias writing the same ScorePath field.
	flag.StringVar(&cfg.ScorePath, "s", "", "input score file, shorthand")
	// --output string, default "" (rendered as out.wav): output WAV path.
	flag.StringVar(&cfg.OutputPath, "output", "", "output WAV path")
	// -o: shorthand alias writing the same OutputPath field.
	flag.StringVar(&cfg.OutputPath, "o", "", "output WAV path, shorthand")
	// --mode string, default "offline": only offline and stream are
	// accepted; both render identically, anything else errors.
	flag.StringVar(&cfg.RenderMode, "mode", "offline", "render mode: offline|stream")
	// --sample-rate int, default 48000: only 44100, 48000, 96000 render;
	// other values are rejected by the encoder.
	flag.IntVar(&cfg.SampleRate, "sample-rate", engine.DefaultSampleRate, "sample rate in Hz: 44100, 48000, or 96000")
	// --bit-depth string, default "24": 16/24/32 integer PCM or 32f float;
	// empty also selects 24-bit PCM; unknown names error.
	flag.StringVar(&cfg.BitDepth, "bit-depth", "24", "WAV bit depth: 16, 24, 32 (PCM) or 32f (32-bit float)")
	// --channels int, default 2: 1 downmixes stereo to mono, 2 keeps
	// stereo; other counts error.
	flag.IntVar(&cfg.Channels, "channels", 2, "output channels: 1 (mono) or 2 (stereo)")
	// --humanize float 0.0-1.0, default 0: deterministic timing/velocity
	// variation (fixed seed 1); 0 disables it.
	flag.Float64Var(&cfg.Humanize, "humanize", 0, "humanization strength 0.0..1.0")
	// --reverb preset name, default "hall": selects the master reverb
	// space; none/off/dry bypass it, unknown names keep the default.
	flag.StringVar(&cfg.ReverbPreset, "reverb", "hall", "reverb preset: cathedral|hall|chapel|room|plate|none")
	// --reverb-damping float 0.0-1.0, default -1 (keep preset damping):
	// 0.0 disables the feedback lowpass for a bright tail, 1.0 applies
	// the maximum cutoff; anything else errors.
	flag.Float64Var(&cfg.ReverbDamping, "reverb-damping", -1, "reverb HF damping 0.0-1.0 (default -1: preset)")
	// --noise-gate float dBFS, default 0 (disabled): negative values mute
	// per-voice sampler output below the threshold (e.g. -50); positive
	// values are rejected.
	flag.Float64Var(&cfg.NoiseGate, "noise-gate", 0, "voice noise gate threshold in dBFS (default 0: off)")
	// --master-gain float 0.0-1.0, default 1.0: output multiplier applied
	// before encoding; out-of-range values clamp into range.
	flag.Float64Var(&cfg.MasterGain, "master-gain", 1.0, "master gain 0.0..1.0")
	// --saturation string, default "tape": tape keeps tanh saturation,
	// clean passes signal linearly with a hard clamp at ±1; anything
	// else errors.
	flag.StringVar(&cfg.Saturation, "saturation", "tape", "saturation: tape|clean")
	// --import-midi path, default "": renders a Standard MIDI File
	// (format 0/1) instead of --score when non-empty.
	flag.StringVar(&cfg.ImportMIDI, "import-midi", "", "Standard MIDI File to import and render")
	// --export-midi path, default "": writes the loaded score as a
	// format-1 SMF after rendering.
	flag.StringVar(&cfg.ExportMIDI, "export-midi", "", "export the loaded score to an SMF (.mid) file")
	// --export-json path, default "": with --import-midi, also writes the
	// imported score as JSON.
	flag.StringVar(&cfg.ExportJSON, "export-json", "", "with --import-midi: write imported score as JSON")
	// --profile-cpu path, default "": writes a pprof CPU profile covering
	// the render.
	flag.StringVar(&cfg.ProfileCPU, "profile-cpu", "", "write CPU profile to file")
	// --profile-mem path, default "": writes a heap profile after render.
	flag.StringVar(&cfg.ProfileMem, "profile-mem", "", "write memory profile to file")
	// --verbose bool, default false: human-readable progress and paths.
	flag.BoolVar(&cfg.Verbose, "verbose", false, "verbose output")
	// -v: shorthand alias for --verbose.
	flag.BoolVar(&cfg.Verbose, "v", false, "verbose output, shorthand")
	// --benchmark bool, default false: renders 10x in memory, reports the
	// average, then still writes the WAV.
	flag.BoolVar(&cfg.Benchmark, "benchmark", false, "render 10x in memory and report average time")
	// --limiter bool, default false: routes the master bus through the
	// lookahead limiter (-0.1 dBFS ceiling) after the soft clipper.
	flag.BoolVar(&cfg.Limiter, "limiter", false, "mastering limiter after soft clipper")
	// --parallel bool, default false: voices each track in its own
	// goroutine from a shared pool, then sums sequentially in track
	// order, so output stays byte-identical to sequential rendering.
	flag.BoolVar(&cfg.Parallel, "parallel", false, "parallel per-track voicing, sequential sum")
	// --mono-bass off|Hz, default "off": integer cutoff frequency folds
	// bass to mono; "off" disables.
	flag.StringVar(&cfg.MonoBass, "mono-bass", "off", "mono bass crossover: off or cutoff Hz")
	// --batch-dir path, default "": renders every score/MIDI file in the
	// directory with an isolated engine each, then exits.
	flag.StringVar(&cfg.BatchDir, "batch-dir", "", "render all score files in directory")
	// --load-sfz path, default "": validates an SFZ library and reports
	// regions before the score render proceeds.
	flag.StringVar(&cfg.LoadSFZ, "load-sfz", "", "validate an SFZ library before rendering")
	// --version bool, default false: prints the pinned release and exits
	// before any render.
	flag.BoolVar(&cfg.ShowVersion, "version", false, "print Resonata version and exit")

	// Backward compatible aliases for the pre-headless CLI.
	legacyLoad := flag.String("load", "", "legacy alias for --score")
	legacyOutput := flag.String("out", "", "legacy alias for --output")
	flag.Parse()

	if cfg.ScorePath == "" && *legacyLoad != "" {
		cfg.ScorePath = *legacyLoad
	}
	if cfg.OutputPath == "" && *legacyOutput != "" {
		cfg.OutputPath = *legacyOutput
	}
	return cfg
}

func main() {
	// Render pipeline order: parse flags -> optional batch dir (renders a
	// whole directory and exits) -> optional SFZ validation -> load score
	// (file or MIDI import) -> optional humanize (fixed seed, so takes
	// are reproducible) -> optional benchmark -> block render to WAV with
	// progress on stderr and the summary on stdout -> optional MIDI
	// export. The render loop disables the garbage collector around the
	// file write (see renderToFile) so no GC pause can stall a long
	// render. Rendered audio is deterministic: fixed seeds drive all
	// randomness on a single-goroutine audio path, and wall-clock time
	// only feeds progress/benchmark reporting, never samples.
	log.SetPrefix("resonata: ")
	log.SetFlags(0)
	cfg := parseFlags()

	// --version reports the pinned release and exits before any render.
	if cfg.ShowVersion {
		fmt.Printf("Resonata v%s\n", Version)
		return
	}

	if cfg.ProfileCPU != "" {
		if err := startCPUProfile(cfg.ProfileCPU); err != nil {
			log.Fatalf("profile-cpu: %v", err)
		}
		defer stopCPUProfile()
	}
	if cfg.ProfileMem != "" {
		defer func() {
			if err := writeMemProfile(cfg.ProfileMem); err != nil {
				log.Fatalf("profile-mem: %v", err)
			}
		}()
	}

	// Batch mode renders a whole directory and exits.
	if strings.TrimSpace(cfg.BatchDir) != "" {
		if err := runBatch(cfg); err != nil {
			log.Fatal(err)
		}
		return
	}

	// SFZ inspection validates a sample library (Windows-authored paths
	// included) before the score render proceeds.
	if strings.TrimSpace(cfg.LoadSFZ) != "" {
		if err := inspectSFZ(cfg.LoadSFZ, cfg.Verbose); err != nil {
			log.Fatal(err)
		}
	}

	s, err := loadScore(cfg)
	if err != nil {
		log.Fatal(err)
	}

	if cfg.Humanize > 0 {
		if cfg.Verbose {
			log.Printf("applying humanize strength %.2f", cfg.Humanize)
		}
		s = engine.Humanize(s, float32(cfg.Humanize), 1)
	}

	if cfg.Benchmark {
		avg, err := runBenchmark(cfg, s)
		if err != nil {
			log.Fatalf("benchmark: %v", err)
		}
		fmt.Printf("benchmark: 10 renders, avg %v per render\n", avg.Round(time.Millisecond))
	}

	// Progress reporting through stderr keeps stdout clean for scripts.
	progress := make(chan float64, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for p := range progress {
			fmt.Fprintf(os.Stderr, "\rRendering: %5.1f%%", p*100)
		}
		fmt.Fprintln(os.Stderr)
	}()

	stats, err := renderToFile(cfg, s, progress)
	close(progress)
	<-done
	if err != nil {
		log.Fatalf("render: %v", err)
	}

	if cfg.ExportMIDI != "" {
		if err := exportMIDIFile(s, cfg.ExportMIDI); err != nil {
			log.Fatalf("midi export: %v", err)
		}
		if cfg.Verbose {
			log.Printf("exported MIDI to %s", cfg.ExportMIDI)
		}
	}

	fmt.Printf("Rendered %d tracks -> %s (%.2f s, peak %.3f, %v)\n",
		len(s.Tracks), stats.path, stats.seconds, stats.peak,
		stats.elapsed.Round(time.Millisecond))
}

// inspectSFZ validates a sample library through the cross-platform
// loader (sanitized paths, include expansion, case fallback) and
// reports its regions. It proves Windows-authored libraries load on
// any host before the score render proceeds.
func inspectSFZ(path string, verbose bool) error {
	f, err := sampler.LoadSFZ(path)
	if err != nil {
		return fmt.Errorf("load-sfz: %w", err)
	}
	samples := 0
	for i := range f.Regions {
		if f.Regions[i].Sample != nil {
			samples++
		}
		if verbose {
			log.Printf("region %d: %q keys %d-%d", i,
				f.Regions[i].SamplePath, f.Regions[i].LoKey, f.Regions[i].HiKey)
		}
	}
	fmt.Printf("SFZ %s: %d regions, %d decoded samples\n", path, len(f.Regions), samples)
	return nil
}

// loadScore resolves the input score from MIDI import or a score file.
func loadScore(cfg *CLIConfig) (*score.Score, error) {
	if strings.TrimSpace(cfg.ImportMIDI) != "" {
		data, err := os.ReadFile(cfg.ImportMIDI)
		if err != nil {
			return nil, fmt.Errorf("midi import: %w", err)
		}
		f, err := midi.ReadFile(data)
		if err != nil {
			return nil, fmt.Errorf("midi import: %w", err)
		}
		s, err := midi.ToScore(f)
		if err != nil {
			return nil, fmt.Errorf("midi import: %w", err)
		}
		if err := score.Validate(s); err != nil {
			return nil, fmt.Errorf("imported score invalid: %w", err)
		}
		if cfg.Verbose {
			log.Printf("imported %s: %q, %d tracks, %d notes",
				cfg.ImportMIDI, s.Metadata.Title, len(s.Tracks), s.TotalNotes())
		}
		if strings.TrimSpace(cfg.ExportJSON) != "" {
			js, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("export json: %w", err)
			}
			js = append(js, '\n')
			if err := os.WriteFile(cfg.ExportJSON, js, 0o644); err != nil {
				return nil, fmt.Errorf("export json: %w", err)
			}
			if cfg.Verbose {
				log.Printf("wrote imported score to %s", cfg.ExportJSON)
			}
		}
		return s, nil
	}
	if strings.TrimSpace(cfg.ScorePath) == "" {
		return nil, fmt.Errorf("no input: provide --score=path or --import-midi=path (use -h for help)")
	}
	s, err := score.ParseFile(cfg.ScorePath)
	if err != nil {
		return nil, fmt.Errorf("score parse: %w", err)
	}
	if err := score.Validate(s); err != nil {
		return nil, fmt.Errorf("invalid score: %w", err)
	}
	if cfg.Verbose {
		log.Printf("loaded %s: %q, %g BPM %s, %d tracks, %d notes, %.2f s",
			cfg.ScorePath, s.Metadata.Title, s.Metadata.BPM, s.Metadata.TimeSignature,
			len(s.Tracks), s.TotalNotes(), s.Duration())
	}
	return s, nil
}

// renderStats describes one completed file render.
type renderStats struct {
	path    string
	frames  int
	seconds float64
	peak    float32
	elapsed time.Duration
}

// renderToFile renders s to cfg.OutputPath with the configured sample
// rate, bit depth, reverb preset, and master gain. Progress fractions in
// [0,1] are sent to progress when non-nil. The audio loop reuses fixed
// scratch buffers and performs no allocations per block.
func renderToFile(cfg *CLIConfig, s *score.Score, progress chan<- float64) (renderStats, error) {
	var stats renderStats
	mode := strings.ToLower(strings.TrimSpace(cfg.RenderMode))
	if mode == "" {
		mode = "offline"
	}
	if mode != "offline" && mode != "stream" {
		return stats, fmt.Errorf("unknown --mode %q (want offline|stream)", cfg.RenderMode)
	}
	var spec encoder.Spec
	var err error
	if spec, err = encoder.Resolve(cfg.BitDepth, cfg.Channels, cfg.SampleRate); err != nil {
		return stats, err
	}
	sampleRate, channels := spec.SampleRate, spec.Channels
	out := strings.TrimSpace(cfg.OutputPath)
	if out == "" {
		out = "out.wav"
	}
	if cfg.Verbose {
		log.Printf("render mode=%s rate=%d bit=%s ch=%d reverb=%s gain=%.2f -> %s",
			mode, sampleRate, cfg.BitDepth, channels, cfg.ReverbPreset, cfg.MasterGain, out)
	}

	eng, err := engine.New(s, float64(sampleRate), engine.DefaultBlockSize)
	if err != nil {
		return stats, err
	}
	applyReverbPreset(eng, cfg.ReverbPreset)
	if err := applySaturation(eng, cfg.Saturation); err != nil {
		return stats, err
	}
	if err := applyReverbDamping(eng, cfg.ReverbDamping); err != nil {
		return stats, err
	}
	if err := applyNoiseGate(eng, cfg.NoiseGate); err != nil {
		return stats, err
	}
	eng.Mixer().SetLimiterEnabled(cfg.Limiter)
	eng.Mixer().SetParallel(cfg.Parallel)
	if err := applyMonoBass(eng, cfg.MonoBass); err != nil {
		return stats, err
	}
	gain := float32(cfg.MasterGain)
	if !(gain >= 0) || gain > 1 {
		if gain < 0 {
			gain = 0
		} else {
			gain = 1
		}
	}

	f, err := os.Create(out)
	if err != nil {
		return stats, err
	}
	w, err := spec.NewWriter(f)
	if err != nil {
		f.Close()
		return stats, err
	}

	block := eng.BlockSize()
	left := make([]float32, block)
	right := make([]float32, block)
	var mono []float32
	if channels == 1 {
		mono = make([]float32, block)
	}
	total := eng.TotalFrames()
	done := 0
	peak := float32(0)
	start := time.Now()

	// The render loop below is zero-allocation by construction
	// (pre-allocated buffers, fixed ring lines), so disabling the
	// collector removes even the possibility of a GC pause mid-render.
	// Restored to the default schedule once the file is written.
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100)

	report := func() {
		if progress == nil || total <= 0 {
			return
		}
		select {
		case progress <- float64(done) / float64(total):
		default:
		}
	}

	for done < total {
		n := block
		if total-done < n {
			n = total - done
		}
		var writeErr error
		eng.ProcessFrames(n, func(master []float32) {
			frames := len(master) / 2
			for i := 0; i < frames; i++ {
				l := master[2*i] * gain
				r := master[2*i+1] * gain
				left[i], right[i] = l, r
				if a := abs32(l); a > peak {
					peak = a
				}
				if a := abs32(r); a > peak {
					peak = a
				}
			}
			if channels == 1 {
				encoder.DownmixMono(mono[:frames], left[:frames], right[:frames], frames)
				_, writeErr = w.WriteFrames(mono[:frames])
			} else {
				_, writeErr = w.WriteFrames(left[:frames], right[:frames])
			}
		})
		if writeErr != nil {
			w.Close()
			f.Close()
			return stats, writeErr
		}
		done += n
		report()
	}
	if err := w.Close(); err != nil {
		f.Close()
		return stats, err
	}
	if err := f.Close(); err != nil {
		return stats, err
	}
	elapsed := time.Since(start)
	stats = renderStats{
		path:    out,
		frames:  total,
		seconds: float64(total) / float64(sampleRate),
		peak:    peak,
		elapsed: elapsed,
	}
	return stats, nil
}

// applyReverbPreset tunes the engine mixer to the requested space.
// The name "none" bypasses the reverb network. Unknown names keep the
// engine default and are reported when verbose logging is enabled.
func applyReverbPreset(eng *engine.Engine, preset string) {
	name := strings.ToLower(strings.TrimSpace(preset))
	if name == "" {
		return
	}
	if name == "none" || name == "off" || name == "dry" {
		eng.Mixer().SetReverbWet(0)
		return
	}
	if !eng.Mixer().SetReverbPreset(name) {
		log.Printf("unknown --reverb %q, keeping default", preset)
	}
}

// runBenchmark renders the score 10 times in memory without file I/O
// and returns the average wall time per render.
func runBenchmark(cfg *CLIConfig, s *score.Score) (time.Duration, error) {
	var sampleRate int
	switch cfg.SampleRate {
	case 44100, 48000, 96000:
		sampleRate = cfg.SampleRate
	default:
		return 0, fmt.Errorf("unsupported --sample-rate %d (want 44100, 48000, or 96000)", cfg.SampleRate)
	}
	const runs = 10
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100)
	start := time.Now()
	for i := 0; i < runs; i++ {
		eng, err := engine.New(s, float64(sampleRate), engine.DefaultBlockSize)
		if err != nil {
			return 0, err
		}
		applyReverbPreset(eng, cfg.ReverbPreset)
		eng.Mixer().SetParallel(cfg.Parallel)
		total := eng.TotalFrames()
		done := 0
		for done < total {
			n := eng.BlockSize()
			if total-done < n {
				n = total - done
			}
			eng.ProcessFrames(n, nil)
			done += n
		}
	}
	return time.Since(start) / runs, nil
}

// applySaturation selects the master clipper mode: tape keeps tanh
// saturation (the default), clean passes signal linearly with a hard
// clamp at ±1. Unknown names are rejected so typos never render silently.
func applySaturation(eng *engine.Engine, mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "tape":
		eng.Mixer().SetSaturationBypass(false)
		return nil
	case "clean":
		eng.Mixer().SetSaturationBypass(true)
		return nil
	default:
		return fmt.Errorf("unknown --saturation %q (want tape|clean)", mode)
	}
}

// applyReverbDamping overrides the HF air-absorption on every reverb
// instance when d is in [0, 1]: 0.0 is a bright tail with no feedback
// lowpass, 1.0 is the maximum cutoff. Negative values keep the preset
// damping so the default render is byte-identical; anything else is
// rejected so typos never render silently.
func applyReverbDamping(eng *engine.Engine, d float64) error {
	if d < 0 {
		return nil
	}
	if d > 1 || math.IsNaN(d) {
		return fmt.Errorf("unknown --reverb-damping %v (want 0.0-1.0)", d)
	}
	eng.Mixer().SetReverbDamping(float32(d))
	return nil
}

// applyNoiseGate sets the sampler voice noise-gate threshold in dBFS:
// 0 (the default) disables the gate so output is byte-identical,
// negative values mute sub-threshold voice output. Positive or NaN
// values are rejected.
func applyNoiseGate(eng *engine.Engine, db float64) error {
	if db == 0 {
		return nil
	}
	if !(db < 0) {
		return fmt.Errorf("unknown --noise-gate %v (want 0 or negative dBFS)", db)
	}
	eng.SetNoiseGateDB(db)
	return nil
}

// applyMonoBass sets the mono-bass crossover: "off" (the default)
// disables it so output is byte-identical, otherwise an integer cutoff
// frequency in Hz folds bass to mono. Anything else is rejected.
func applyMonoBass(eng *engine.Engine, s string) error {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" || v == "off" {
		return nil
	}
	hz, err := strconv.Atoi(v)
	if err != nil || hz <= 0 {
		return fmt.Errorf("unknown --mono-bass %q (want off or cutoff Hz)", s)
	}
	eng.Mixer().SetMonoBassCutoff(float32(hz))
	return nil
}

// exportMIDIFile converts a score into a format-1 Standard MIDI File.
func exportMIDIFile(s *score.Score, outPath string) error {
	f := midi.FromScore(s, midi.DefaultTicksPerQuarter)
	data := midi.WriteFile(f)
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return err
	}
	events := 0
	for _, tr := range f.Tracks {
		events += len(tr)
	}
	fmt.Printf("exported MIDI -> %s: format %d, %d PPQ, %d tracks, %d events, %d bytes\n",
		outPath, f.Header.Format, f.Header.TicksPerQuarter,
		len(f.Tracks), events, len(data))
	return nil
}

// importMIDIScoreForBatch is a small helper used by batch mode to turn a
// MIDI file into a score without JSON side effects.
func importMIDIScoreForBatch(path string) (*score.Score, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := midi.ReadFile(data)
	if err != nil {
		return nil, err
	}
	return midi.ToScore(f)
}

// abs32 returns the magnitude of v.
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// outputPathForBatch maps an input score path to an output WAV path.
// When outDir names an existing directory the WAV file is placed there
// with the input base name. Otherwise the WAV file sits next to the
// input file or uses outDir as a file path.
func outputPathForBatch(inPath, outDir string) string {
	base := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath)) + ".wav"
	if strings.TrimSpace(outDir) == "" {
		return filepath.Join(filepath.Dir(inPath), base)
	}
	if info, err := os.Stat(outDir); err == nil && info.IsDir() {
		return filepath.Join(outDir, base)
	}
	return outDir
}

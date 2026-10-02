package sampler

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"resonata/pkg/wav"
)

// SFZFile is a parsed SFZ definition: a flat list of regions plus parse
// diagnostics collected by the tolerant scanner.
type SFZFile struct {
	Regions []Region
	Stats   ParseStats
}

// ParseStats counts what the parser saw, for diagnostics and fuzzing.
type ParseStats struct {
	Opcodes        int // non-header key=value tokens
	Known          int // dispatched opcodes
	Unknown        int // silently ignored opcodes
	BadValues      int // malformed values dropped
	Headers        int // <...> tokens
	UnknownHeaders int // ignored headers (<effect>, <curve>, ...)
	EmptyRegions   int // regions discarded for having no sample
	Strays         int // stray bytes/words skipped by the tokenizer
	CommentSkips   int // comment skips performed
}

// Section levels of the SFZ state machine.
const (
	levelGlobal = iota
	levelGroup
	levelRegion
)

// ParseContext carries the hierarchical <global>/<group>/<region> state
// through opcode dispatch.
type ParseContext struct {
	file        *SFZFile
	tok         *Tokenizer
	global      Region // defaults from <global> (or the preamble)
	group       Region // defaults from <group>, includes global
	cur         Region // region being built
	level       int
	inRegion    bool
	defaultPath string // <control> default_path prefix for samples
}

// target returns the Region that opcodes at the current level write to.
func (c *ParseContext) target() *Region {
	switch c.level {
	case levelRegion:
		return &c.cur
	case levelGroup:
		return &c.group
	}
	return &c.global
}

// badValue records a dropped malformed value.
func (c *ParseContext) badValue() { c.file.Stats.BadValues++ }

// ParseSFZ parses SFZ text with the universal tolerant scanner. It never
// fails on messy input: malformed values are dropped (counted in
// Stats.BadValues), unknown opcodes and headers are ignored, and regions
// without a sample are discarded. The returned error is always nil and
// exists for API symmetry with LoadSFZ.
func ParseSFZ(data []byte) (*SFZFile, error) {
	ctx := &ParseContext{file: &SFZFile{}, tok: NewTokenizer(data)}
	ctx.global = newRegion()
	ctx.group = ctx.global
	ctx.cur = ctx.global

	for {
		key, value, err := ctx.tok.NextOpcode()
		if err != nil {
			break // io.EOF: the tolerant scanner produces no other errors
		}
		if len(key) == 0 {
			continue
		}
		if key[0] == '<' {
			ctx.header(key)
			continue
		}
		ctx.file.Stats.Opcodes++
		lk := ctx.tok.LowerKey(key)
		if h, ok := opcodeDispatch[string(lk)]; ok { // non-allocating lookup
			ctx.file.Stats.Known++
			h(ctx, value)
		} else {
			ctx.file.Stats.Unknown++ // silently drop unknown opcodes
		}
	}
	ctx.flushRegion()
	ctx.file.Stats.Strays = ctx.tok.Strays
	ctx.file.Stats.CommentSkips = ctx.tok.CommentSkips
	return ctx.file, nil
}

// header applies a <name> token to the section state machine. Known
// headers flush the open region and move the default chain; unknown ones
// (<effect>, <curve>, <midi>, <master>, ...) are ignored without
// disturbing the current level.
func (c *ParseContext) header(tok []byte) {
	c.file.Stats.Headers++
	name := tok[1:]
	if i := bytes.IndexByte(name, '>'); i >= 0 {
		name = name[:i]
	}
	if i := bytes.IndexAny(name, " \t\r\n"); i >= 0 {
		name = name[:i]
	}
	switch {
	case bytes.EqualFold(name, []byte("global")):
		c.flushRegion()
		c.global = newRegion()
		c.group = c.global
		c.cur = c.global
		c.level = levelGlobal
	case bytes.EqualFold(name, []byte("group")):
		c.flushRegion()
		c.group = c.global
		c.cur = c.group
		c.level = levelGroup
	case bytes.EqualFold(name, []byte("region")):
		c.flushRegion()
		c.cur = c.group
		c.level = levelRegion
		c.inRegion = true
	case bytes.EqualFold(name, []byte("control")):
		// <control> carries file-scope opcodes (default_path); it does
		// not reset the default chain.
		c.flushRegion()
		c.level = levelGlobal
	default:
		c.file.Stats.UnknownHeaders++
	}
}

// flushRegion appends the region being built, normalizing inverted key
// and velocity ranges. Regions without a sample are discarded.
func (c *ParseContext) flushRegion() {
	if !c.inRegion {
		return
	}
	c.inRegion = false
	if c.cur.SamplePath == "" {
		c.file.Stats.EmptyRegions++
		return
	}
	if c.cur.LoKey > c.cur.HiKey {
		c.cur.LoKey, c.cur.HiKey = c.cur.HiKey, c.cur.LoKey
	}
	if c.cur.LoVel > c.cur.HiVel {
		c.cur.LoVel, c.cur.HiVel = c.cur.HiVel, c.cur.LoVel
	}
	if c.cur.LoRand > c.cur.HiRand {
		c.cur.LoRand, c.cur.HiRand = c.cur.HiRand, c.cur.LoRand
	}
	swap := func(lo, hi *int) {
		if *lo > *hi {
			*lo, *hi = *hi, *lo
		}
	}
	swap(&c.cur.XfinLoKey, &c.cur.XfinHiKey)
	swap(&c.cur.XfinLoVel, &c.cur.XfinHiVel)
	swap(&c.cur.XfoutLoKey, &c.cur.XfoutHiKey)
	swap(&c.cur.XfoutLoVel, &c.cur.XfoutHiVel)
	c.file.Regions = append(c.file.Regions, c.cur)
}

// LoadSFZ parses the SFZ file at path and decodes every referenced WAV
// sample. Before parsing, #include directives expand recursively with
// cycle detection. Sample paths are sanitized (backslash normalization,
// quote stripping) and resolve against the SFZ file's directory with a
// case-insensitive fallback on Linux and macOS; regions referencing the
// same resolved file share one decoded SampleData.
func LoadSFZ(path string) (*SFZFile, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	resolver := NewPathResolver(filepath.Dir(abs))
	expander := NewIncludeExpander(resolver)
	data, err := expander.Expand(abs)
	if err != nil {
		return nil, err
	}
	f, err := ParseSFZ(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cache := make(map[string]*SampleData, len(f.Regions))
	for i := range f.Regions {
		rg := &f.Regions[i]
		p, err := resolver.Resolve(rg.SamplePath)
		if err != nil {
			return nil, fmt.Errorf("%s: region %d (%s): %w", path, i, rg.SamplePath, err)
		}
		sd, ok := cache[p]
		if !ok {
			sd, err = loadSample(p)
			if err != nil {
				return nil, fmt.Errorf("%s: region %d (%s): %w", path, i, rg.SamplePath, err)
			}
			cache[p] = sd
		}
		rg.Sample = sd
		// Region-level loop opcodes override the sample's own loop points.
		if rg.LoopStart >= 0 {
			sd.LoopStart = rg.LoopStart
		}
		if rg.LoopEnd >= 0 {
			sd.LoopEnd = rg.LoopEnd
		}
	}
	return f, nil
}

// loadSample decodes a WAV file into mono float32 frames, averaging
// multi-channel sources.
func loadSample(path string) (*SampleData, error) {
	info, samples, err := wav.DecodeFile(path)
	if err != nil {
		return nil, err
	}
	frames := info.Frames
	mono := samples
	if info.Channels > 1 {
		mono = make([]float32, frames)
		inv := 1 / float32(info.Channels)
		for i := 0; i < frames; i++ {
			var sum float32
			base := i * info.Channels
			for c := 0; c < info.Channels; c++ {
				sum += samples[base+c]
			}
			mono[i] = sum * inv
		}
	}
	return &SampleData{
		Samples:    mono,
		SampleRate: info.SampleRate,
		Channels:   info.Channels,
		LoopStart:  info.LoopStart,
		LoopEnd:    info.LoopEnd,
	}, nil
}

// OpcodeHandler applies one opcode value to the parse context.
type OpcodeHandler func(ctx *ParseContext, value []byte)

// opcodeDispatch routes known SFZ opcodes (v1/v2 plus common vendor
// extensions). Opcodes absent from the map are silently ignored, so
// unknown GUI, oscillator, or CC opcodes never fail a load.
var opcodeDispatch = map[string]OpcodeHandler{
	// Zone: sample and key/velocity mapping.
	"sample":          handleSample,
	"lokey":           keyField(func(r *Region) *int { return &r.LoKey }),
	"hikey":           keyField(func(r *Region) *int { return &r.HiKey }),
	"key":             handleKey,
	"pitch_keycenter": keyField(func(r *Region) *int { return &r.PitchKeyCenter }),
	"pitch_keytrack":  intField(func(r *Region) *int { return &r.PitchKeytrack }),
	"lovel":           keyField(func(r *Region) *int { return &r.LoVel }),
	"hivel":           keyField(func(r *Region) *int { return &r.HiVel }),

	// Sample window and looping.
	"offset":         intField(func(r *Region) *int { return &r.Offset }),
	"end":            intField(func(r *Region) *int { return &r.End }),
	"count":          intField(func(r *Region) *int { return &r.Count }),
	"loop_mode":      lowerStringField(func(r *Region) *string { return &r.LoopMode }),
	"loop_start":     intField(func(r *Region) *int { return &r.LoopStart }),
	"loop_end":       intField(func(r *Region) *int { return &r.LoopEnd }),
	"loop_type":      intField(func(r *Region) *int { return &r.LoopType }),
	"loop_count":     intField(func(r *Region) *int { return &r.LoopCount }),
	"loop_tune":      intField(func(r *Region) *int { return &r.LoopTune }),
	"loop_crossfade": intField(func(r *Region) *int { return &r.LoopCrossfade }),
	"reverse":        intField(func(r *Region) *int { return &r.Reverse }),

	// Velocity and key crossfade zones.
	"xfin_lokey":  intField(func(r *Region) *int { return &r.XfinLoKey }),
	"xfin_hikey":  intField(func(r *Region) *int { return &r.XfinHiKey }),
	"xfin_lovel":  intField(func(r *Region) *int { return &r.XfinLoVel }),
	"xfin_hivel":  intField(func(r *Region) *int { return &r.XfinHiVel }),
	"xfout_lokey": intField(func(r *Region) *int { return &r.XfoutLoKey }),
	"xfout_hikey": intField(func(r *Region) *int { return &r.XfoutHiKey }),
	"xfout_lovel": intField(func(r *Region) *int { return &r.XfoutLoVel }),
	"xfout_hivel": intField(func(r *Region) *int { return &r.XfoutHiVel }),

	// Pitch.
	"transpose":      intField(func(r *Region) *int { return &r.Transpose }),
	"tune":           intField(func(r *Region) *int { return &r.Tune }),
	"pitch_random":   intField(func(r *Region) *int { return &r.PitchRandom }),
	"pitch_veltrack": intField(func(r *Region) *int { return &r.PitchVeltrack }),
	"bend_up":        intField(func(r *Region) *int { return &r.BendUp }),
	"bend_down":      intField(func(r *Region) *int { return &r.BendDown }),
	"bend_step":      intField(func(r *Region) *int { return &r.BendStep }),

	// Amplitude.
	"volume":        float32Field(func(r *Region) *float32 { return &r.Volume }),
	"pan":           handlePan,
	"amp_random":    float32Field(func(r *Region) *float32 { return &r.AmpRandom }),
	"amp_veltrack":  float32Field(func(r *Region) *float32 { return &r.AmpVeltrack }),
	"amp_keytrack":  float32Field(func(r *Region) *float32 { return &r.AmpKeytrack }),
	"amp_keycenter": intField(func(r *Region) *int { return &r.AmpKeycenter }),
	"rt_decay":      float32Field(func(r *Region) *float32 { return &r.RtDecay }),

	// Amplitude envelope.
	"ampeg_delay":       egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Delay }),
	"ampeg_start":       egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Start }),
	"ampeg_attack":      egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Attack }),
	"ampeg_hold":        egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Hold }),
	"ampeg_decay":       egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Decay }),
	"ampeg_sustain":     egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Sustain }),
	"ampeg_release":     egTimeField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Release }),
	"ampeg_vel2attack":  egSignedField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Vel2Attack }),
	"ampeg_vel2decay":   egSignedField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Vel2Decay }),
	"ampeg_vel2release": egSignedField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Vel2Release }),
	"ampeg_key2attack":  egSignedField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Key2Attack }),
	"ampeg_key2release": egSignedField(ensureAmp, func(r *Region) *float32 { return &r.AmpEG.Key2Release }),

	// Pitch envelope.
	"pitcheg_delay":     egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Delay }),
	"pitcheg_start":     egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Start }),
	"pitcheg_attack":    egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Attack }),
	"pitcheg_hold":      egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Hold }),
	"pitcheg_decay":     egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Decay }),
	"pitcheg_sustain":   egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Sustain }),
	"pitcheg_release":   egTimeField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Release }),
	"pitcheg_depth":     egSignedField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Depth }),
	"pitcheg_vel2depth": egSignedField(ensurePitch, func(r *Region) *float32 { return &r.PitchEG.Vel2Depth }),

	// Filter envelope.
	"fileg_delay":     egTimeField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Delay }),
	"fileg_attack":    egTimeField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Attack }),
	"fileg_decay":     egTimeField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Decay }),
	"fileg_sustain":   egTimeField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Sustain }),
	"fileg_release":   egTimeField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Release }),
	"fileg_depth":     egSignedField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Depth }),
	"fileg_vel2depth": egSignedField(ensureFil, func(r *Region) *float32 { return &r.FilEG.Vel2Depth }),

	// Filter.
	"fil_type":      lowerStringField(func(r *Region) *string { return &r.FilType }),
	"cutoff":        float32Field(func(r *Region) *float32 { return &r.Cutoff }),
	"resonance":     float32Field(func(r *Region) *float32 { return &r.Resonance }),
	"fil_keytrack":  float32Field(func(r *Region) *float32 { return &r.FilKeytrack }),
	"fil_keycenter": intField(func(r *Region) *int { return &r.FilKeycenter }),
	"fil_veltrack":  float32Field(func(r *Region) *float32 { return &r.FilVeltrack }),
	"fil_random":    float32Field(func(r *Region) *float32 { return &r.FilRandom }),

	// LFO 1.
	"lfo01_delay":         lfoField(0, func(p *LFOParams) *float32 { return &p.Delay }),
	"lfo01_fade":          lfoField(0, func(p *LFOParams) *float32 { return &p.Fade }),
	"lfo01_freq":          lfoField(0, func(p *LFOParams) *float32 { return &p.Freq }),
	"lfo01_volume":        lfoField(0, func(p *LFOParams) *float32 { return &p.Volume }),
	"lfo01_amplitude":     lfoField(0, func(p *LFOParams) *float32 { return &p.Amplitude }),
	"lfo01_pitch":         lfoField(0, func(p *LFOParams) *float32 { return &p.Pitch }),
	"lfo01_filter":        lfoField(0, func(p *LFOParams) *float32 { return &p.Filter }),
	"lfo01_pan":           lfoField(0, func(p *LFOParams) *float32 { return &p.Pan }),
	"lfo01_volume_smooth": lfoField(0, func(p *LFOParams) *float32 { return &p.VolumeSmooth }),
	"lfo01_wave":          lfoIntField(0, func(p *LFOParams) *int { return &p.Wave }),
	"lfo01_freq_wave":     lfoIntField(0, func(p *LFOParams) *int { return &p.FreqWave }),

	// LFO 2.
	"lfo02_delay":         lfoField(1, func(p *LFOParams) *float32 { return &p.Delay }),
	"lfo02_fade":          lfoField(1, func(p *LFOParams) *float32 { return &p.Fade }),
	"lfo02_freq":          lfoField(1, func(p *LFOParams) *float32 { return &p.Freq }),
	"lfo02_volume":        lfoField(1, func(p *LFOParams) *float32 { return &p.Volume }),
	"lfo02_amplitude":     lfoField(1, func(p *LFOParams) *float32 { return &p.Amplitude }),
	"lfo02_pitch":         lfoField(1, func(p *LFOParams) *float32 { return &p.Pitch }),
	"lfo02_filter":        lfoField(1, func(p *LFOParams) *float32 { return &p.Filter }),
	"lfo02_pan":           lfoField(1, func(p *LFOParams) *float32 { return &p.Pan }),
	"lfo02_volume_smooth": lfoField(1, func(p *LFOParams) *float32 { return &p.VolumeSmooth }),
	"lfo02_wave":          lfoIntField(1, func(p *LFOParams) *int { return &p.Wave }),
	"lfo02_freq_wave":     lfoIntField(1, func(p *LFOParams) *int { return &p.FreqWave }),

	// Legacy single-target LFO names, routed onto LFO 1.
	"amp_lfo_delay":   lfoField(0, func(p *LFOParams) *float32 { return &p.Delay }),
	"amp_lfo_freq":    lfoField(0, func(p *LFOParams) *float32 { return &p.Freq }),
	"amp_lfo_depth":   lfoField(0, func(p *LFOParams) *float32 { return &p.Volume }),
	"pitch_lfo_freq":  lfoField(0, func(p *LFOParams) *float32 { return &p.Freq }),
	"pitch_lfo_depth": lfoField(0, func(p *LFOParams) *float32 { return &p.Pitch }),
	"fil_lfo_freq":    lfoField(0, func(p *LFOParams) *float32 { return &p.Freq }),
	"fil_lfo_depth":   lfoField(0, func(p *LFOParams) *float32 { return &p.Filter }),

	// EQ bands 1-3.
	"eq1_freq":     eqField(0, func(b *EQBand) *float32 { return &b.Freq }),
	"eq1_bw":       eqField(0, func(b *EQBand) *float32 { return &b.BW }),
	"eq1_gain":     eqField(0, func(b *EQBand) *float32 { return &b.Gain }),
	"eq1_veltrack": eqField(0, func(b *EQBand) *float32 { return &b.Veltrack }),
	"eq2_freq":     eqField(1, func(b *EQBand) *float32 { return &b.Freq }),
	"eq2_bw":       eqField(1, func(b *EQBand) *float32 { return &b.BW }),
	"eq2_gain":     eqField(1, func(b *EQBand) *float32 { return &b.Gain }),
	"eq2_veltrack": eqField(1, func(b *EQBand) *float32 { return &b.Veltrack }),
	"eq3_freq":     eqField(2, func(b *EQBand) *float32 { return &b.Freq }),
	"eq3_bw":       eqField(2, func(b *EQBand) *float32 { return &b.BW }),
	"eq3_gain":     eqField(2, func(b *EQBand) *float32 { return &b.Gain }),
	"eq3_veltrack": eqField(2, func(b *EQBand) *float32 { return &b.Veltrack }),

	// Voice behavior.
	"off_by":         intField(func(r *Region) *int { return &r.OffBy }),
	"off_mode":       lowerStringField(func(r *Region) *string { return &r.OffMode }),
	"polyphony":      intField(func(r *Region) *int { return &r.Polyphony }),
	"note_polyphony": intField(func(r *Region) *int { return &r.NotePolyphony }),
	"trigger":        lowerStringField(func(r *Region) *string { return &r.Trigger }),
	"seq_length":     seqField(func(r *Region) *int { return &r.SeqLength }),
	"seq_position":   seqField(func(r *Region) *int { return &r.SeqPosition }),
	"lorand":         float32Field(func(r *Region) *float32 { return &r.LoRand }),
	"hirand":         float32Field(func(r *Region) *float32 { return &r.HiRand }),
	"group_label":    rawStringField(func(r *Region) *string { return &r.GroupLabel }),
	"label":          rawStringField(func(r *Region) *string { return &r.GroupLabel }),
	"comment":        handleComment,
	"default_path":   handleDefaultPath,

	// Keyswitching.
	"sw_lokey":   keyField(func(r *Region) *int { return &r.SwLoKey }),
	"sw_hikey":   keyField(func(r *Region) *int { return &r.SwHiKey }),
	"sw_default": keyField(func(r *Region) *int { return &r.SwDefault }),
	"sw_last":    keyField(func(r *Region) *int { return &r.SwLast }),
	"sw_down":    keyField(func(r *Region) *int { return &r.SwDown }),
	"sw_up":      keyField(func(r *Region) *int { return &r.SwUp }),
}

// handleSample assigns the sample path, prefixed with the <control>
// default_path. A second sample opcode inside an open region flushes it
// and starts a fresh one, so header-less SFZ files (one "sample=..."
// line per zone) parse correctly.
func handleSample(ctx *ParseContext, value []byte) {
	path := SanitizePath(string(value))
	if path == "" {
		ctx.badValue()
		return
	}
	if ctx.defaultPath != "" && !filepath.IsAbs(filepath.FromSlash(path)) {
		path = ctx.defaultPath + path
	}
	// Lexical cleanup keeps diagnostics readable; resolution handles
	// the rest (case fallback, relative joins).
	path = slashClean(path)
	switch {
	case ctx.inRegion && ctx.cur.SamplePath != "":
		ctx.flushRegion()
		ctx.cur = ctx.group
		ctx.inRegion = true
	case !ctx.inRegion:
		ctx.cur = ctx.group
		ctx.inRegion = true
		ctx.level = levelRegion
	}
	ctx.cur.SamplePath = path
}

// handleKey implements the key= shortcut: one value sets lokey, hikey,
// and pitch_keycenter together.
func handleKey(ctx *ParseContext, value []byte) {
	n, ok := parseKeyBytes(value)
	if !ok || n < 0 || n > 127 {
		ctx.badValue()
		return
	}
	r := ctx.target()
	r.LoKey, r.HiKey, r.PitchKeyCenter = n, n, n
}

// handlePan converts SFZ pan percent (-100..100) to normalized [-1,1].
func handlePan(ctx *ParseContext, value []byte) {
	f, ok := parseFloatBytes(value)
	if !ok {
		ctx.badValue()
		return
	}
	ctx.target().Pan = float32(clampF(f/100, -1, 1))
}

// handleDefaultPath stores the sanitized <control> default_path prefix
// applied to every subsequent relative sample path.
func handleDefaultPath(ctx *ParseContext, value []byte) {
	p := SanitizePath(string(value))
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	ctx.defaultPath = p
}

// handleComment absorbs comment= opcodes.
func handleComment(*ParseContext, []byte) {}

// ensureAmp/ensurePitch/ensureFil adapt the Region methods to the
// handler factories.
func ensureAmp(r *Region)   { r.ensureAmpEG() }
func ensurePitch(r *Region) { r.ensurePitchEG() }
func ensureFil(r *Region)   { r.ensureFilEG() }

// keyField builds a handler for key-range opcodes, accepting MIDI
// numbers or scientific pitch names (c4, eb2, c#-1).
func keyField(sel func(*Region) *int) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		n, ok := parseKeyBytes(value)
		if !ok || n < 0 || n > 127 {
			ctx.badValue()
			return
		}
		*sel(ctx.target()) = n
	}
}

// intField builds a handler for plain signed integer opcodes.
func intField(sel func(*Region) *int) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		n, ok := parseIntBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		*sel(ctx.target()) = n
	}
}

// float32Field builds a handler for float opcodes with any finite value.
func float32Field(sel func(*Region) *float32) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		f, ok := parseFloatBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		*sel(ctx.target()) = float32(f)
	}
}

// lowerStringField stores a lowercased string (modes and types).
func lowerStringField(sel func(*Region) *string) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		*sel(ctx.target()) = strings.ToLower(strings.TrimSpace(string(value)))
	}
}

// rawStringField stores a trimmed string preserving case (labels).
func rawStringField(sel func(*Region) *string) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		*sel(ctx.target()) = strings.TrimSpace(string(value))
	}
}

// egTimeField builds a handler for non-negative envelope times/levels.
func egTimeField(ensure func(*Region), sel func(*Region) *float32) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		f, ok := parseFloatBytes(value)
		if !ok || f < 0 {
			ctx.badValue()
			return
		}
		r := ctx.target()
		ensure(r)
		*sel(r) = float32(f)
	}
}

// egSignedField builds a handler for signed envelope depths/veltrack.
func egSignedField(ensure func(*Region), sel func(*Region) *float32) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		f, ok := parseFloatBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		r := ctx.target()
		ensure(r)
		*sel(r) = float32(f)
	}
}

// seqField builds a handler for 1-based round-robin sequence opcodes.
// Values below 1 are malformed and dropped, keeping the default of 1
// (no cycling). It dispatches through the level target like every other
// opcode, so seq_length and seq_position inherit from <global> and
// <group> into <region> by the standard rules.
func seqField(sel func(*Region) *int) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		n, ok := parseIntBytes(value)
		if !ok || n < 1 {
			ctx.badValue()
			return
		}
		*sel(ctx.target()) = n
	}
}

// lfoField builds a handler for one float field of LFO idx.
func lfoField(idx int, sel func(*LFOParams) *float32) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		f, ok := parseFloatBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		*sel(&ctx.target().LFO[idx]) = float32(f)
	}
}

// lfoIntField builds a handler for one int field of LFO idx.
func lfoIntField(idx int, sel func(*LFOParams) *int) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		n, ok := parseIntBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		*sel(&ctx.target().LFO[idx]) = n
	}
}

// eqField builds a handler for one float field of EQ band idx.
func eqField(band int, sel func(*EQBand) *float32) OpcodeHandler {
	return func(ctx *ParseContext, value []byte) {
		f, ok := parseFloatBytes(value)
		if !ok {
			ctx.badValue()
			return
		}
		*sel(&ctx.target().EQ[band]) = float32(f)
	}
}

// parseKeyBytes parses a key value: a MIDI number or a pitch name.
func parseKeyBytes(b []byte) (int, bool) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return 0, false
	}
	if n, ok := parseIntBytes(b); ok {
		return n, true
	}
	return ParsePitch(b)
}

// parseIntBytes parses an optionally signed base-10 integer from bytes
// without allocating.
func parseIntBytes(b []byte) (int, bool) {
	b = bytes.TrimSpace(b)
	i, neg := 0, false
	if len(b) > 0 && (b[0] == '+' || b[0] == '-') {
		neg = b[0] == '-'
		i++
	}
	if i >= len(b) {
		return 0, false
	}
	n := 0
	for ; i < len(b); i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 1<<30 {
			return 0, false // overflow guard
		}
	}
	if neg {
		n = -n
	}
	return n, true
}

// parseFloatBytes parses a finite decimal value. The string conversion
// happens once at load time; parsing is not a hot path.
func parseFloatBytes(b []byte) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// slashClean lexically cleans a slash-separated path without touching
// the OS separator rules.
func slashClean(p string) string {
	if p == "" {
		return p
	}
	leading := strings.HasPrefix(p, "/")
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
				continue
			}
			out = append(out, part)
		default:
			out = append(out, part)
		}
	}
	cleaned := strings.Join(out, "/")
	if leading {
		cleaned = "/" + cleaned
	}
	if cleaned == "" {
		return "."
	}
	return cleaned
}

// clampF bounds v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	switch {
	case v < lo || v != v:
		return lo
	case v > hi:
		return hi
	}
	return v
}

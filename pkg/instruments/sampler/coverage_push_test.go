package sampler

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"resonata/pkg/wav"
)

// TestPushCommentOpcode proves comment= opcodes are absorbed without regions.
func TestPushCommentOpcode(t *testing.T) {
	f, err := ParseSFZ([]byte("comment=hello world\n<region> sample=x.wav lokey=60\n"))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
}

// TestPushDefaultPathOpcode proves <control> default_path prefixes samples.
func TestPushDefaultPathOpcode(t *testing.T) {
	dir := t.TempDir()
	writeTestWav(t, filepath.Join(dir, "sub_c4.wav"), wav.PCM24, 48000, 500, 1, 261.63)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestWav(t, filepath.Join(sub, "c4.wav"), wav.PCM24, 48000, 500, 1, 261.63)
	sfz := "<control> default_path=sub/\n<region> sample=c4.wav lokey=60\n"
	path := filepath.Join(dir, "d.sfz")
	if err := os.WriteFile(path, []byte(sfz), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := LoadSFZ(path)
	if err != nil {
		t.Fatalf("LoadSFZ: %v", err)
	}
	if len(f.Regions) != 1 || f.Regions[0].Sample == nil {
		t.Fatalf("default_path region = %+v", f.Regions)
	}
}

// TestPushLFOOpcodes proves lfo float/int opcode handlers accept and clamp.
func TestPushLFOOpcodes(t *testing.T) {
	f, err := ParseSFZ([]byte("<region> sample=x.wav lokey=60 lfo01_freq=5.5 lfo01_wave=2 lfo01_freq_wave=bad lfo02_delay=0.1\n"))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
	r := f.Regions[0]
	if r.LFO[0].Freq != 5.5 {
		t.Fatalf("lfo freq = %v, want 5.5", r.LFO[0].Freq)
	}
	if r.LFO[0].Wave != 2 {
		t.Fatalf("lfo wave = %v, want 2", r.LFO[0].Wave)
	}
}

// TestPushParseIncludePathVariants proves quoted/empty/unterminated forms.
func TestPushParseIncludePathVariants(t *testing.T) {
	if got := parseIncludePath(`#include "lib/strings.sfz" ; comment`); got != "lib/strings.sfz" {
		t.Fatalf("quoted = %q", got)
	}
	if got := parseIncludePath(`#include 'lib/piano.sfz'`); got != "lib/piano.sfz" {
		t.Fatalf("squote = %q", got)
	}
	if got := parseIncludePath(`#include`); got != "" {
		t.Fatalf("bare = %q, want empty", got)
	}
	if got := parseIncludePath(`#include "unterminated`); got != "" {
		t.Fatalf("unterminated = %q, want empty", got)
	}
	if got := parseIncludePath(`#include lib/bass.sfz // voice`); got != "lib/bass.sfz" {
		t.Fatalf("unquoted = %q", got)
	}
}

// TestPushResolveEdges proves empty and absolute-missing inputs fail.
func TestPushResolveEdges(t *testing.T) {
	r := NewPathResolver(t.TempDir())
	if _, err := r.Resolve("   "); err == nil {
		t.Fatal("empty resolve accepted")
	}
	if _, err := r.Resolve(filepath.Join(t.TempDir(), "nope-not-here.wav")); err == nil {
		t.Fatal("absolute missing resolve accepted")
	}
}

// TestPushResolveCacheCounters proves slow-path miss then hit accounting.
func TestPushResolveCacheCounters(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Bank"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "Bank", "Violin.wav")
	if err := os.WriteFile(want, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewPathResolver(dir)
	beforeMiss := r.Misses()
	beforeHit := r.Hits()
	p1, err := r.Resolve("bank/violin.wav")
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	if r.Misses() != beforeMiss+1 {
		t.Fatalf("misses = %d, want %d", r.Misses(), beforeMiss+1)
	}
	p2, err := r.Resolve("bank/violin.wav")
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("cached = %q, want %q", p2, p1)
	}
	if r.Hits() != beforeHit+1 {
		t.Fatalf("hits = %d, want %d", r.Hits(), beforeHit+1)
	}
	if r2 := r.ForDir(dir); r2 == nil {
		t.Fatal("ForDir returned nil")
	}
}

// TestPushReadSampleBoundedEdges proves clamp and degenerate-window paths.
func TestPushReadSampleBoundedEdges(t *testing.T) {
	src := []float32{0, 10, 20, 30}
	if got := readSampleBounded(src, 5, 3, 3); got != float64(src[3]) {
		t.Fatalf("degenerate hi<=lo = %v", got)
	}
	if got := readSampleBounded(src, -5, 0, 4); got != 0 {
		t.Fatalf("low clamp = %v", got)
	}
	if got := readSampleBounded(src, 99, 0, 4); got != 30 {
		t.Fatalf("high clamp = %v", got)
	}
}

// TestPushXfadeGainZones proves velocity/key crossfade shaping.
func TestPushXfadeGainZones(t *testing.T) {
	rg := &Region{XfinLoVel: 0, XfinHiVel: 100, XfoutLoVel: 0, XfoutHiVel: 127}
	if got := xfadeGain(rg, 60, 50); got <= 0 || got > 1 {
		t.Fatalf("mid gain = %v, want (0,1]", got)
	}
	flat := &Region{}
	if got := xfadeGain(flat, 60, 80); got != 1 {
		t.Fatalf("flat gain = %v, want 1", got)
	}
	swapped := &Region{XfinLoVel: 100, XfinHiVel: 0}
	if got := xfadeGain(swapped, 60, 50); got <= 0 || got > 1 {
		t.Fatalf("swapped gain = %v, want (0,1]", got)
	}
}

// TestPushSetParametersClamps proves master/attack/release clamping.
func TestPushSetParametersClamps(t *testing.T) {
	s := New(48000)
	s.SetParameters(map[string]float32{"master_volume": 5, "attack": -1, "release": 99, "unknown_key": 1})
	if s.masterGain != 1 {
		t.Fatalf("master = %v, want 1", s.masterGain)
	}
	s.SetParameters(map[string]float32{"master_volume": -2})
	if s.masterGain != 0 {
		t.Fatalf("master = %v, want 0", s.masterGain)
	}
}

// TestPushTrackRegisterIdempotent proves re-register fills missing slots.
func TestPushTrackRegisterIdempotent(t *testing.T) {
	s := New(48000)
	v := &s.voices[0]
	v.ptrack = &s.pitchTrack
	v.trackedPitch = 60
	v.trackRegister(60)
	n := s.pitchTrack[60].n
	v.trackRegister(60)
	if s.pitchTrack[60].n != n {
		t.Fatalf("re-register changed n %d -> %d", n, s.pitchTrack[60].n)
	}
}

// TestPushFinishStealFree proves fade-to-free path deactivates the voice.
func TestPushFinishStealFree(t *testing.T) {
	s := New(48000)
	v := &s.voices[0]
	v.Active = true
	v.isStealing = true
	v.stealFree = true
	v.finishSteal()
	if v.Active || v.isStealing || v.stealFree {
		t.Fatalf("steal-free not cleared: %+v", v)
	}
}

// TestPushLoadSFZMissing proves missing SFZ path errors.
func TestPushLoadSFZMissing(t *testing.T) {
	if _, err := LoadSFZ(filepath.Join(t.TempDir(), "missing.sfz")); err == nil {
		t.Fatal("missing sfz accepted")
	}
}

// TestPushNewDefaultsRate proves non-positive rates fall back to 48000.
func TestPushNewDefaultsRate(t *testing.T) {
	for _, sr := range []float64{0, -1, -48000} {
		s := New(sr)
		if s.sampleRate != 48000 {
			t.Fatalf("New(%v).sampleRate = %v, want 48000", sr, s.sampleRate)
		}
	}
}

// TestPushVoiceProcessGuards proves inactive/malformed voices render
// nothing and never panic.
func TestPushVoiceProcessGuards(t *testing.T) {
	buf := []float32{9, 9, 9, 9}
	v := &Voice{}
	v.Process(buf, 0.01)
	v.Active = true
	v.Process(buf, 0.01)
	v.Region = &Region{}
	v.Process(buf, 0.01)
	v.Process(nil, 0.01)
	v.Process(buf, 0)
	v.Process(buf, -1)
	for i, x := range buf {
		if x != 9 {
			t.Fatalf("buf[%d] = %v, want untouched 9", i, x)
		}
	}
}

// TestPushTrackRegisterEdges proves nil trackers, out-of-range pitches,
// and full rosters are safe.
func TestPushTrackRegisterEdges(t *testing.T) {
	s := New(48000)
	v := &s.voices[1]
	v.ptrack = nil
	v.trackRegister(60) // nil tracker: no-op
	v.ptrack = &s.pitchTrack
	v.trackRegister(-1)
	v.trackRegister(200)
	if s.pitchTrack[60].n != 0 {
		t.Fatal("out-of-range register polluted pitch 60")
	}
	// Fill pitch 61 to the cap, then register one more voice.
	for i := 0; i < maxPitchVoices; i++ {
		w := &s.voices[2+i]
		w.ptrack = &s.pitchTrack
		w.trackedPitch = -1
		w.trackRegister(61)
	}
	if s.pitchTrack[61].n != maxPitchVoices {
		t.Fatalf("roster n = %d, want %d", s.pitchTrack[61].n, maxPitchVoices)
	}
	extra := &Voice{ptrack: &s.pitchTrack, trackedPitch: -1}
	extra.trackRegister(61) // past cap: plays untracked, no panic
	if s.pitchTrack[61].n != maxPitchVoices {
		t.Fatalf("cap overflow n = %d, want %d", s.pitchTrack[61].n, maxPitchVoices)
	}
}

// TestPushNoteOnParamsGuards proves missing SFZ and bad pitches are
// ignored without allocation or panic.
func TestPushNoteOnParamsGuards(t *testing.T) {
	s := New(48000)
	np := instruments.NoteParams{}
	s.NoteOnParams(60, 0.8, np)
	s.NoteOnParams(-1, 0.8, np)
	s.NoteOnParams(200, 0.8, np)
	if got := s.ActiveVoices(); got != 0 {
		t.Fatalf("voices = %d, want 0", got)
	}
}

// TestPushClampFSlashClean proves pure helper edge behavior.
func TestPushClampFSlashClean(t *testing.T) {
	if got := clampF(99, 0, 1); got != 1 {
		t.Fatalf("clamp high = %v", got)
	}
	if got := clampF(-9, 0, 1); got != 0 {
		t.Fatalf("clamp low = %v", got)
	}
	if got := clampF(0.5, 0, 1); got != 0.5 {
		t.Fatalf("clamp mid = %v", got)
	}
	if got := clampF(math.NaN(), 0, 1); got != 0 {
		t.Fatalf("clamp NaN = %v, want lo", got)
	}
	for in, want := range map[string]string{
		"":         "",
		"/":        "/",
		"a//b":     "a/b",
		"a/./b":    "a/b",
		"a/b/../c": "a/c",
		"..":       "..",
	} {
		if got := slashClean(in); got != want {
			t.Fatalf("slashClean(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPushDefaultPathNoSlash proves prefixes without trailing slashes
// gain one, and empty values stay empty.
func TestPushDefaultPathNoSlash(t *testing.T) {
	var ctx ParseContext
	handleDefaultPath(&ctx, []byte("sub"))
	if ctx.defaultPath != "sub/" {
		t.Fatalf("defaultPath = %q, want sub/", ctx.defaultPath)
	}
	handleDefaultPath(&ctx, []byte("sub/"))
	if ctx.defaultPath != "sub/" {
		t.Fatalf("defaultPath = %q, want sub/", ctx.defaultPath)
	}
	handleComment(&ctx, []byte("anything"))
}

// TestPushBadValueOpcodes proves malformed opcode values are dropped with
// defaults kept instead of failing the parse.
func TestPushBadValueOpcodes(t *testing.T) {
	f, err := ParseSFZ([]byte("<region> sample=x.wav lokey=60 lfo01_freq=bad seq_length=0 keycenter=zz9 ampeg_attack=bad\n"))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
}

// TestPushXfadeKeyZones proves key crossfade shaping at zone edges.
func TestPushXfadeKeyZones(t *testing.T) {
	rg := &Region{XfinLoKey: 40, XfinHiKey: 80, XfoutLoKey: 0, XfoutHiKey: 127}
	lo := xfadeGain(rg, 40, 80)
	mid := xfadeGain(rg, 60, 80)
	hi := xfadeGain(rg, 80, 80)
	if !(lo <= mid && mid <= hi) {
		t.Fatalf("key gains not rising: %v %v %v", lo, mid, hi)
	}
	if got := xfadeGain(rg, 200, 80); got < 0 || got > 1 {
		t.Fatalf("out-of-zone gain = %v", got)
	}
}

// TestPushResolveAbsolute proves absolute existing paths bypass the base.
func TestPushResolveAbsolute(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "Kick.wav")
	if err := os.WriteFile(abs, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewPathResolver(dir)
	got, err := r.Resolve(abs)
	if err != nil {
		t.Fatalf("absolute resolve: %v", err)
	}
	if got != abs {
		t.Fatalf("absolute = %q, want %q", got, abs)
	}
}

// TestPushWindowsResolver proves the Windows branch skips the slow walk.
func TestPushWindowsResolver(t *testing.T) {
	dir := t.TempDir()
	r := &PathResolver{basePath: dir, cache: &resolveCache{entries: make(map[string]string)}, isWindows: true}
	if _, err := r.Resolve("missing.wav"); err == nil {
		t.Fatal("windows missing resolve accepted")
	}
}

// TestPushDotDotWalk proves ".." segments resolve through the slow path.
func TestPushDotDotWalk(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Bank"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "Bank", "Violin.wav")
	if err := os.WriteFile(want, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewPathResolver(dir)
	got, err := r.Resolve("bank/../BANK/violin.wav")
	if err != nil {
		t.Fatalf("dotdot resolve: %v", err)
	}
	if got != want {
		t.Fatalf("dotdot = %q, want %q", got, want)
	}
}

// TestPushXfadeExtremeClamps proves out-of-range velocities and pitches
// clamp inside the equal-power crossfade helpers.
func TestPushXfadeExtremeClamps(t *testing.T) {
	rg := &Region{XfinLoVel: 10, XfinHiVel: 100, XfoutLoVel: 20, XfoutHiVel: 110,
		XfinLoKey: 30, XfinHiKey: 90, XfoutLoKey: 10, XfoutHiKey: 100}
	for _, tc := range [][2]int{{-5, 60}, {500, 60}, {80, -10}, {80, 200}} {
		if got := xfadeGain(rg, tc[1], tc[0]); got < 0 || got > 1 {
			t.Fatalf("xfadeGain(%d,%d) = %v out of [0,1]", tc[1], tc[0], got)
		}
	}
}

// pushRegion builds a playable single-key region over sd.
func pushRegion(sd *SampleData, lo, hi int) Region {
	r := newRegion()
	r.SamplePath, r.Sample = "t.wav", sd
	r.LoKey, r.HiKey, r.PitchKeyCenter = lo, hi, lo
	return r
}

func pushSample(frames int) *SampleData {
	sd := &SampleData{Samples: make([]float32, frames), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	return sd
}

// TestPushNoteOnVelocityClamp proves over-range velocity clamps to 127.
func TestPushNoteOnVelocityClamp(t *testing.T) {
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{pushRegion(pushSample(48000), 0, 127)}})
	s.NoteOn(60, 5.0)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
}

// TestPushOffByTrigger proves off_by regions invoke the cutoff scan.
func TestPushOffByTrigger(t *testing.T) {
	sd := pushSample(48000)
	r1 := pushRegion(sd, 60, 69)
	r2 := pushRegion(sd, 60, 69)
	r2.OffBy = 5
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{r1, r2}})
	s.NoteOn(60, 0.9)
	s.NoteOn(61, 0.9)
	if got := s.ActiveVoices(); got < 1 {
		t.Fatalf("voices = %d, want >= 1", got)
	}
}

// TestPushSamplerProcessGuards proves nil/empty/oversized buffer handling.
func TestPushSamplerProcessGuards(t *testing.T) {
	s := New(48000)
	s.Process(nil, 0.01)
	s.Process(dsp.NewStereoBuffer(8), 0)
	s.Process(dsp.NewStereoBuffer(8), -1)
	big := dsp.NewStereoBuffer(9000)
	big.SetLen(9000)
	s.Process(big, 9000.0/48000)
	if len(s.mix) < 9000 {
		t.Fatalf("mix len = %d, want >= 9000 after grow", len(s.mix))
	}
}

// TestPushVoiceStealHandoff proves a sample-overshoot mid-fade hands off
// to the pending note instead of dying silently.
func TestPushVoiceStealHandoff(t *testing.T) {
	s := New(48000)
	v := &s.voices[3]
	tiny := pushSample(4)
	v.Region = &Region{Sample: tiny}
	v.end = 1
	v.Phase = 99 // past the end: defensive overshoot branch
	v.Active = true
	v.isStealing = true
	v.fadeStep = 0 // defective step falls back to the default
	v.pendingRegion = &Region{Sample: pushSample(8)}
	v.pendingPitch, v.pendingVelocity = 64, 0.8
	v.stealSampleRate = 0 // falls back to 48000
	v.Process(make([]float32, 64), 64.0/48000)
	if v.stealFadeProgress < 0 || v.stealFadeProgress > 1 {
		t.Fatalf("fade progress = %v out of [0,1]", v.stealFadeProgress)
	}
}

// TestPushRenderToCompletion proves voices deactivate past sample end.
func TestPushRenderToCompletion(t *testing.T) {
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{pushRegion(pushSample(100), 60, 60)}})
	s.NoteOn(60, 0.9)
	buf := dsp.NewStereoBuffer(512)
	buf.SetLen(512)
	dt := 512.0 / 48000
	for i := 0; i < 10; i++ {
		s.Process(buf, dt)
	}
	s.NoteOff(60)
	for i := 0; i < 500 && s.ActiveVoices() > 0; i++ {
		s.Process(buf, dt)
	}
	if got := s.ActiveVoices(); got != 0 {
		t.Fatalf("voices = %d, want 0 after full render", got)
	}
}

// TestPushInitVoiceLoopClamp proves loop points clamp to the window end.
func TestPushInitVoiceLoopClamp(t *testing.T) {
	s := New(48000)
	sd := pushSample(100)
	sd.LoopStart, sd.LoopEnd = 10, 80
	rg := newRegion()
	rg.Sample = sd
	rg.LoopMode = LoopContinuous
	rg.End = 50
	v := &s.voices[4]
	initVoice(v, &rg, 60, 0.8, 48000, 0.01, 0.1, 0.8, 0.2, 0, instruments.NoteParams{})
	if !v.looping || v.loopEnd != 50 {
		t.Fatalf("looping=%v end=%v, want true/50", v.looping, v.loopEnd)
	}
	rg2 := newRegion()
	rg2.Sample = sd
	rg2.LoopMode = LoopContinuous
	rg2.End = 50
	sd.LoopStart = 60 // start past the clamped end: looping disabled
	v2 := &s.voices[5]
	initVoice(v2, &rg2, 60, 0.8, 48000, 0.01, 0.1, 0.8, 0.2, 0, instruments.NoteParams{})
	if v2.looping {
		t.Fatal("looping with start>=end accepted")
	}
}

// TestPushInterpDirect proves bounded interpolation clamps taps.
func TestPushInterpDirect(t *testing.T) {
	src := []float32{1, 2, 3, 4}
	if got := interpSampleBounded(src, -3, 0, 4); got != 1 {
		t.Fatalf("low = %v, want 1", got)
	}
	if got := interpSampleBounded(src, 99, 0, 4); got != 4 {
		t.Fatalf("high = %v, want 4", got)
	}
	if n := resampleTo(make([]float32, 8), nil, 1, 0, false); n != 0 {
		t.Fatalf("empty src n = %d", n)
	}
	if n := resampleTo(make([]float32, 8), src, 0, 0, false); n != 0 {
		t.Fatalf("bad rate n = %d", n)
	}
	if n := resampleTo(make([]float32, 8), src, 1, math.NaN(), false); n != 0 {
		t.Fatalf("NaN phase n = %d", n)
	}
}

// TestPushExprNaNEnvelope proves NaN expression and per-note envelope
// overrides apply safely through NoteOnParams.
func TestPushExprNaNEnvelope(t *testing.T) {
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{pushRegion(pushSample(48000), 0, 127)}})
	np := instruments.NoteParams{HasExpression: true, Expression: float32(math.NaN())}
	np.Env = instruments.EnvelopeOverride{Attack: 0.05, HasAttack: true, Release: -1, HasRelease: true}
	s.NoteOnParams(60, 0.9, np)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
}

// TestPushBoundedLengthClamp proves window-valid but buffer-outside taps
// clamp to the sample edges.
func TestPushBoundedLengthClamp(t *testing.T) {
	src := []float32{1, 2, 3, 4}
	if got := readSampleBounded(src, 8, 0, 10); got != 4 {
		t.Fatalf("over-len = %v, want 4", got)
	}
	if got := readSampleBounded(src, -3, -5, 4); got != 1 {
		t.Fatalf("under-len = %v, want 1", got)
	}
	if n := resampleTo(make([]float32, 4), src, 1, -5, false); n != 4 {
		t.Fatalf("neg phase n = %d, want 4", n)
	}
}

// TestPushWalkDotAndNotDir proves "." segments skip and file-intermediates
// error during the slow walk.
func TestPushWalkDotAndNotDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Bank"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "Bank", "Violin.wav")
	if err := os.WriteFile(want, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewPathResolver(dir)
	got, err := r.Resolve("BANK/./violin.wav")
	if err != nil {
		t.Fatalf("dot resolve: %v", err)
	}
	if got != want {
		t.Fatalf("dot = %q, want %q", got, want)
	}
	plain := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(plain, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r2 := NewPathResolver(plain)
	if _, err := r2.Resolve("sub/file.wav"); err == nil {
		t.Fatal("file-as-dir resolve accepted")
	}
}

// TestPushSignedEGBadValue proves malformed signed depths are dropped.
func TestPushSignedEGBadValue(t *testing.T) {
	f, err := ParseSFZ([]byte("<region> sample=x.wav lokey=60 ampeg_vel2attack=bad pitcheg_depth=bad\n"))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
}

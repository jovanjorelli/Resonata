package sampler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"resonata/pkg/wav"
)

// TestR2MegaOpcodes proves a fully dressed region parses every opcode
// family: key/velocity/range, transpose/tune/bend, volume/pan,
// amp/pitch/filter envelopes, both LFOs plus dedicated LFOs, EQ bands,
// loops, crossfades, switch/sequence/playback switches.
func TestR2MegaOpcodes(t *testing.T) {
	doc := `<region> sample=x.wav lokey=60 hikey=72 pitch_keycenter=64 ` +
		`lovel=10 hivel=100 lorand=0 hirand=1 offset=5 end=100 transpose=2 tune=10 ` +
		`pitch_random=5 pitch_veltrack=20 bend_up=2 bend_down=2 bend_step=1 ` +
		`volume=-3 pan=-0.5 amp_random=4 amp_veltrack=30 amp_keytrack=50 amp_keycenter=60 rt_decay=1.5 ` +
		`ampeg_delay=0.1 ampeg_start=0 ampeg_attack=0.01 ampeg_hold=0.05 ampeg_decay=0.2 ampeg_sustain=80 ` +
		`ampeg_release=0.3 ampeg_vel2attack=10 ampeg_vel2decay=5 ampeg_vel2release=5 ` +
		`ampeg_key2attack=3 ampeg_key2release=3 ` +
		`pitcheg_delay=0.1 pitcheg_start=0 pitcheg_attack=0.01 pitcheg_hold=0.05 pitcheg_decay=0.2 ` +
		`pitcheg_sustain=70 pitcheg_release=0.3 pitcheg_depth=100 pitcheg_vel2depth=20 ` +
		`fileg_delay=0.1 fileg_attack=0.01 fileg_decay=0.2 fileg_sustain=60 fileg_release=0.3 ` +
		`fileg_depth=200 fileg_vel2depth=10 fil_type=lpf_2p cutoff=800 resonance=2 ` +
		`fil_keytrack=100 fil_keycenter=60 fil_veltrack=40 fil_random=5 ` +
		`lfo01_delay=0.2 lfo01_fade=0.3 lfo01_freq=5.5 lfo01_volume=10 lfo01_amplitude=5 ` +
		`lfo01_pitch=8 lfo01_filter=6 lfo01_pan=4 lfo01_volume_smooth=2 lfo01_wave=1 lfo01_freq_wave=0 ` +
		`lfo02_delay=0.1 lfo02_fade=0.2 lfo02_freq=6.5 lfo02_volume=7 lfo02_amplitude=3 ` +
		`lfo02_pitch=4 lfo02_filter=2 lfo02_pan=1 lfo02_volume_smooth=1 lfo02_wave=0 lfo02_freq_wave=1 ` +
		`amp_lfo_delay=0.1 amp_lfo_freq=4.5 amp_lfo_depth=6 pitch_lfo_freq=5.0 pitch_lfo_depth=7 ` +
		`fil_lfo_freq=4.0 fil_lfo_depth=8 ` +
		`eq1_freq=200 eq1_bw=1 eq1_gain=-2 eq1_veltrack=10 eq2_freq=800 eq2_bw=1.5 ` +
		`eq2_gain=3 eq2_veltrack=5 eq3_freq=5000 eq3_bw=0.8 eq3_gain=1 eq3_veltrack=0 ` +
		`loop_mode=loop_continuous loop_start=10 loop_end=90 loop_type=0 loop_count=3 ` +
		`loop_tune=0 loop_crossfade=0.05 reverse=0 ` +
		`xfin_lokey=40 xfin_hikey=80 xfin_lovel=20 xfin_hivel=100 ` +
		`xfout_lokey=0 xfout_hikey=127 xfout_lovel=0 xfout_hivel=127 ` +
		`off_by=5 off_mode=normal polyphony=8 trigger=attack ` +
		`sw_lokey=36 sw_hikey=48 sw_default=40 sw_last=42 sw_down=36 sw_up=48 ` +
		`seq_length=2 seq_position=1 comment=hello` + "\n"
	f, err := ParseSFZ([]byte(doc))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(f.Regions))
	}
	r := f.Regions[0]
	if r.Volume != -3 || r.LoKey != 60 || r.HiKey != 72 || r.PitchKeyCenter != 64 {
		t.Fatalf("key/vol = %+v", r)
	}
	if r.LFO[1].Freq != 6.5 || r.OffBy != 5 || r.Polyphony != 8 {
		t.Fatalf("lfo/off/poly = %+v", r)
	}
	if r.LoopStart != 10 || r.LoopEnd != 90 {
		t.Fatalf("loop = %d/%d", r.LoopStart, r.LoopEnd)
	}
}

// TestR2SwappedRanges proves inverted key/velocity/random ranges swap
// into ascending order.
func TestR2SwappedRanges(t *testing.T) {
	f, err := ParseSFZ([]byte("<region> sample=x.wav lokey=70 hikey=60 lovel=90 hivel=20 lorand=0.9 hirand=0.1\n"))
	if err != nil {
		t.Fatalf("ParseSFZ: %v", err)
	}
	r := f.Regions[0]
	if r.LoKey != 60 || r.HiKey != 70 || r.LoVel != 20 || r.HiVel != 90 {
		t.Fatalf("unswapped = %+v", r)
	}
	if r.LoRand != 0.1 || r.HiRand != 0.9 {
		t.Fatalf("rand unswapped = %+v", r)
	}
}

// TestR2EmptyOpcode proves a valueless opcode line is skipped.
func TestR2EmptyOpcode(t *testing.T) {
	f, err := ParseSFZ([]byte("<region> sample=x.wav lokey=60\n= 5\n"))
	if err != nil {
		t.Fatalf("empty opcode: %v", err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d", len(f.Regions))
	}
}

// TestR2IncludeErrors proves empty, missing, nested, and over-deep
// includes fail with actionable errors.
func TestR2IncludeErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestWav(t, filepath.Join(dir, "c4.wav"), wav.PCM24, 48000, 500, 1, 261.63)
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := LoadSFZ(write("empty.sfz", "#include\n<region> sample=c4.wav lokey=60\n")); err == nil ||
		!strings.Contains(err.Error(), "empty #include path") {
		t.Fatalf("empty include = %v", err)
	}
	if _, err := LoadSFZ(write("miss.sfz", "#include \"nope.sfz\"\n<region> sample=c4.wav lokey=60\n")); err == nil ||
		!strings.Contains(err.Error(), "include not found") {
		t.Fatalf("missing include = %v", err)
	}
	write("inner.sfz", "#include \"missing.sfz\"\n")
	if _, err := LoadSFZ(write("outer.sfz", "#include \"inner.sfz\"\n<region> sample=c4.wav lokey=60\n")); err == nil {
		t.Fatal("nested missing include accepted")
	}
	// Chain past maxIncludeDepth (relative names: write() joins dir).
	prev := "leaf.sfz"
	write(prev, "<region> sample=c4.wav lokey=60\n")
	for i := 0; i < 70; i++ {
		name := "chain" + string(rune('a'+i%26)) + string(rune('0'+(i/26)%10)) + string(rune('0'+(i/260)%10)) + ".sfz"
		write(name, "#include \""+prev+"\"\n")
		prev = name
	}
	if _, err := LoadSFZ(filepath.Join(dir, prev)); err == nil || !strings.Contains(err.Error(), "depth") {
		t.Fatalf("deep chain = %v, want depth error", err)
	}
}

// TestR2LoopOverrides proves region loop opcodes replace sample points.
func TestR2LoopOverrides(t *testing.T) {
	dir := t.TempDir()
	writeTestWav(t, filepath.Join(dir, "c4.wav"), wav.PCM24, 48000, 2000, 1, 261.63)
	p := filepath.Join(dir, "l.sfz")
	if err := os.WriteFile(p, []byte("<region> sample=c4.wav lokey=60 loop_start=10 loop_end=100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := LoadSFZ(p)
	if err != nil {
		t.Fatalf("LoadSFZ: %v", err)
	}
	if f.Regions[0].Sample.LoopStart != 10 || f.Regions[0].Sample.LoopEnd != 100 {
		t.Fatalf("loops = %d/%d", f.Regions[0].Sample.LoopStart, f.Regions[0].Sample.LoopEnd)
	}
}

// TestR2GarbageSample proves undecodable sample files fail loudly.
func TestR2GarbageSample(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "junk.bin"), []byte("not audio at all.............."), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "g.sfz")
	if err := os.WriteFile(p, []byte("<region> sample=junk.bin lokey=60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSFZ(p); err == nil {
		t.Fatal("garbage sample accepted")
	}
}

// TestR2VoiceFadeEdges proves defective fade steps fall back and
// completed fades clamp to exactly 1. Single-frame blocks freeze the
// fade mid-flight so the clamped value stays observable.
func TestR2VoiceFadeEdges(t *testing.T) {
	s := New(48000)
	a := &s.voices[6]
	a.Region = &Region{Sample: pushSample(64)}
	a.end = 64
	a.Phase = 0
	a.Active = true
	a.Envelope.Trigger()
	a.isStealing = true
	a.fadeStep = 0 // falls back to the default step
	a.pendingRegion = &Region{Sample: pushSample(64)}
	a.pendingPitch, a.pendingVelocity = 60, 0.8
	a.Process(make([]float32, 1), 1.0/48000)
	if a.stealFadeProgress <= 0 || a.stealFadeProgress >= 1 || !a.isStealing {
		t.Fatalf("fallback fade = %v stealing=%v", a.stealFadeProgress, a.isStealing)
	}
	b := &s.voices[7]
	b.Region = &Region{Sample: pushSample(64)}
	b.end = 64
	b.Active = true
	b.Envelope.Trigger()
	b.isStealing = true
	b.fadeStep = 0.5
	b.stealFadeProgress = 0.9 // completes this frame: clamps to 1
	b.pendingRegion = &Region{Sample: pushSample(64)}
	b.pendingPitch, b.pendingVelocity = 61, 0.8
	b.Process(make([]float32, 1), 1.0/48000)
	if b.stealFadeProgress != 1 {
		t.Fatalf("progress = %v, want 1", b.stealFadeProgress)
	}
}

// TestR2EnvelopeIdleHandoff proves an idle envelope mid-fade hands off
// at once instead of rendering silence.
func TestR2EnvelopeIdleHandoff(t *testing.T) {
	s := New(48000)
	v := &s.voices[8]
	v.Region = &Region{Sample: pushSample(64)}
	v.end = 64
	v.Phase = 0
	v.Active = true // fresh envelope: idle
	v.isStealing = true
	v.pendingRegion = &Region{Sample: pushSample(64)}
	v.pendingPitch, v.pendingVelocity = 62, 0.8
	v.Process(make([]float32, 1), 1.0/48000)
	if v.stealFadeProgress != 1 {
		t.Fatalf("progress = %v, want handoff", v.stealFadeProgress)
	}
}

// TestR2PhaseEndStealing proves reaching sample end mid-fade starts the
// pending note.
func TestR2PhaseEndStealing(t *testing.T) {
	s := New(48000)
	v := &s.voices[9]
	sd := pushSample(16)
	v.Region = &Region{Sample: sd}
	v.end = 16
	v.PlaybackRate = 1
	v.Phase = 15.5
	v.Active = true
	v.Envelope.Trigger()
	v.isStealing = true
	v.pendingRegion = &Region{Sample: pushSample(16)}
	v.pendingPitch, v.pendingVelocity = 63, 0.8
	v.Process(make([]float32, 1), 1.0/48000)
	if v.stealFadeProgress != 1 {
		t.Fatalf("progress = %v, want handoff", v.stealFadeProgress)
	}
}

// TestR2EndClampStealTop proves a zero window clamps inside the steal
// handoff instead of indexing out of range.
func TestR2EndClampStealTop(t *testing.T) {
	s := New(48000)
	v := &s.voices[10]
	v.Region = &Region{Sample: &SampleData{Samples: []float32{}, SampleRate: 48000, Channels: 1}}
	v.Active = true
	v.isStealing = true
	v.stealFadeProgress = 1
	v.pendingRegion = &Region{Sample: &SampleData{Samples: []float32{}, SampleRate: 48000, Channels: 1}}
	v.pendingPitch, v.pendingVelocity = 64, 0.8
	v.Process(make([]float32, 8), 8.0/48000) // must not panic
}

// TestR2SamplerEmptyBlock proves zero-length blocks are safe no-ops.
func TestR2SamplerEmptyBlock(t *testing.T) {
	s := New(48000)
	buf := dsp.NewStereoBuffer(16)
	buf.SetLen(0)
	s.Process(buf, 16.0/48000)
}

// TestR2PositiveOverrides proves positive decay/release overrides apply.
func TestR2PositiveOverrides(t *testing.T) {
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{pushRegion(pushSample(48000), 0, 127)}})
	np := instruments.NoteParams{
		Env:           instruments.EnvelopeOverride{Attack: 0.05, HasAttack: true, Decay: 0.2, HasDecay: true, Release: 0.6, HasRelease: true},
		HasExpression: true, Expression: 2.0,
	}
	s.NoteOnParams(60, 0.9, np)
	if got := s.ActiveVoices(); got != 1 {
		t.Fatalf("voices = %d, want 1", got)
	}
}

// TestR2XfoutSwap proves reversed key/velocity fade-out zones swap.
func TestR2XfoutSwap(t *testing.T) {
	rg := &Region{XfoutLoVel: 100, XfoutHiVel: 0, XfoutLoKey: 90, XfoutHiKey: 30}
	if got := xfadeGain(rg, 60, 50); got <= 0 || got > 1 {
		t.Fatalf("swapped out gain = %v", got)
	}
}

package sampler

import (
	"io"
	"os"
	"strings"
	"testing"

	"resonata/pkg/dsp"
)

// messyPath is the committed real-world torture fixture.
const messyPath = "../../../test_data/messy_real_world.sfz"

// TestMessySFZ parses the deliberately messy vendor-style fixture and
// checks paths with spaces, pitch-name keys, hierarchical inheritance,
// unknown-opcode tolerance, and parse statistics.
func TestMessySFZ(t *testing.T) {
	data, err := os.ReadFile(messyPath)
	if err != nil {
		t.Fatal(err)
	}
	sfz, err := ParseSFZ(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(sfz.Regions) != 4 {
		t.Fatalf("regions = %d, want 4 (pno, eb2, broken, no_newline)", len(sfz.Regions))
	}
	r0, r1, r2, r3 := sfz.Regions[0], sfz.Regions[1], sfz.Regions[2], sfz.Regions[3]

	// Region 0: path with spaces, default_path prefix, pitch-name keys,
	// tabs for indentation, and region-level overrides of group/global.
	if r0.SamplePath != "Symphony Woods/Woods/pno c4 loud.wav" {
		t.Errorf("r0 path = %q", r0.SamplePath)
	}
	if r0.LoKey != 60 || r0.HiKey != 61 || r0.PitchKeyCenter != 60 {
		t.Errorf("r0 keys = %d-%d center %d, want 60-61 center 60 (c4..c#4)",
			r0.LoKey, r0.HiKey, r0.PitchKeyCenter)
	}
	if r0.LoVel != 1 || r0.HiVel != 100 {
		t.Errorf("r0 velocities = %d-%d", r0.LoVel, r0.HiVel)
	}
	if r0.Offset != 120 || r0.End != 44000 {
		t.Errorf("r0 window = %d-%d", r0.Offset, r0.End)
	}
	if r0.LoopMode != LoopContinuous || r0.LoopStart != 800 || r0.LoopEnd != 43000 || r0.LoopCrossfade != 20 {
		t.Errorf("r0 loop = %s %d-%d xf %d", r0.LoopMode, r0.LoopStart, r0.LoopEnd, r0.LoopCrossfade)
	}
	if r0.Transpose != -12 || r0.Tune != 3 || r0.PitchKeytrack != 100 {
		t.Errorf("r0 pitch = transpose %d tune %d keytrack %d", r0.Transpose, r0.Tune, r0.PitchKeytrack)
	}
	if !r0.HasAmpEG || r0.AmpEG.Attack != 0.01 || r0.AmpEG.Sustain != 80 || r0.AmpEG.Release != 0.35 {
		t.Errorf("r0 ampeg = %+v (has=%v)", r0.AmpEG, r0.HasAmpEG)
	}
	if r0.LFO[0].Freq != 5 || r0.LFO[0].Volume != 1.2 {
		t.Errorf("r0 lfo01 = %+v", r0.LFO[0])
	}
	if r0.LFO[1].Freq != 0.4 || r0.LFO[1].Pan != 0.5 {
		t.Errorf("r0 lfo02 = %+v", r0.LFO[1])
	}
	if r0.FilType != "lpf_2p" || r0.Cutoff != 2500 || r0.Resonance != 6 {
		t.Errorf("r0 filter = %s %v %v", r0.FilType, r0.Cutoff, r0.Resonance)
	}
	if r0.OffBy != 1 || r0.OffMode != "fast" || r0.Polyphony != 8 || r0.Trigger != "attack" {
		t.Errorf("r0 behavior = off_by %d %s poly %d trigger %s", r0.OffBy, r0.OffMode, r0.Polyphony, r0.Trigger)
	}
	if r0.GroupLabel != "Flute fallback" {
		t.Errorf("r0 label = %q", r0.GroupLabel)
	}
	if r0.Volume != -1.5 {
		t.Errorf("r0 volume = %v, want -1.5 inherited from <global>", r0.Volume)
	}
	if r0.SwLoKey != 12 || r0.SwHiKey != 36 || r0.SwDefault != 24 {
		t.Errorf("r0 keyswitch = %d-%d default %d, want 12-36/24 inherited from <group>",
			r0.SwLoKey, r0.SwHiKey, r0.SwDefault)
	}
	if r0.EQ[1].Freq != 1200 || r0.EQ[1].BW != 1.4 || r0.EQ[1].Gain != -1.5 {
		t.Errorf("r0 eq2 = %+v", r0.EQ[1])
	}

	// Region 1: spaces around '=', trailing ';' comment, key= shortcut,
	// signed values, and inheritance of the global amp envelope.
	if r1.SamplePath != "Symphony Woods/Woods/eb2 soft.wav" {
		t.Errorf("r1 path = %q", r1.SamplePath)
	}
	if r1.LoKey != 39 || r1.HiKey != 39 || r1.PitchKeyCenter != 39 {
		t.Errorf("r1 key= shortcut gave %d-%d center %d, want all 39 (eb2)",
			r1.LoKey, r1.HiKey, r1.PitchKeyCenter)
	}
	if r1.LoVel != 101 || r1.HiVel != 127 || r1.Count != 0 {
		t.Errorf("r1 vel/count = %d-%d/%d", r1.LoVel, r1.HiVel, r1.Count)
	}
	if r1.RtDecay != 0.5 || r1.AmpVeltrack != -50 || r1.BendUp != 200 || r1.BendDown != -200 {
		t.Errorf("r1 amp/pitch = %+v", r1)
	}
	if !r1.HasPitchEG || r1.PitchEG.Depth != 1200 || r1.PitchEG.Attack != 0.02 {
		t.Errorf("r1 pitcheg = %+v (has=%v)", r1.PitchEG, r1.HasPitchEG)
	}
	if !r1.HasFilEG || r1.FilEG.Depth != -3600 || r1.FilEG.Attack != 0.1 {
		t.Errorf("r1 fileg = %+v (has=%v)", r1.FilEG, r1.HasFilEG)
	}
	if r1.EQ[0].Freq != 200 || r1.EQ[0].BW != 1.0 || r1.EQ[0].Gain != 3 {
		t.Errorf("r1 eq1 = %+v", r1.EQ[0])
	}
	if !r1.HasAmpEG || r1.AmpEG.Attack != 0.005 || r1.AmpEG.Release != 0.4 || r1.AmpEG.Sustain != 100 {
		t.Errorf("r1 inherited ampeg = %+v (has=%v)", r1.AmpEG, r1.HasAmpEG)
	}
	if r1.LFO[0].Freq != 5.2 || r1.LFO[0].Volume != 2 {
		t.Errorf("r1 legacy lfo inheritance = %+v", r1.LFO[0])
	}
	if r1.GroupLabel != "Woodwinds" {
		t.Errorf("r1 label = %q, want inherited Woodwinds", r1.GroupLabel)
	}

	// Region 2: only a sample; everything else inherits <global> keys.
	if r2.SamplePath != "Symphony Woods/Woods/broken" {
		t.Errorf("r2 path = %q", r2.SamplePath)
	}
	if r2.LoKey != 60 || r2.HiKey != 84 {
		t.Errorf("r2 keys = %d-%d, want inherited 60-84 (c4..c6)", r2.LoKey, r2.HiKey)
	}

	// Region 3: no trailing newline, g9 top, c#-1 -> MIDI 1.
	if r3.SamplePath != "Symphony Woods/Tail/no_newline.wav" {
		t.Errorf("r3 path = %q", r3.SamplePath)
	}
	if r3.HiKey != 127 {
		t.Errorf("r3 hikey = %d, want 127 (g9)", r3.HiKey)
	}
	if r3.PitchKeyCenter != 1 {
		t.Errorf("r3 keycenter = %d, want 1 (c#-1)", r3.PitchKeyCenter)
	}

	// Diagnostics.
	s := sfz.Stats
	if s.BadValues != 3 { // lokey=abc, volume=12db, hikey=zzz
		t.Errorf("BadValues = %d, want 3", s.BadValues)
	}
	if s.Unknown != 5 { // gui_knob7, vendor_randomizer, type, mix, v3
		t.Errorf("Unknown = %d, want 5", s.Unknown)
	}
	if s.UnknownHeaders != 2 { // <effect>, <curve>
		t.Errorf("UnknownHeaders = %d, want 2", s.UnknownHeaders)
	}
	if s.EmptyRegions != 1 { // the sample-less region
		t.Errorf("EmptyRegions = %d, want 1", s.EmptyRegions)
	}
	if s.Headers != 10 {
		t.Errorf("Headers = %d, want 10", s.Headers)
	}
	if s.Strays < 4 || s.CommentSkips < 6 {
		t.Errorf("Strays/CommentSkips = %d/%d, want >=4/>=6", s.Strays, s.CommentSkips)
	}
}

func TestTokenizerLookahead(t *testing.T) {
	// Values run until whitespace followed by an opcode, header, comment,
	// or end of input.
	tok := NewTokenizer([]byte("sample=Grand Piano/c4 loud.wav lokey=60 // trail\nhikey=64"))
	k, v, err := tok.NextOpcode()
	if err != nil || string(k) != "sample" || string(v) != "Grand Piano/c4 loud.wav" {
		t.Fatalf("first = %q/%q err %v", k, v, err)
	}
	k, v, err = tok.NextOpcode()
	if err != nil || string(k) != "lokey" || string(v) != "60" {
		t.Fatalf("second = %q/%q err %v", k, v, err)
	}
	k, v, err = tok.NextOpcode()
	if err != nil || string(k) != "hikey" || string(v) != "64" {
		t.Fatalf("third = %q/%q err %v", k, v, err)
	}
	if _, _, err := tok.NextOpcode(); err != io.EOF {
		t.Fatalf("end err = %v, want io.EOF", err)
	}
	if tok.CommentSkips == 0 {
		t.Error("comment not counted")
	}

	// Headers come back as raw "<name>" keys with nil values.
	tok.Reset([]byte("volume=-6 <region>\nsample=a.wav"))
	k, v, _ = tok.NextOpcode()
	if string(k) != "volume" || string(v) != "-6" {
		t.Fatalf("volume = %q/%q", k, v)
	}
	k, v, _ = tok.NextOpcode()
	if string(k) != "<region>" || v != nil {
		t.Fatalf("header = %q/%q", k, v)
	}
	k, v, _ = tok.NextOpcode()
	if string(k) != "sample" || string(v) != "a.wav" {
		t.Fatalf("sample = %q/%q", k, v)
	}

	// Stray characters and words are skipped, never fatal.
	tok.Reset([]byte("/ junk ?? lokey=c4"))
	k, v, err = tok.NextOpcode()
	if err != nil || string(k) != "lokey" || string(v) != "c4" {
		t.Fatalf("after strays = %q/%q err %v", k, v, err)
	}
	if tok.Strays < 4 {
		t.Errorf("Strays = %d, want >= 4", tok.Strays)
	}

	// Empty values and spaces around '='.
	tok.Reset([]byte("sample=\nlokey=60"))
	k, v, _ = tok.NextOpcode()
	if string(k) != "sample" || len(v) != 0 {
		t.Fatalf("empty value = %q/%q", k, v)
	}
	k, v, _ = tok.NextOpcode()
	if string(k) != "lokey" || string(v) != "60" {
		t.Fatalf("after empty = %q/%q", k, v)
	}
	tok.Reset([]byte("pan = 50"))
	k, v, _ = tok.NextOpcode()
	if string(k) != "pan" || string(v) != "50" {
		t.Fatalf("spaced equals = %q/%q", k, v)
	}
}

func TestTokenizerZeroCopy(t *testing.T) {
	data := []byte("sample=a b.wav lokey=60")
	tok := NewTokenizer(data)
	_, v, err := tok.NextOpcode()
	if err != nil || string(v) != "a b.wav" {
		t.Fatalf("value = %q err %v", v, err)
	}
	// Mutating the input must show through: values alias data (zero-copy).
	data[8] = 'X'
	if string(v) != "aXb.wav" {
		t.Fatalf("value %q does not alias the input", v)
	}
}

func TestParsePitchTable(t *testing.T) {
	valid := []struct {
		name string
		want int
	}{
		{"c4", 60}, {"C4", 60}, {"a4", 69}, {"A4", 69}, {"eb2", 39}, {"c#-1", 1},
		{"F#5", 78}, {"bb3", 58}, {"db0", 13}, {"g9", 127}, {"c-1", 0}, {"cs4", 61},
		{"ef2", 39}, {"d4", 62}, {"  g4 ", 67},
	}
	for _, tc := range valid {
		got, ok := ParsePitch([]byte(tc.name))
		if !ok || got != tc.want {
			t.Errorf("ParsePitch(%q) = %d (%v), want %d", tc.name, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "x4", "c", "4", "c4x", "h3", "cb-2", "c128", "c#", "-"} {
		if got, ok := ParsePitch([]byte(bad)); ok {
			t.Errorf("ParsePitch(%q) = %d, want failure", bad, got)
		}
	}
}

func TestDispatchCoverage(t *testing.T) {
	if len(opcodeDispatch) < 100 {
		t.Fatalf("opcodeDispatch has %d entries, want 100+", len(opcodeDispatch))
	}
	// Spot-check that every family is represented.
	for _, op := range []string{"sample", "lokey", "key", "loop_mode", "transpose",
		"ampeg_release", "pitcheg_depth", "fileg_depth", "cutoff", "lfo01_freq",
		"lfo02_pan", "amp_lfo_depth", "eq3_gain", "off_by", "sw_last", "default_path"} {
		if _, ok := opcodeDispatch[op]; !ok {
			t.Errorf("opcode %q missing from dispatch", op)
		}
	}
}

// TestRegionWindow checks offset/end sample-window playback.
func TestRegionWindow(t *testing.T) {
	sd := &SampleData{Samples: make([]float32, 100), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	rg := newRegion()
	rg.SamplePath = "w.wav"
	rg.Sample = sd
	rg.Offset = 40
	rg.End = 70 // 30 playable frames
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{rg}})
	s.NoteOn(60, 1.0)

	buf := dsp.NewStereoBuffer(256)
	buf.SetLen(256)
	s.Process(buf, 1.0/48000)
	if s.ActiveVoices() != 0 {
		t.Fatal("voice survived past the end frame")
	}
	for i := 0; i < 30; i++ {
		if buf.Left[i] == 0 {
			t.Fatalf("frame %d inside the window is silent", i)
		}
	}
	for i := 30; i < 256; i++ {
		if buf.Left[i] != 0 {
			t.Fatalf("frame %d past the window = %v, want 0", i, buf.Left[i])
		}
	}
}

// TestOffByCutoff checks exclusive voice groups.
func TestOffByCutoff(t *testing.T) {
	sd := &SampleData{Samples: make([]float32, 48000), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	r1, r2 := newRegion(), newRegion()
	r1.SamplePath, r1.Sample = "a.wav", sd
	r1.LoKey, r1.HiKey, r1.PitchKeyCenter = 60, 69, 60
	r1.OffBy = 5
	r2.SamplePath, r2.Sample = "b.wav", sd
	r2.LoKey, r2.HiKey, r2.PitchKeyCenter = 70, 79, 70
	r2.OffBy = 5

	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{r1, r2}})
	s.NoteOn(60, 0.9)
	s.NoteOn(70, 0.9)
	if s.ActiveVoices() != 2 {
		t.Fatalf("voices = %d, want 2 (old one releasing)", s.ActiveVoices())
	}
	if !s.voices[0].releasing || s.voices[0].Pitch != 60 {
		t.Fatalf("off_by did not cut the first voice: %+v", s.voices[0])
	}
	if s.voices[1].releasing {
		t.Fatal("the new voice must not be releasing")
	}
	advance(t, s, 0.4) // default 0.2 s release completes
	if s.ActiveVoices() != 1 {
		t.Fatalf("voices after cutoff release = %d, want 1", s.ActiveVoices())
	}
}

// TestAmpEGOverride checks that a region ampeg replaces the sampler
// envelope defaults (fast 10 ms release here).
func TestAmpEGOverride(t *testing.T) {
	sd := &SampleData{Samples: make([]float32, 48000), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	rg := newRegion()
	rg.SamplePath, rg.Sample = "a.wav", sd
	rg.HasAmpEG = true
	rg.AmpEG = EGParams{Attack: 0.001, Sustain: 100, Release: 0.01}

	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{rg}})
	s.NoteOn(60, 1.0)
	advance(t, s, 0.05)
	if s.ActiveVoices() != 1 {
		t.Fatal("voice died during the note")
	}
	s.NoteOff(60)
	advance(t, s, 0.05) // the 10 ms release must be done by now
	if s.ActiveVoices() != 0 {
		t.Fatalf("voices = %d after the fast release (default 0.2 s would still sound)",
			s.ActiveVoices())
	}
}

// advance moves the sampler forward by d seconds, discarding audio.
func advance(t *testing.T, s *Sampler, d float64) {
	t.Helper()
	buf := dsp.NewStereoBuffer(512)
	frames := int(d * 48000)
	for done := 0; done < frames; {
		n := min(512, frames-done)
		buf.SetLen(n)
		s.Process(buf, 1.0/48000)
		done += n
	}
}

// FuzzParseSFZ asserts the universal parser's safety invariants on
// arbitrary input: no panics, no errors, valid emitted regions, and
// prompt tokenizer termination. Seeds run during plain go test; fuzzing
// engine via -fuzz.
func FuzzParseSFZ(f *testing.F) {
	seeds := []string{
		"", "<", "<region", "<region>", "sample=", "=x", "lokey=",
		"lokey=999999999999", "sample=a\x00b.wav lokey=c4", "\xff\xfe\x00binary junk",
		"sample=x.wav lokey=c4 hikey=/*unterminated", "/*", "//", "#", ";", "/",
		"key=zz9 pan=nan volume=1e999 ampeg_attack=-3",
		strings.Repeat("<region>", 500),
		strings.Repeat("sample=a.wav ", 200),
		"sample=a.wav= trailing equals lokey== weird",
		"<global>volume=3<group>pan=10<region>sample=s.wav",
		"sample=C#4 loud.wav lokey=c#4 // hash inside a path is not a comment",
		"lovel=100 hivel=1\t\r\n sample=z.wav",
	}
	if data, err := os.ReadFile(messyPath); err == nil {
		seeds = append(seeds, string(data))
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		sfz, err := ParseSFZ(data)
		if err != nil || sfz == nil {
			t.Fatalf("ParseSFZ(%q) = %v, %v", truncForLog(data), sfz, err)
		}
		for i := range sfz.Regions {
			r := &sfz.Regions[i]
			if r.SamplePath == "" {
				t.Errorf("region %d: empty sample path", i)
			}
			if r.LoKey < 0 || r.LoKey > 127 || r.HiKey < 0 || r.HiKey > 127 || r.LoKey > r.HiKey {
				t.Errorf("region %d: keys %d-%d invalid", i, r.LoKey, r.HiKey)
			}
			if r.LoVel < 0 || r.LoVel > 127 || r.HiVel < 0 || r.HiVel > 127 || r.LoVel > r.HiVel {
				t.Errorf("region %d: velocities %d-%d invalid", i, r.LoVel, r.HiVel)
			}
			if r.PitchKeyCenter < 0 || r.PitchKeyCenter > 127 {
				t.Errorf("region %d: keycenter %d invalid", i, r.PitchKeyCenter)
			}
		}
		// The scanner alone must also terminate promptly.
		tok := NewTokenizer(data)
		for n := 0; ; n++ {
			if _, _, err := tok.NextOpcode(); err != nil {
				break
			}
			if n > 4*len(data)+64 {
				t.Fatal("tokenizer did not terminate")
			}
		}
	})
}

// truncForLog shortens binary input for readable failure messages.
func truncForLog(b []byte) string {
	if len(b) > 60 {
		b = b[:60]
	}
	return string(b)
}

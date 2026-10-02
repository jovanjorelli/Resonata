package sampler

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"resonata/pkg/wav"
)

// specSFZ is the simplified example from the sampler specification.
const specSFZ = "\nloop_mode=no_loop\n\n" +
	" sample=c4.wav lokey=60 hikey=64 pitch_keycenter=60\n" +
	" sample=c5.wav lokey=65 hikey=69 pitch_keycenter=67\n"

func TestParseSFZSpecExample(t *testing.T) {
	f, err := ParseSFZ([]byte(specSFZ))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 2 {
		t.Fatalf("regions = %d, want 2", len(f.Regions))
	}
	r0, r1 := f.Regions[0], f.Regions[1]
	if r0.SamplePath != "c4.wav" || r0.LoKey != 60 || r0.HiKey != 64 || r0.PitchKeyCenter != 60 {
		t.Errorf("region 0 = %+v", r0)
	}
	if r1.SamplePath != "c5.wav" || r1.LoKey != 65 || r1.HiKey != 69 || r1.PitchKeyCenter != 67 {
		t.Errorf("region 1 = %+v", r1)
	}
	for i, r := range f.Regions {
		if r.LoopMode != LoopNoLoop {
			t.Errorf("region %d: loop_mode = %q (group default not applied)", i, r.LoopMode)
		}
		if r.LoVel != 0 || r.HiVel != 127 || r.Volume != 0 || r.Pan != 0 {
			t.Errorf("region %d: defaults wrong: %+v", i, r)
		}
		if r.LoopStart != -1 || r.LoopEnd != -1 {
			t.Errorf("region %d: unset loop points = %d/%d, want -1", i, r.LoopStart, r.LoopEnd)
		}
	}
}

func TestParseSFZGroupRegion(t *testing.T) {
	doc := `// orchestral sketch
<group>
volume=-6 pan=-50
lovel=0 hivel=63
loop_mode=loop_continuous loop_start=100 loop_end=2000
<region>
sample=soft_a.wav lokey=57 hikey=64 pitch_keycenter=60
<region>
sample=soft_e.wav lokey=65 hikey=72

<group>
volume=0 pan=25
<region> sample=loud_a.wav lokey=57 hikey=72 pitch_keycenter=64 loop_mode=no_loop
`
	f, err := ParseSFZ([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 3 {
		t.Fatalf("regions = %d, want 3", len(f.Regions))
	}
	r0, r1, r2 := f.Regions[0], f.Regions[1], f.Regions[2]

	if r0.SamplePath != "soft_a.wav" || r0.LoKey != 57 || r0.HiKey != 64 || r0.PitchKeyCenter != 60 {
		t.Errorf("region 0 keys = %+v", r0)
	}
	if r0.Volume != -6 || r0.Pan != -0.5 || r0.LoVel != 0 || r0.HiVel != 63 {
		t.Errorf("region 0 group defaults = %+v", r0)
	}
	if r0.LoopMode != LoopContinuous || r0.LoopStart != 100 || r0.LoopEnd != 2000 {
		t.Errorf("region 0 loop = %+v", r0)
	}

	if r1.SamplePath != "soft_e.wav" || r1.LoKey != 65 || r1.HiKey != 72 {
		t.Errorf("region 1 keys = %+v", r1)
	}
	if r1.PitchKeyCenter != 60 || r1.HiVel != 63 || r1.Volume != -6 {
		t.Errorf("region 1 inheritance = %+v", r1)
	}

	// The second <group> resets defaults: hivel and loop points are back
	// to their built-in values.
	if r2.SamplePath != "loud_a.wav" || r2.PitchKeyCenter != 64 {
		t.Errorf("region 2 = %+v", r2)
	}
	if r2.Volume != 0 || r2.Pan != 0.25 || r2.LoVel != 0 || r2.HiVel != 127 {
		t.Errorf("region 2 reset defaults = %+v", r2)
	}
	if r2.LoopMode != LoopNoLoop || r2.LoopStart != -1 || r2.LoopEnd != -1 {
		t.Errorf("region 2 loop reset = %+v", r2)
	}
}

func TestParseSFZSamplePathSpaces(t *testing.T) {
	f, err := ParseSFZ([]byte("sample=Grand Piano/c4 loud.wav lokey=60 hikey=64\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 1 {
		t.Fatalf("regions = %d", len(f.Regions))
	}
	if f.Regions[0].SamplePath != "Grand Piano/c4 loud.wav" {
		t.Errorf("path = %q", f.Regions[0].SamplePath)
	}
	if f.Regions[0].LoKey != 60 || f.Regions[0].HiKey != 64 {
		t.Errorf("keys after spaced path = %+v", f.Regions[0])
	}
}

// TestParseSFZTolerance replaces strict-error behavior: the universal
// parser never fails on messy input, it drops what it cannot use and
// counts everything in Stats.
func TestParseSFZTolerance(t *testing.T) {
	// Malformed values are dropped; well-formed ones on the same line survive.
	sfz, err := ParseSFZ([]byte("sample=a.wav lokey=xx volume=loud pan=25"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sfz.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(sfz.Regions))
	}
	r := sfz.Regions[0]
	if r.LoKey != 0 {
		t.Errorf("bad lokey kept: %d, want default 0", r.LoKey)
	}
	if r.Volume != 0 {
		t.Errorf("bad volume kept: %v, want default 0", r.Volume)
	}
	if r.Pan != 0.25 {
		t.Errorf("pan = %v, want 0.25 (from percent)", r.Pan)
	}
	if sfz.Stats.BadValues != 2 {
		t.Errorf("BadValues = %d, want 2", sfz.Stats.BadValues)
	}

	// A region without a sample is discarded, not fatal.
	sfz, _ = ParseSFZ([]byte("<region>\nvolume=-3\n"))
	if len(sfz.Regions) != 0 || sfz.Stats.EmptyRegions != 1 {
		t.Errorf("sample-less region: %d regions, %+v", len(sfz.Regions), sfz.Stats)
	}

	// Unknown opcodes and headers are ignored without breaking the state.
	sfz, _ = ParseSFZ([]byte("<effect>\ntype=chorus\n<region>\nsample=a.wav\ngui_thing=1\n"))
	if len(sfz.Regions) != 1 {
		t.Fatalf("regions = %d, want 1", len(sfz.Regions))
	}
	if sfz.Stats.Unknown != 2 || sfz.Stats.UnknownHeaders != 1 {
		t.Errorf("stats = %+v", sfz.Stats)
	}

	// Stray tokens never stop the scan.
	sfz, _ = ParseSFZ([]byte("nonsense / ?? sample=a.wav lokey=60"))
	if len(sfz.Regions) != 1 || sfz.Regions[0].LoKey != 60 {
		t.Fatalf("after strays: %+v", sfz.Regions)
	}
	if sfz.Stats.Strays == 0 {
		t.Error("strays not counted")
	}

	// An empty sample value is dropped.
	sfz, _ = ParseSFZ([]byte("sample= lokey=60"))
	if len(sfz.Regions) != 0 {
		t.Fatalf("empty sample produced %d regions", len(sfz.Regions))
	}

	// Inverted key ranges are normalized on flush.
	sfz, _ = ParseSFZ([]byte("sample=a.wav lokey=80 hikey=40"))
	if len(sfz.Regions) != 1 || sfz.Regions[0].LoKey != 40 || sfz.Regions[0].HiKey != 80 {
		t.Fatalf("inverted keys not normalized: %+v", sfz.Regions)
	}
}

// writeTestWav renders a sine WAV with the pkg/wav writer for loader tests.
func writeTestWav(t *testing.T, path string, format wav.Format, sr, frames, ch int, freq float64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := wav.NewWriter(f, format, sr, ch)
	if err != nil {
		t.Fatal(err)
	}
	l := make([]float32, frames)
	r := make([]float32, frames)
	for i := range l {
		l[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sr)))
		r[i] = l[i]
	}
	if ch == 1 {
		if _, err := w.WriteFrames(l); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := w.WriteFrames(l, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 24-bit PCM mono round trip within quantization error.
	p24 := filepath.Join(dir, "s24.wav")
	writeTestWav(t, p24, wav.PCM24, 48000, 1000, 1, 440)
	info, out, err := wav.DecodeFile(p24)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != wav.PCM24 || info.SampleRate != 48000 || info.Channels != 1 ||
		info.BitsPerSample != 24 || info.Frames != 1000 || info.HasLoop {
		t.Fatalf("info = %+v", info)
	}
	for i, v := range out {
		want := float32(math.Sin(2 * math.Pi * 440 * float64(i) / 48000))
		if math.Abs(float64(v-want)) > 1e-6 {
			t.Fatalf("frame %d = %v, want ~%v", i, v, want)
		}
	}

	// 32-bit float stereo round trip is exact.
	pf := filepath.Join(dir, "sf.wav")
	writeTestWav(t, pf, wav.Float32, 44100, 500, 2, 523.25)
	info, out, err = wav.DecodeFile(pf)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != wav.Float32 || info.BitsPerSample != 32 || info.Channels != 2 || info.Frames != 500 {
		t.Fatalf("info = %+v", info)
	}
	if len(out) != 1000 {
		t.Fatalf("len = %d, want 1000", len(out))
	}
	for i := 0; i < 500; i++ {
		want := float32(math.Sin(2 * math.Pi * 523.25 * float64(i) / 44100))
		if out[i*2] != want || out[i*2+1] != want {
			t.Fatalf("frame %d = %v/%v, want %v", i, out[i*2], out[i*2+1], want)
		}
	}
}

func TestDecode16BitAndSmpl(t *testing.T) {
	// Hand-built 16-bit stereo file with a smpl chunk (loop frames 1..3).
	var buf []byte
	u16 := func(v uint16) { buf = binary.LittleEndian.AppendUint16(buf, v) }
	u32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	data := []int16{1000, -1000, 2000, -2000, 3000, -3000, 4000, -4000}
	buf = append(buf, "RIFF"...)
	u32(4 + 24 + 8 + uint32(len(data))*2 + 8 + 60)
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	u32(16)
	u16(1)
	u16(2)
	u32(44100)
	u32(44100 * 4)
	u16(4)
	u16(16)
	buf = append(buf, "data"...)
	u32(uint32(len(data)) * 2)
	for _, v := range data {
		u16(uint16(v))
	}
	buf = append(buf, "smpl"...)
	u32(60)
	for i := 0; i < 7; i++ {
		u32(0) // manufacturer..SMPTEOffset
	}
	u32(1) // cSampleLoops
	u32(0) // cbSamplerData
	u32(0)
	u32(0) // loop id, type
	u32(1)
	u32(3) // start=1, end=3
	u32(0)
	u32(0) // fraction, playCount

	path := filepath.Join(t.TempDir(), "m16.wav")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	info, out, err := wav.DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.FormatTag != wav.PCM24 || info.BitsPerSample != 16 || info.Channels != 2 || info.Frames != 4 {
		t.Fatalf("info = %+v", info)
	}
	if !info.HasLoop || info.LoopStart != 1 || info.LoopEnd != 3 {
		t.Fatalf("loop = %+v", info)
	}
	for i, want := range data {
		if got := out[i]; got != float32(want)/32768 {
			t.Fatalf("sample %d = %v, want %v", i, got, float32(want)/32768)
		}
	}
}

func TestLoadSFZ(t *testing.T) {
	dir := t.TempDir()
	writeTestWav(t, filepath.Join(dir, "c4.wav"), wav.PCM24, 48000, 2000, 1, 261.63)
	writeTestWav(t, filepath.Join(dir, "c5.wav"), wav.Float32, 44100, 1500, 2, 523.25)
	sfz := specSFZ + " sample=c4.wav lokey=70 hikey=72 pitch_keycenter=70\n"
	path := filepath.Join(dir, "p.sfz")
	if err := os.WriteFile(path, []byte(sfz), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := LoadSFZ(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 3 {
		t.Fatalf("regions = %d, want 3", len(f.Regions))
	}
	r0, r1, r2 := f.Regions[0], f.Regions[1], f.Regions[2]
	if r0.Sample == nil || r0.Sample.Frames() != 2000 || r0.Sample.SampleRate != 48000 || r0.Sample.Channels != 1 {
		t.Fatalf("region 0 sample = %+v", r0.Sample)
	}
	// Stereo float source: downmixed to mono frames, source layout kept.
	if r1.Sample == nil || r1.Sample.Frames() != 1500 || r1.Sample.Channels != 2 || r1.Sample.SampleRate != 44100 {
		t.Fatalf("region 1 sample = %+v", r1.Sample)
	}
	for i, v := range r1.Sample.Samples {
		want := float32(math.Sin(2 * math.Pi * 523.25 * float64(i) / 44100))
		if v != want {
			t.Fatalf("downmix frame %d = %v, want %v", i, v, want)
		}
	}
	// Regions sharing a file share one decoded SampleData.
	if r2.Sample != r0.Sample {
		t.Fatal("duplicate sample paths were not deduplicated")
	}

	// Missing sample file surfaces a helpful error.
	bad := filepath.Join(dir, "bad.sfz")
	if err := os.WriteFile(bad, []byte("sample=nope.wav lokey=60\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSFZ(bad); err == nil || !strings.Contains(err.Error(), "nope.wav") {
		t.Fatalf("missing sample error = %v", err)
	}
	if _, err := LoadSFZ(filepath.Join(dir, "missing.sfz")); err == nil {
		t.Fatal("missing SFZ file: want error")
	}
}

func TestRegionHelpers(t *testing.T) {
	rg := newRegion()
	rg.Sample = &SampleData{Samples: make([]float32, 10), SampleRate: 48000, Channels: 1}
	if !rg.Matches(60, 64) || rg.Matches(60, 64) != true {
		t.Error("default region should match everything")
	}
	rg.LoKey, rg.HiKey = 60, 64
	if rg.Matches(59, 64) || rg.Matches(65, 64) || !rg.Matches(62, 64) {
		t.Error("key range matching is wrong")
	}
	rg.LoVel, rg.HiVel = 32, 96
	if rg.Matches(62, 31) || rg.Matches(62, 97) || !rg.Matches(62, 64) {
		t.Error("velocity range matching is wrong")
	}
	rg.Volume = -6
	if g := rg.LinearGain(); math.Abs(float64(g)-0.501187) > 1e-5 {
		t.Errorf("LinearGain(-6 dB) = %v, want ~0.5012", g)
	}
	rg.PitchKeyCenter = 69
	if r := rg.PlaybackRateFor(81, 48000); math.Abs(r-2) > 1e-9 {
		t.Errorf("rate +12 semitones = %v, want 2", r)
	}
	if r := rg.PlaybackRateFor(69, 96000); math.Abs(r-0.5) > 1e-9 {
		t.Errorf("rate at double output rate = %v, want 0.5", r)
	}
	var noSample Region
	if noSample.Matches(60, 64) {
		t.Error("region without sample must not match")
	}
	if noSample.PlaybackRateFor(60, 48000) != 0 {
		t.Error("region without sample must have zero rate")
	}
}

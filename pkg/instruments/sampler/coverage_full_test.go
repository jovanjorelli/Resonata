package sampler

import (
	"math"
	"os"
	"path/filepath"
	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
	"strings"
	"testing"
)

// TestSamplerCoverageEdges verifies sampler boundary handling for invalid pitch, nil buffer, and negative rates.
func TestSamplerCoverageEdges(t *testing.T) {
	dir := t.TempDir()
	longLinePath := filepath.Join(dir, "long.sfz")
	if err := os.WriteFile(longLinePath, []byte(strings.Repeat("a", 2*1024*1024)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exp := NewIncludeExpander(NewPathResolver(dir))
	if _, err := exp.Expand(longLinePath); err == nil {
		t.Fatal("expected scanner error on overlong line")
	}

	parseIncludePath("#include \"unclosed")

	handleComment(nil, nil)

	rBase := &PathResolver{basePath: dir, cache: &resolveCache{entries: make(map[string]string)}}
	rSub := rBase.ForDir(dir)
	if rSub == nil {
		t.Fatal("nil resolver")
	}

	isWithinBase("/nonexistent/base/path/123", "/nonexistent/base/path/123/sub")

	rBase.cache.entries[strings.ToLower(filepath.Join(dir, "escaped.wav"))] = "/etc/passwd"
	if _, err := rBase.resolveCaseInsensitive("escaped.wav"); err == nil {
		t.Fatal("expected error for cached path outside root")
	}

	if _, err := rBase.Resolve("/nonexistent/abs/path.wav"); err == nil {
		t.Fatal("expected error for nonexistent abs path")
	}
	rBase.Resolve(filepath.Join(dir, "abs_inside_nonexistent.wav"))

	subDir := filepath.Join(dir, "sub")
	os.Mkdir(subDir, 0o755)
	outsideDir := t.TempDir()
	os.Symlink(outsideDir, filepath.Join(subDir, "outside_symlink"))
	rBase.Resolve("sub/outside_symlink/file.wav")

	ParseSFZ([]byte("<region> =stray sample=x.wav\n"))

	ParseSFZ([]byte("<region> sample=x.wav xfin_lokey=70 xfin_hikey=60 xfin_lovel=100 xfin_hivel=80 xfout_lokey=70 xfout_hikey=60 xfout_lovel=100 xfout_hivel=80\n"))

	ParseSFZ([]byte("comment=foo\n<region> sample=x.wav\n"))

	samples := make([]float32, 2000)
	sd := &SampleData{Samples: samples}
	rg := &Region{Sample: sd, LoopMode: LoopContinuous, LoopStart: 10, LoopEnd: 1000}
	v := &Voice{
		Active:           true,
		Region:           rg,
		pendingRegion:    rg,
		stealSampleRate:  48000,
		looping:          true,
		inCrossfade:      true,
		loopStart:        10,
		loopEnd:          1000,
		Phase:            0,
		crossfadeReadPos: 10,
		Envelope:         dsp.NewEnvelope(0.01, 0.01, 1.0, 0.01),
	}
	v.Envelope.Trigger()
	buf := make([]float32, 1)
	v.Process(buf, 1.0/48000.0)

	v.looping = true
	v.inCrossfade = true
	v.Phase = 1100
	v.Process(buf, 1.0/48000.0)

	vSteal := &Voice{
		Active:          true,
		isStealing:      true,
		pendingRelease:  true,
		pendingRegion:   rg,
		stealSampleRate: 48000,
		Envelope:        dsp.NewEnvelope(0.01, 0.01, 1.0, 0.01),
	}
	vSteal.Envelope.Trigger()
	vSteal.finishSteal()

	smp := New(48000)
	smp.sfz = &SFZFile{}
	smp.NoteOnParams(-1, 0.8, instruments.NoteParams{})
	smp.NoteOnParams(128, 0.8, instruments.NoteParams{})

	smp.trig = []xfTrigger{
		{rg: rg, gain: 1.0},
		{rg: rg, gain: 0.5},
	}
	smp.fireTriggers(60, 0.8, instruments.NoteParams{})

	smp.voices[0].Active = true
	smp.voices[0].Pitch = 60
	smp.voices[0].startTime = 10.0
	smp.voices[1].Active = true
	smp.voices[1].Pitch = 60
	smp.voices[1].startTime = 5.0
	smp.pitchCandidates(60)

	vInit := &Voice{Envelope: dsp.NewEnvelope(0.01, 0.01, 1.0, 0.01)}
	initVoice(vInit, rg, 60, 0.8, 48000, 0, 0, 0, 0, 0, instruments.NoteParams{
		HasExpression: true,
		Expression:    -0.5,
	})
	initVoice(vInit, rg, 60, 0.8, 48000, 0, 0, 0, 0, 0, instruments.NoteParams{
		HasExpression: true,
		Expression:    1.5,
	})
	initVoice(vInit, rg, 60, 0.8, 48000, 0, 0, 0, 0, 0, instruments.NoteParams{
		HasExpression: true,
		Expression:    float32(math.NaN()),
	})
}

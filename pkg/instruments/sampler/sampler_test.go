package sampler

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
)

const testRate = 48000

// newTestSampler builds a sampler with one region holding a 1-second
// 440 Hz sine at 48 kHz, keycenter 69.
func newTestSampler(t *testing.T) (*Sampler, *SFZFile) {
	t.Helper()
	sine := make([]float32, testRate)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / testRate))
	}
	f := &SFZFile{Regions: []Region{{
		SamplePath: "a.wav", LoKey: 0, HiKey: 127, PitchKeyCenter: 69,
		LoVel: 0, HiVel: 127, LoopMode: LoopNoLoop, LoopStart: -1, LoopEnd: -1,
		LoRand: 0, HiRand: 1,
		Sample: &SampleData{Samples: sine, SampleRate: testRate, Channels: 1},
	}}}
	s := New(testRate)
	s.Load(f)
	return s, f
}

// render processes d seconds through the sampler, counting rising zero
// crossings on the left channel and tracking its peak magnitude.
func render(s *Sampler, d float64) (crossings int, peak float32) {
	dt := 1.0 / testRate
	buf := dsp.NewStereoBuffer(512)
	frames := int(d * testRate)
	var prev float32
	for done := 0; done < frames; {
		n := min(512, frames-done)
		buf.SetLen(n)
		s.Process(buf, dt)
		for _, v := range buf.Left {
			if a := abs32(v); a > peak {
				peak = a
			}
			if prev < 0 && v >= 0 {
				crossings++
			}
			prev = v
		}
		done += n
	}
	return
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func TestSamplerNoteOnOff(t *testing.T) {
	s, _ := newTestSampler(t)
	if s.ActiveVoices() != 0 {
		t.Fatalf("idle voices = %d", s.ActiveVoices())
	}
	if _, peak := render(s, 0.05); peak != 0 {
		t.Fatalf("idle peak = %v, want exact silence", peak)
	}

	s.NoteOn(69, 1.0)
	if s.ActiveVoices() != 1 {
		t.Fatalf("voices = %d, want 1", s.ActiveVoices())
	}
	c, peak := render(s, 0.5) // 440 Hz for 0.5 s -> ~220 rising crossings
	if c < 210 || c > 230 {
		t.Errorf("crossings = %d, want ~220 (440 Hz)", c)
	}
	if peak < 0.6 || peak > 0.75 {
		t.Errorf("peak = %v, want ~0.707 (center pan)", peak)
	}

	s.NoteOff(69) // 0.2 s release should complete well within 0.5 s
	render(s, 0.5)
	if s.ActiveVoices() != 0 {
		t.Fatalf("voices after release = %d, want 0", s.ActiveVoices())
	}
	if _, peak := render(s, 0.1); peak != 0 {
		t.Fatalf("peak after release = %v, want exact silence", peak)
	}
}

func TestSamplerPitchShift(t *testing.T) {
	s, _ := newTestSampler(t)
	// +12 semitones doubles the playback rate: 880 Hz.
	s.NoteOn(81, 1.0)
	if c, _ := render(s, 0.25); c < 210 || c > 230 {
		t.Errorf("pitch 81: crossings = %d, want ~220 (880 Hz)", c)
	}
	s.NoteOff(81)
	render(s, 0.4)

	// -12 semitones halves the rate: 220 Hz.
	s.NoteOn(57, 1.0)
	if c, _ := render(s, 0.5); c < 100 || c > 120 {
		t.Errorf("pitch 57: crossings = %d, want ~110 (220 Hz)", c)
	}
}

func TestSamplerVelocityAndVolume(t *testing.T) {
	s1, _ := newTestSampler(t)
	s1.NoteOn(69, 1.0)
	_, full := render(s1, 0.1)

	s2, _ := newTestSampler(t)
	s2.NoteOn(69, 0.5)
	_, half := render(s2, 0.1)
	if r := half / full; math.Abs(float64(r)-0.5) > 0.03 {
		t.Errorf("velocity scaling = %v, want ~0.5", r)
	}

	s3, f3 := newTestSampler(t)
	f3.Regions[0].Volume = -6 // dB
	s3.NoteOn(69, 1.0)
	_, quiet := render(s3, 0.1)
	if r := quiet / full; math.Abs(float64(r)-0.5012) > 0.03 {
		t.Errorf("volume -6 dB ratio = %v, want ~0.501", r)
	}
}

func TestSamplerPan(t *testing.T) {
	render := func(pan float32) (lpeak, rpeak float32) {
		s, f := newTestSampler(t)
		f.Regions[0].Pan = pan
		s.NoteOn(69, 1.0)
		buf := dsp.NewStereoBuffer(1024)
		buf.SetLen(1024)
		s.Process(buf, 1.0/testRate)
		for i := range buf.Left {
			if a := abs32(buf.Left[i]); a > lpeak {
				lpeak = a
			}
			if a := abs32(buf.Right[i]); a > rpeak {
				rpeak = a
			}
		}
		return
	}
	if l, r := render(-1); l < 0.9 || r != 0 {
		t.Errorf("pan -1: left=%v right=%v, want loud/exact-zero", l, r)
	}
	if l, r := render(1); l != 0 || r < 0.9 {
		t.Errorf("pan +1: left=%v right=%v, want exact-zero/loud", l, r)
	}
	if l, r := render(0); math.Abs(float64(l-r)) > 1e-6 {
		t.Errorf("pan 0: left=%v right=%v, want equal", l, r)
	}
}

func TestSamplerPolyphony(t *testing.T) {
	s, _ := newTestSampler(t)
	s.NoteOn(60, 0.8)
	s.NoteOn(64, 0.8)
	s.NoteOn(67, 0.8)
	if s.ActiveVoices() != 3 {
		t.Fatalf("voices = %d, want 3", s.ActiveVoices())
	}
	s.NoteOff(60)
	releasing := 0
	for i := range s.voices {
		if s.voices[i].releasing {
			releasing++
			if s.voices[i].Pitch != 60 {
				t.Errorf("releasing voice has pitch %d", s.voices[i].Pitch)
			}
		}
	}
	if releasing != 1 {
		t.Fatalf("releasing voices = %d, want 1", releasing)
	}
	_, peak := render(s, 0.5) // the 60 voice finishes its release here
	if peak < 0.3 {
		t.Errorf("peak = %v, other voices should still sound", peak)
	}
	if s.ActiveVoices() != 2 {
		t.Fatalf("voices = %d, want 2 (only pitch 60 released)", s.ActiveVoices())
	}
}

func TestVoiceStealing(t *testing.T) {
	s, _ := newTestSampler(t)
	// Fill all 16 voices, advancing the clock between notes.
	for p := 60; p < 60+MaxVoices; p++ {
		s.NoteOn(p, 0.8)
		render(s, 1.0/testRate) // one frame, advances the clock
	}
	if s.ActiveVoices() != MaxVoices {
		t.Fatalf("voices = %d, want %d", s.ActiveVoices(), MaxVoices)
	}
	// No releasing voices: the oldest sounding voice is stolen.
	s.NoteOn(90, 0.9)
	if s.voices[0].Pitch != 90 || !s.voices[0].Active {
		t.Fatalf("oldest voice not stolen: %+v", s.voices[0])
	}
	if s.ActiveVoices() != MaxVoices {
		t.Fatalf("voices = %d, want %d", s.ActiveVoices(), MaxVoices)
	}
	// Two voices enter release at different times; the older one is stolen.
	s.NoteOff(62) // voices[2]
	render(s, 0.01)
	s.NoteOff(64) // voices[4]
	render(s, 0.01)
	s.NoteOn(91, 0.9)
	if s.voices[2].Pitch != 91 {
		t.Errorf("stolen voice = %d (pitch %d), want the older releasing voices[2]", 2, s.voices[2].Pitch)
	}
	if s.voices[4].Pitch != 64 || !s.voices[4].releasing {
		t.Errorf("newer releasing voice was stolen: %+v", s.voices[4])
	}
}

func TestLooping(t *testing.T) {
	s, f := newTestSampler(t)
	f.Regions[0].LoopMode = LoopContinuous
	f.Regions[0].Sample.LoopStart = 100
	f.Regions[0].Sample.LoopEnd = 47900 // inside the 48000-frame sample
	s.NoteOn(69, 1.0)
	// Three sample lengths of audio: a non-looping voice would be gone.
	if _, peak := render(s, 3.0); peak < 0.6 {
		t.Fatalf("peak = %v, looping voice should keep sounding", peak)
	}
	if s.ActiveVoices() != 1 {
		t.Fatalf("voices = %d, looping voice stopped", s.ActiveVoices())
	}
	s.NoteOff(69)
	render(s, 0.5)
	if s.ActiveVoices() != 0 {
		t.Fatal("looping voice survived note-off + release")
	}
}

func TestOneShotIgnoresNoteOff(t *testing.T) {
	s, f := newTestSampler(t)
	f.Regions[0].LoopMode = LoopOneShot
	s.NoteOn(69, 1.0)
	s.NoteOff(69)
	if s.voices[0].releasing {
		t.Fatal("one_shot voice entered release on note-off")
	}
	if _, peak := render(s, 0.3); peak < 0.6 {
		t.Fatalf("one_shot peak = %v, want full playback", peak)
	}
	if s.ActiveVoices() != 1 {
		t.Fatal("one_shot voice stopped early")
	}
}

func TestSampleRateConversion(t *testing.T) {
	// The same 440 Hz sine stored at 24 kHz: at pitch 69 the rate is
	// 24000/48000 = 0.5, so the output stays 440 Hz.
	sine := make([]float32, 24000)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 24000))
	}
	f := &SFZFile{Regions: []Region{{
		SamplePath: "h.wav", LoKey: 0, HiKey: 127, PitchKeyCenter: 69,
		LoVel: 0, HiVel: 127, LoopMode: LoopNoLoop, LoopStart: -1, LoopEnd: -1,
		LoRand: 0, HiRand: 1,
		Sample: &SampleData{Samples: sine, SampleRate: 24000, Channels: 1},
	}}}
	s := New(testRate)
	s.Load(f)

	s.NoteOn(69, 1.0)
	if c, _ := render(s, 0.5); c < 210 || c > 230 {
		t.Errorf("pitch 69: crossings = %d, want ~220 (440 Hz)", c)
	}
	s.NoteOff(69)
	render(s, 0.4)

	// Pitch 81: 2 × 0.5 = 1.0 source frames per output frame -> 880 Hz.
	s.NoteOn(81, 1.0)
	if c, _ := render(s, 0.25); c < 210 || c > 230 {
		t.Errorf("pitch 81: crossings = %d, want ~220 (880 Hz)", c)
	}
}

func TestVoiceProcessZeroFill(t *testing.T) {
	// A 100-frame constant sample rendered into a 256-frame buffer must
	// deactivate the voice and zero the untouched tail.
	samples := make([]float32, 100)
	for i := range samples {
		samples[i] = 0.5
	}
	rg := &Region{
		SamplePath: "z.wav", PitchKeyCenter: 69, LoopMode: LoopNoLoop,
		Sample: &SampleData{Samples: samples, SampleRate: testRate, Channels: 1},
	}
	env := dsp.NewEnvelope(0.001, 1e-6, 1.0, 0.1)
	env.Trigger()
	v := Voice{
		Region: rg, Pitch: 69, Velocity: 1, PlaybackRate: 1,
		Envelope: env, Active: true, loopEnd: 100, exprGain: 1, crossfadeGain: 1,
	}
	buf := make([]float32, 256)
	for i := range buf {
		buf[i] = 7
	}
	v.Process(buf, 256.0/testRate)
	if v.Active {
		t.Fatal("voice stayed active past the sample end")
	}
	if buf[50] < 0.4 || buf[50] > 0.55 {
		t.Errorf("frame 50 = %v, want ~0.5", buf[50])
	}
	for i := 100; i < 256; i++ {
		if buf[i] != 0 {
			t.Fatalf("frame %d = %v, want zero fill", i, buf[i])
		}
	}
}

func TestSamplerSetParameters(t *testing.T) {
	s, _ := newTestSampler(t)
	s.SetParameters(map[string]float32{"master_volume": 0})
	s.NoteOn(69, 1.0)
	if _, peak := render(s, 0.1); peak != 0 {
		t.Fatalf("master_volume 0: peak = %v, want silence", peak)
	}

	// Clamping: master_volume 5 behaves as 1. Envelope parameters apply
	// to the next NoteOn, so retrigger to hear the short release.
	s.SetParameters(map[string]float32{"master_volume": 5, "release": 0.01})
	s.NoteOff(69)
	render(s, 0.3) // drain the voice still using the default release
	if s.ActiveVoices() != 0 {
		t.Fatal("voice did not finish its release")
	}
	s.NoteOn(69, 1.0)
	if _, peak := render(s, 0.05); peak < 0.6 || peak > 0.75 {
		t.Fatalf("clamped master_volume: peak = %v, want ~0.707", peak)
	}
	s.NoteOff(69)
	render(s, 0.05) // the 10 ms release completes well within 50 ms
	if s.ActiveVoices() != 0 {
		t.Fatal("short release did not finish")
	}

	// Unknown keys and nil are no-ops.
	s.SetParameters(map[string]float32{"nonsense": 1})
	s.SetParameters(nil)
}

func TestSamplerZeroAlloc(t *testing.T) {
	s, _ := newTestSampler(t)
	s.NoteOn(69, 0.9)
	buf := dsp.NewStereoBuffer(256)
	dt := 1.0 / testRate
	buf.SetLen(256)
	s.Process(buf, dt) // warm the mix scratch
	allocs := testing.AllocsPerRun(50, func() {
		buf.SetLen(256)
		s.Process(buf, dt)
	})
	if allocs != 0 {
		t.Fatalf("Process allocates %v times per call, want 0", allocs)
	}
	allocs = testing.AllocsPerRun(20, func() {
		s.NoteOn(70, 0.8)
		s.NoteOff(70)
	})
	if allocs != 0 {
		t.Fatalf("NoteOn/NoteOff allocate %v times per call, want 0", allocs)
	}
}

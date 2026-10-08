package sampler

import (
	"math"
	"testing"
)

// regRegion builds a playable single-key region over synthetic audio.
func regRegion(frames int) (Region, *SampleData) {
	sd := &SampleData{Samples: make([]float32, frames), SampleRate: 48000, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	r := newRegion()
	r.SamplePath, r.Sample = "t.wav", sd
	r.LoKey, r.HiKey, r.PitchKeyCenter = 60, 60, 60
	return r, sd
}

// TestRegPedalHoldRelease proves note-offs defer while held and fire on
// release, with accessors reporting each transition.
func TestRegPedalHoldRelease(t *testing.T) {
	r, _ := regRegion(48000)
	r.HiKey = 61
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{r}})
	if s.PedalDown() {
		t.Fatal("pedal down on fresh sampler")
	}
	s.NoteOn(60, 0.9)
	s.NoteOff(60)
	releasing := false
	for i := range s.voices {
		if s.voices[i].Active && s.voices[i].releasing {
			releasing = true
		}
		if s.voices[i].pedalHeld {
			t.Fatal("voice held without pedal")
		}
	}
	if !releasing {
		t.Fatal("no-pedal note-off did not start release")
	}
	s.NoteOn(61, 0.9)
	s.SetPedal(true)
	if !s.PedalDown() {
		t.Fatal("pedal not reported")
	}
	s.NoteOff(61)
	held := 0
	for i := range s.voices {
		if s.voices[i].Active && s.voices[i].pedalHeld {
			held++
		}
	}
	if held != 1 {
		t.Fatalf("held voices = %d, want 1", held)
	}
	s.SetPedal(false)
	if s.PedalDown() {
		t.Fatal("pedal stuck down")
	}
	var voice *Voice
	for i := range s.voices {
		if s.voices[i].Active && !s.voices[i].releasing {
			t.Fatalf("voice %d neither held nor releasing", i)
		}
		if s.voices[i].pedalHeld {
			t.Fatal("pedal flag survived release")
		}
		if s.voices[i].Active {
			voice = &s.voices[i]
		}
	}
	if voice == nil {
		t.Fatal("released voice vanished")
	}
	buf := make([]float32, 512)
	for i := 0; i < 500 && s.ActiveVoices() > 0; i++ {
		for k := range s.voices {
			if s.voices[k].Active {
				s.voices[k].Process(buf, 512.0/48000)
			}
		}
	}
	if got := s.ActiveVoices(); got != 0 {
		t.Fatalf("voices = %d after release, want 0", got)
	}
}

// TestRegNoiseGateAPI proves dB conversion, disable paths, and the
// linear threshold reporter.
func TestRegNoiseGateAPI(t *testing.T) {
	s := New(48000)
	if got := s.NoiseGateThreshold(); got != 0 {
		t.Fatalf("threshold = %v, want 0 (off)", got)
	}
	s.SetNoiseGateDB(-50)
	want := float32(math.Pow(10, -50.0/20))
	if got := s.NoiseGateThreshold(); got != want {
		t.Fatalf("threshold = %v, want %v", got, want)
	}
	for _, db := range []float64{0, 3, math.NaN()} {
		s.SetNoiseGateDB(db)
		if got := s.NoiseGateThreshold(); got != 0 {
			t.Fatalf("db %v: threshold = %v, want 0 (off)", db, got)
		}
	}
	s.SetNoiseGateDB(-200) // floors at -120 dB
	if got, want := s.NoiseGateThreshold(), float32(math.Pow(10, -6)); got != want {
		t.Fatalf("floored = %v, want %v", got, want)
	}
}

// TestRegGateMutesQuiet proves sub-threshold voice output mutes while
// loud output passes untouched.
func TestRegGateMutesQuiet(t *testing.T) {
	r, _ := regRegion(4096)
	s := New(48000)
	s.Load(&SFZFile{Regions: []Region{r}})
	s.SetNoiseGateDB(-50)
	s.NoteOn(60, 0.001) // voice output sits below the gate
	buf := make([]float32, 512)
	v := &s.voices[0]
	v.Process(buf, 512.0/48000)
	for i, x := range buf {
		if x != 0 {
			t.Fatalf("frame %d = %v, want gated silence", i, x)
		}
	}
	s2 := New(48000)
	s2.Load(&SFZFile{Regions: []Region{r}})
	s2.NoteOn(60, 0.9)
	peak := float32(0)
	v2 := &s2.voices[0]
	v2.Process(buf, 512.0/48000)
	for _, x := range buf {
		if x < 0 {
			x = -x
		}
		if x > peak {
			peak = x
		}
	}
	if peak <= 0.1 {
		t.Fatalf("loud voice gated: peak %v", peak)
	}
}

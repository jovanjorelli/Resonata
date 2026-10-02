package sampler

import (
	"math"
	"testing"
)

// xfRegion builds a constant-sample region over keys with a velocity
// range and crossfade zones.
func xfRegion(path string, loKey, hiKey, loVel, hiVel, xfinLo, xfinHi, xfoutLo, xfoutHi int) Region {
	n := testRate / 2
	sd := &SampleData{Samples: make([]float32, n), SampleRate: testRate, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = 0.5
	}
	return Region{
		SamplePath: path, LoKey: loKey, HiKey: hiKey, PitchKeyCenter: 60,
		LoVel: loVel, HiVel: hiVel, LoopMode: LoopNoLoop,
		XfinLoVel: xfinLo, XfinHiVel: xfinHi,
		XfoutLoVel: xfoutLo, XfoutHiVel: xfoutHi,
		LoRand: 0, HiRand: 1, Sample: sd,
	}
}

// xfPair is the standard two-layer velocity crossfade: A fades out
// across 60-70, B fades in across the same zone.
func xfPair() (a, b Region) {
	a = xfRegion("a.wav", 60, 60, 1, 70, 0, 0, 60, 70)
	b = xfRegion("b.wav", 60, 60, 60, 127, 60, 70, 127, 127)
	return a, b
}

// velFor maps an integer MIDI velocity to a float safely inside its
// bucket, robust against float32 rounding at bucket edges.
func velFor(v int) float32 { return (float32(v) + 0.5) / 127 }

// voicesFor returns the active voices playing the given sample path.
func voicesFor(s *Sampler, path string) []*Voice {
	var out []*Voice
	for i := range s.voices {
		if v := &s.voices[i]; v.Active && v.Region != nil && v.Region.SamplePath == path {
			out = append(out, v)
		}
	}
	return out
}

// loadXFBinds builds a sampler over the given regions.
func loadXFBinds(regions ...Region) *Sampler {
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	return s
}

// TestXfinParsing reads all eight crossfade opcodes with group-level
// inheritance and region override.
func TestXfinParsing(t *testing.T) {
	f, err := ParseSFZ([]byte("<group>\n" +
		"xfin_lovel=60\nxfin_hivel=70\nxfout_lovel=60\nxfout_hivel=70\n" +
		"<region>\nsample=a.wav\nkey=60\nlovel=1\nhivel=70\n" +
		"<region>\nsample=b.wav\nkey=60\nlovel=60\nhivel=127\nxfout_lovel=50\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Regions) != 2 {
		t.Fatalf("regions = %d, want 2", len(f.Regions))
	}
	a := f.Regions[0]
	if a.XfinLoVel != 60 || a.XfinHiVel != 70 || a.XfoutLoVel != 60 || a.XfoutHiVel != 70 {
		t.Errorf("inherited zones = %+v", a)
	}
	b := f.Regions[1]
	if b.XfoutLoVel != 50 || b.XfoutHiVel != 70 {
		t.Errorf("override zone = %d/%d, want 50/70", b.XfoutLoVel, b.XfoutHiVel)
	}
	if b.XfinLoVel != 60 || b.XfinHiVel != 70 {
		t.Errorf("inherited xfin = %d/%d, want 60/70", b.XfinLoVel, b.XfinHiVel)
	}
}

// TestCrossfadeMidpoint blends two layers at equal power: velocity 65
// sits halfway through the 60-70 zone, so both gains are ~0.707.
func TestCrossfadeMidpoint(t *testing.T) {
	a, b := xfPair()
	s := loadXFBinds(a, b)
	s.NoteOn(60, velFor(65))
	va, vb := voicesFor(s, "a.wav"), voicesFor(s, "b.wav")
	if len(va) != 1 || len(vb) != 1 {
		t.Fatalf("voices = %d/%d, want 1/1", len(va), len(vb))
	}
	want := float32(math.Cos(math.Pi / 4)) // == sin(pi/4)
	if d := va[0].crossfadeGain - want; d < -1e-6 || d > 1e-6 {
		t.Errorf("A gain = %v, want ~%v", va[0].crossfadeGain, want)
	}
	if d := vb[0].crossfadeGain - want; d < -1e-6 || d > 1e-6 {
		t.Errorf("B gain = %v, want ~%v", vb[0].crossfadeGain, want)
	}
}

// TestCrossfadeBoundaries parks the velocity at each end of the zone:
// the outgoing layer plays full while the incoming layer is silent.
func TestCrossfadeBoundaries(t *testing.T) {
	a, b := xfPair()
	s := loadXFBinds(a, b)
	s.NoteOn(60, velFor(60))
	va, vb := voicesFor(s, "a.wav"), voicesFor(s, "b.wav")
	if len(va) != 1 || len(vb) != 1 {
		t.Fatalf("vel 60: voices = %d/%d, want 1/1", len(va), len(vb))
	}
	if va[0].crossfadeGain != 1.0 {
		t.Errorf("vel 60: A gain = %v, want 1.0", va[0].crossfadeGain)
	}
	if vb[0].crossfadeGain != 0.0 {
		t.Errorf("vel 60: B gain = %v, want 0.0", vb[0].crossfadeGain)
	}

	s2 := loadXFBinds(a, b)
	s2.NoteOn(60, velFor(70))
	va, vb = voicesFor(s2, "a.wav"), voicesFor(s2, "b.wav")
	if len(va) != 1 || len(vb) != 1 {
		t.Fatalf("vel 70: voices = %d/%d, want 1/1", len(va), len(vb))
	}
	if d := va[0].crossfadeGain; d < -1e-6 || d > 1e-6 {
		t.Errorf("vel 70: A gain = %v, want ~0.0", va[0].crossfadeGain)
	}
	if vb[0].crossfadeGain != 1.0 {
		t.Errorf("vel 70: B gain = %v, want 1.0", vb[0].crossfadeGain)
	}
}

// TestCrossfadeNone keeps disjoint layers on a single full-gain voice.
func TestCrossfadeNone(t *testing.T) {
	a := xfRegion("a.wav", 60, 60, 1, 63, 0, 0, 127, 127)
	b := xfRegion("b.wav", 60, 60, 64, 127, 0, 0, 127, 127)
	s := loadXFBinds(a, b)
	s.NoteOn(60, velFor(32))
	if n := len(activePitches(s)); n != 1 {
		t.Fatalf("low trigger voices = %d, want 1", n)
	}
	if g := voicesFor(s, "a.wav")[0].crossfadeGain; g != 1.0 {
		t.Fatalf("low gain = %v, want 1.0", g)
	}
	s2 := loadXFBinds(a, b)
	s2.NoteOn(60, velFor(96))
	if n := len(activePitches(s2)); n != 1 {
		t.Fatalf("high trigger voices = %d, want 1", n)
	}
	if g := voicesFor(s2, "b.wav")[0].crossfadeGain; g != 1.0 {
		t.Fatalf("high gain = %v, want 1.0", g)
	}
}

// TestCrossfadeRoundRobin layers two round-robin pairs across one
// velocity zone: each trigger sounds one take per layer, and the takes
// advance together step by step.
func TestCrossfadeRoundRobin(t *testing.T) {
	mk := func(path string, pos, loVel, hiVel, xfinLo, xfinHi, xfoutLo, xfoutHi int) Region {
		r := xfRegion(path, 60, 60, loVel, hiVel, xfinLo, xfinHi, xfoutLo, xfoutHi)
		r.SeqLength, r.SeqPosition = 2, pos
		return r
	}
	regions := []Region{
		mk("a1.wav", 1, 1, 70, 0, 0, 60, 70),
		mk("a2.wav", 2, 1, 70, 0, 0, 60, 70),
		mk("b1.wav", 1, 60, 127, 60, 70, 127, 127),
		mk("b2.wav", 2, 60, 127, 60, 70, 127, 127),
	}
	s := loadXFBinds(regions...)
	s.NoteOn(60, velFor(65))
	if got := seqVoicePaths(s, 60); !equalPaths(got, []string{"a1.wav", "b1.wav"}) {
		t.Fatalf("step 0 takes = %v, want [a1.wav b1.wav]", got)
	}
	s.NoteOn(60, velFor(65))
	if got := seqVoicePaths(s, 60); !equalPaths(got, []string{"a1.wav", "a2.wav", "b1.wav", "b2.wav"}) {
		t.Fatalf("step 1 takes = %v, want all four takes", got)
	}
}

// TestCrossfadeRandom composes probability with blending: over one
// hundred seeded triggers in the overlap zone, both-layers, A-only,
// and B-only outcomes all occur.
func TestCrossfadeRandom(t *testing.T) {
	a, b := xfPair()
	a.LoRand, a.HiRand = 0, 0.6
	b.LoRand, b.HiRand = 0.4, 1
	s := loadXFBinds(a, b)
	var both, onlyA, onlyB int
	for k := 0; k < 100; k++ {
		before := len(activePitches(s))
		s.NoteOn(60, velFor(65))
		after := len(activePitches(s))
		hasA, hasB := len(voicesFor(s, "a.wav")) > 0, len(voicesFor(s, "b.wav")) > 0
		switch {
		case hasA && hasB && after-before == 2:
			both++
		case hasA && !hasB && after-before == 1:
			onlyA++
		case hasB && !hasA && after-before == 1:
			onlyB++
		default:
			t.Fatalf("trigger %d: unexpected outcome (a=%v b=%v delta=%d)", k, hasA, hasB, after-before)
		}
		renderMono(s, 0.5) // drain: faded and finished voices free
	}
	if both == 0 || onlyA == 0 || onlyB == 0 {
		t.Fatalf("outcomes both=%d a=%d b=%d, want all represented", both, onlyA, onlyB)
	}
	t.Logf("outcomes both=%d a=%d b=%d", both, onlyA, onlyB)
}

// TestCrossfadePoolExhaustion drops the quiet layer when only one
// voice is free: fifteen foreign voices fill the pool, then the
// overlap trigger keeps the dominant layer and drops the rest.
func TestCrossfadePoolExhaustion(t *testing.T) {
	a, b := xfPair()
	c := xfRegion("c.wav", 61, 127, 0, 127, 0, 0, 127, 127)
	s := loadXFBinds(a, b, c)
	for p := 61; p <= 75; p++ {
		s.NoteOn(p, 0.8)
	}
	if n := len(activePitches(s)); n != 15 {
		t.Fatalf("fillers = %d, want 15", n)
	}
	s.NoteOn(60, velFor(66)) // A ~0.588, B ~0.809
	if n := len(activePitches(s)); n != 16 {
		t.Fatalf("voices = %d, want a full pool of 16", n)
	}
	vb := voicesFor(s, "b.wav")
	if len(vb) != 1 {
		t.Fatalf("B voices = %d, want the dominant layer kept", len(vb))
	}
	if d := vb[0].crossfadeGain - 0.80901699; d < -1e-6 || d > 1e-6 {
		t.Errorf("B gain = %v, want ~0.809", vb[0].crossfadeGain)
	}
	if va := voicesFor(s, "a.wav"); len(va) != 0 {
		t.Errorf("A voices = %d, want the quiet layer dropped", len(va))
	}
}

// TestCrossfadeZeroAlloc hammers the overlap zone with zero heap
// allocations across matching, gain math, and trigger collection.
func TestCrossfadeZeroAlloc(t *testing.T) {
	a, b := xfPair()
	s := loadXFBinds(a, b)
	s.NoteOn(60, velFor(65)) // warm the trigger scratch
	if allocs := testing.AllocsPerRun(1000, func() {
		s.NoteOn(60, velFor(65))
	}); allocs != 0 {
		t.Fatalf("crossfade NoteOn allocates %v times, want 0", allocs)
	}
}

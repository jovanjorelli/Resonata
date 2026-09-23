package sampler

import (
	"sort"
	"testing"

	"resonata/pkg/dsp"
)

// randRegion builds a region with a probabilistic window over the full
// key and velocity range.
func randRegion(path string, lo, hi float32) Region {
	r := seqTestRegion(path, 60, 60, 0, 127, 1, 1)
	r.LoRand, r.HiRand = lo, hi
	return r
}

// triggerOnce fires one note, captures the selected region paths, then
// releases and drains all voices so the next trigger starts clean.
func triggerOnce(t *testing.T, s *Sampler, pitch int, vel float32) []string {
	t.Helper()
	s.NoteOn(pitch, vel)
	var out []string
	for i := range s.voices {
		v := &s.voices[i]
		if v.Active && !v.releasing && v.Pitch == pitch && v.Region != nil {
			out = append(out, v.Region.SamplePath)
		}
	}
	sort.Strings(out)
	s.NoteOff(pitch)
	buf := dsp.NewStereoBuffer(512)
	dt := 1.0 / testRate
	for done := 0; done < int(0.3*testRate); {
		n := 512
		if int(0.3*testRate)-done < n {
			n = int(0.3*testRate) - done
		}
		buf.SetLen(n)
		s.Process(buf, dt)
		done += n
	}
	if s.ActiveVoices() != 0 {
		t.Fatalf("voices did not drain: %d", s.ActiveVoices())
	}
	return out
}

// triggerSequence fires n notes, returning each trigger's selection.
func triggerSequence(t *testing.T, s *Sampler, pitch int, vel float32, n int) [][]string {
	t.Helper()
	seq := make([][]string, 0, n)
	for i := 0; i < n; i++ {
		seq = append(seq, triggerOnce(t, s, pitch, vel))
	}
	return seq
}

func equalSelections(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !equalPaths(a[i], b[i]) {
			return false
		}
	}
	return true
}

// TestRandTwoLayers checks complementary random windows both fire across
// repeated triggers, and that the stream is deterministic per seed.
func TestRandTwoLayers(t *testing.T) {
	mk := func() *Sampler {
		s := New(testRate)
		s.Load(&SFZFile{Regions: []Region{
			randRegion("lo.wav", 0, 0.5),
			randRegion("hi.wav", 0.5, 1),
		}})
		return s
	}
	a := triggerSequence(t, mk(), 60, 0.8, 100)
	seen := map[string]bool{}
	for k, sel := range a {
		if len(sel) == 0 {
			t.Fatalf("trigger %d selected nothing (windows cover [0,1])", k)
		}
		for _, p := range sel {
			seen[p] = true
		}
	}
	if !seen["lo.wav"] || !seen["hi.wav"] {
		t.Fatalf("layers reached = %v, want both", seen)
	}
	b := triggerSequence(t, mk(), 60, 0.8, 100)
	if !equalSelections(a, b) {
		t.Fatal("same seed produced different selection sequences")
	}
}

// TestRandPointWindow checks a [0,0] window almost never fires: only an
// exact 0.0 roll (probability 2^-24) admits it.
func TestRandPointWindow(t *testing.T) {
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{randRegion("pin.wav", 0, 0)}})
	hits := 0
	for i := 0; i < 10; i++ {
		hits += len(triggerOnce(t, s, 60, 0.8))
	}
	if hits > 1 {
		t.Fatalf("point window fired %d/10 times, want 0 or 1", hits)
	}
}

// TestRandBackwardCompat checks regions without random opcodes fire on
// every trigger.
func TestRandBackwardCompat(t *testing.T) {
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{seqTestRegion("a.wav", 60, 60, 0, 127, 1, 1)}})
	for i := 0; i < 10; i++ {
		if got := triggerOnce(t, s, 60, 0.8); !equalPaths(got, []string{"a.wav"}) {
			t.Fatalf("trigger %d: voices = %v, want [a.wav]", i, got)
		}
	}
}

// TestLorandHirandParsing checks window parsing, inverted-range swap,
// group inheritance, and malformed values.
func TestLorandHirandParsing(t *testing.T) {
	f, err := ParseSFZ([]byte("sample=a.wav key=60 lorand=0.8 hirand=0.2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Regions[0]; r.LoRand != 0.2 || r.HiRand != 0.8 {
		t.Fatalf("swapped window = %v/%v, want 0.2/0.8", r.LoRand, r.HiRand)
	}
	g, err := ParseSFZ([]byte("<group>\nlorand=0.1 hirand=0.9\n" +
		"<region>\nsample=a.wav key=60\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := g.Regions[0]; r.LoRand != 0.1 || r.HiRand != 0.9 {
		t.Fatalf("inherited window = %v/%v, want 0.1/0.9", r.LoRand, r.HiRand)
	}
	plain, err := ParseSFZ([]byte("sample=a.wav key=60\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r := plain.Regions[0]; r.LoRand != 0 || r.HiRand != 1 {
		t.Fatalf("defaults = %v/%v, want 0/1", r.LoRand, r.HiRand)
	}
	bad, err := ParseSFZ([]byte("sample=a.wav key=60 lorand=nope\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bad.Regions[0].LoRand != 0 {
		t.Fatalf("malformed lorand kept = %v", bad.Regions[0].LoRand)
	}
	if bad.Stats.BadValues != 1 {
		t.Fatalf("BadValues = %d, want 1", bad.Stats.BadValues)
	}
}

// TestRandSeqCycle checks round-robin cycling inside a fully overlapping
// random-filtered set: eight triggers walk 1-2-3-4 twice.
func TestRandSeqCycle(t *testing.T) {
	mk := func(path string, pos int) Region {
		r := seqTestRegion(path, 60, 60, 0, 127, 4, pos)
		r.LoRand, r.HiRand = 0, 1
		return r
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{
		mk("s1.wav", 1), mk("s2.wav", 2), mk("s3.wav", 3), mk("s4.wav", 4),
	}})
	seq := triggerSequence(t, s, 60, 0.8, 8)
	want := [][]string{
		{"s1.wav"}, {"s2.wav"}, {"s3.wav"}, {"s4.wav"},
		{"s1.wav"}, {"s2.wav"}, {"s3.wav"}, {"s4.wav"},
	}
	if !equalSelections(seq, want) {
		t.Fatalf("cycle = %v, want %v", seq, want)
	}
}

// TestRandZeroAlloc checks a thousand filtered triggers allocate nothing.
func TestRandZeroAlloc(t *testing.T) {
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{
		randRegion("a.wav", 0, 0.4),
		randRegion("b.wav", 0.3, 0.7),
		randRegion("c.wav", 0.6, 1),
	}})
	if allocs := testing.AllocsPerRun(1000, func() { s.NoteOn(60, 0.8) }); allocs != 0 {
		t.Fatalf("1000 filtered triggers allocate %v times, want 0", allocs)
	}
}

// BenchmarkNoteOnRand measures filtered trigger throughput.
func BenchmarkNoteOnRand(b *testing.B) {
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{
		randRegion("a.wav", 0, 0.4),
		randRegion("b.wav", 0.3, 0.7),
		randRegion("c.wav", 0.6, 1),
	}})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.NoteOn(60, 0.8)
	}
}

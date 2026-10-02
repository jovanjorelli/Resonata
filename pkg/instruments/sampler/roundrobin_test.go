package sampler

import (
	"sort"
	"testing"
)

// seqTestRegion builds a region with distinct sample identity for
// round-robin tests. All regions in one group share key and velocity
// ranges and differ only in sequence position.
func seqTestRegion(path string, loKey, hiKey, loVel, hiVel, length, pos int) Region {
	return Region{
		SamplePath: path, LoKey: loKey, HiKey: hiKey, PitchKeyCenter: 60,
		LoVel: loVel, HiVel: hiVel, LoopMode: LoopNoLoop, LoopStart: -1, LoopEnd: -1,
		SeqLength: length, SeqPosition: pos, LoRand: 0, HiRand: 1,
		Sample: &SampleData{Samples: []float32{0.1, 0.2, 0.3}, SampleRate: testRate, Channels: 1},
	}
}

// seqVoicePaths returns the sample paths of active voices holding pitch,
// identifying which regions the last triggers selected.
func seqVoicePaths(s *Sampler, pitch int) []string {
	var out []string
	for i := range s.voices {
		v := &s.voices[i]
		if v.Active && v.Pitch == pitch && v.Region != nil {
			out = append(out, v.Region.SamplePath)
		}
	}
	sort.Strings(out)
	return out
}

func equalPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSeqRoundRobinCycle checks a four-step group cycles 1-2-3-4 and
// wraps back to 1 on the fifth trigger.
func TestSeqRoundRobinCycle(t *testing.T) {
	regions := []Region{
		seqTestRegion("s1.wav", 60, 60, 0, 127, 4, 1),
		seqTestRegion("s2.wav", 60, 60, 0, 127, 4, 2),
		seqTestRegion("s3.wav", 60, 60, 0, 127, 4, 3),
		seqTestRegion("s4.wav", 60, 60, 0, 127, 4, 4),
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	want := [][]string{
		{"s1.wav"},
		{"s1.wav", "s2.wav"},
		{"s1.wav", "s2.wav", "s3.wav"},
		{"s1.wav", "s2.wav", "s3.wav", "s4.wav"},
		{"s1.wav", "s1.wav", "s2.wav", "s3.wav", "s4.wav"},
	}
	for k, w := range want {
		s.NoteOn(60, 0.8)
		if got := seqVoicePaths(s, 60); !equalPaths(got, w) {
			t.Fatalf("trigger %d: voices = %v, want %v", k+1, got, w)
		}
	}
}

// TestSeqPerKeyIndependence checks interleaved notes cycle on their own
// counters: the second C4 selects position 2, not position 3.
func TestSeqPerKeyIndependence(t *testing.T) {
	regions := []Region{
		seqTestRegion("c1.wav", 60, 60, 0, 127, 4, 1),
		seqTestRegion("c2.wav", 60, 60, 0, 127, 4, 2),
		seqTestRegion("c3.wav", 60, 60, 0, 127, 4, 3),
		seqTestRegion("c4.wav", 60, 60, 0, 127, 4, 4),
		seqTestRegion("e1.wav", 64, 64, 0, 127, 4, 1),
		seqTestRegion("e2.wav", 64, 64, 0, 127, 4, 2),
		seqTestRegion("e3.wav", 64, 64, 0, 127, 4, 3),
		seqTestRegion("e4.wav", 64, 64, 0, 127, 4, 4),
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	for _, pitch := range []int{60, 64, 60, 64} {
		s.NoteOn(pitch, 0.8)
	}
	if got, want := seqVoicePaths(s, 60), []string{"c1.wav", "c2.wav"}; !equalPaths(got, want) {
		t.Fatalf("C4 voices = %v, want %v", got, want)
	}
	if got, want := seqVoicePaths(s, 64), []string{"e1.wav", "e2.wav"}; !equalPaths(got, want) {
		t.Fatalf("E4 voices = %v, want %v", got, want)
	}
}

// TestSeqSingleMatchNoAdvance checks a lone matched member of a sequence
// group plays through without moving the counter: a later fully-matched
// trigger still starts at position 1.
func TestSeqSingleMatchNoAdvance(t *testing.T) {
	regions := []Region{
		seqTestRegion("a.wav", 60, 60, 0, 100, 2, 1),
		seqTestRegion("b.wav", 60, 60, 64, 127, 2, 2),
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	s.NoteOn(60, 0.2) // velocity 25: only region a matches
	if got, want := seqVoicePaths(s, 60), []string{"a.wav"}; !equalPaths(got, want) {
		t.Fatalf("lone trigger: voices = %v, want %v", got, want)
	}
	s.NoteOn(60, 0.63) // velocity 80: both match, counter must be at 0
	if got, want := seqVoicePaths(s, 60), []string{"a.wav", "a.wav"}; !equalPaths(got, want) {
		t.Fatalf("group trigger after lone match: voices = %v, want %v (position 1)", got, want)
	}
}

// TestSeqLegacyPassthrough checks regions without sequence opcodes fire
// together on every trigger, preserving pre-round-robin behavior.
func TestSeqLegacyPassthrough(t *testing.T) {
	mk := func(path string) Region {
		r := seqTestRegion(path, 60, 60, 0, 127, 1, 1)
		r.SeqLength, r.SeqPosition = 0, 0 // pre-sequence files carry zeros
		return r
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{mk("a.wav"), mk("b.wav")}})
	for k := 0; k < 3; k++ {
		s.NoteOn(60, 0.8)
		want := []string{"a.wav", "b.wav"}
		for j := 0; j < k; j++ {
			want = append(want, "a.wav", "b.wav")
		}
		sort.Strings(want)
		if got := seqVoicePaths(s, 60); !equalPaths(got, want) {
			t.Fatalf("trigger %d: voices = %v, want %v", k+1, got, want)
		}
	}
}

// TestSeqParsing checks seq_length/seq_position parsing, group-level
// inheritance, defaults, and malformed values.
func TestSeqParsing(t *testing.T) {
	f, err := ParseSFZ([]byte("<group>\nseq_length=4\n" +
		"<region>\nsample=a.wav key=60 seq_position=1\n" +
		"<region>\nsample=b.wav key=60 seq_position=2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Regions) != 2 {
		t.Fatalf("regions = %d, want 2", len(f.Regions))
	}
	for i, r := range f.Regions {
		if r.SeqLength != 4 {
			t.Errorf("region %d: length = %d, want inherited 4", i, r.SeqLength)
		}
		if r.SeqPosition != i+1 {
			t.Errorf("region %d: position = %d, want %d", i, r.SeqPosition, i+1)
		}
	}
	plain, err := ParseSFZ([]byte("sample=a.wav key=60\n"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Regions[0].SeqLength != 1 || plain.Regions[0].SeqPosition != 1 {
		t.Fatalf("defaults = %d/%d, want 1/1",
			plain.Regions[0].SeqLength, plain.Regions[0].SeqPosition)
	}
	bad, err := ParseSFZ([]byte("sample=a.wav key=60 seq_length=0 seq_position=-2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bad.Regions[0].SeqLength != 1 || bad.Regions[0].SeqPosition != 1 {
		t.Fatalf("malformed kept = %d/%d, want defaults 1/1",
			bad.Regions[0].SeqLength, bad.Regions[0].SeqPosition)
	}
	if bad.Stats.BadValues != 2 {
		t.Fatalf("BadValues = %d, want 2", bad.Stats.BadValues)
	}
}

// TestSeqReset checks Load restarts every per-key counter at position 1.
func TestSeqReset(t *testing.T) {
	regions := []Region{
		seqTestRegion("s1.wav", 60, 60, 0, 127, 2, 1),
		seqTestRegion("s2.wav", 60, 60, 0, 127, 2, 2),
	}
	s := New(testRate)
	f := &SFZFile{Regions: regions}
	s.Load(f)
	s.NoteOn(60, 0.8)
	s.NoteOn(60, 0.8)
	if got := seqVoicePaths(s, 60); !equalPaths(got, []string{"s1.wav", "s2.wav"}) {
		t.Fatalf("voices = %v", got)
	}
	s.Load(f) // new score: counters restart
	s.NoteOn(60, 0.8)
	if got := seqVoicePaths(s, 60); !equalPaths(got, []string{"s1.wav"}) {
		t.Fatalf("after reload: voices = %v, want position 1", got)
	}
	s.ResetSequences()
	s.NoteOn(64, 0.8) // untouched key stays silent-safe
	if s.seq[60] != 0 || len(seqVoicePaths(s, 64)) != 0 {
		t.Fatal("reset state wrong")
	}
}

// TestSeqZeroAlloc checks a thousand round-robin triggers allocate
// nothing on the heap.
func TestSeqZeroAlloc(t *testing.T) {
	regions := []Region{
		seqTestRegion("s1.wav", 60, 60, 0, 127, 4, 1),
		seqTestRegion("s2.wav", 60, 60, 0, 127, 4, 2),
		seqTestRegion("s3.wav", 60, 60, 0, 127, 4, 3),
		seqTestRegion("s4.wav", 60, 60, 0, 127, 4, 4),
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	if allocs := testing.AllocsPerRun(1000, func() { s.NoteOn(60, 0.8) }); allocs != 0 {
		t.Fatalf("1000 round-robin triggers allocate %v times, want 0", allocs)
	}
}

// BenchmarkNoteOnSeq measures round-robin trigger throughput.
func BenchmarkNoteOnSeq(b *testing.B) {
	regions := []Region{
		seqTestRegion("s1.wav", 60, 60, 0, 127, 4, 1),
		seqTestRegion("s2.wav", 60, 60, 0, 127, 4, 2),
		seqTestRegion("s3.wav", 60, 60, 0, 127, 4, 3),
		seqTestRegion("s4.wav", 60, 60, 0, 127, 4, 4),
	}
	s := New(testRate)
	s.Load(&SFZFile{Regions: regions})
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s.NoteOn(60, 0.8)
	}
}

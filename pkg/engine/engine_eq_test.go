package engine

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
	"resonata/pkg/score"
)

// goertzelDB measures the level at freq within [from, to] seconds of an
// interleaved master bus (left channel).
func goertzelDB(master []float32, freq, from, to float64, sampleRate int) float64 {
	i0 := int(from*float64(sampleRate)) * 2
	i1 := int(to*float64(sampleRate)) * 2
	if i1 > len(master) {
		i1 = len(master)
	}
	k := 2 * math.Cos(2*math.Pi*freq/float64(sampleRate))
	var s1, s2 float64
	n := 0
	for i := i0; i+1 < i1; i += 2 {
		v := float64(master[i])
		s1, s2 = v+k*s1-s2, s1
		n++
	}
	if n == 0 {
		return -180
	}
	mag := math.Sqrt(math.Max(0, s1*s1+s2*s2-k*s1*s2)) * 2 / float64(n)
	if mag <= 1e-9 {
		return -180
	}
	return 20 * math.Log10(mag)
}

// TestEngineEQSpectral renders the EQ fixture score with and without its
// per-track sculpts and proves with Goertzel probes that each measured
// delta matches the track's theoretical RBJ response. Ported from the
// retired debug binary so the end-to-end JSON coverage is
// kept as a unit test.
func TestEngineEQSpectral(t *testing.T) {
	const sampleRate = 48000
	sculpted, err := score.ParseFile("../../test_data/eq_test.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := score.Validate(sculpted); err != nil {
		t.Fatalf("invalid fixture: %v", err)
	}
	bypassed, err := score.ParseFile("../../test_data/eq_test.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := range bypassed.Tracks {
		bypassed.Tracks[i].EQ = score.TrackEQ{}
	}

	render := func(s *score.Score) []float32 {
		eng, err := New(s, sampleRate, 4096)
		if err != nil {
			t.Fatal(err)
		}
		eng.Mixer().SetReverbWet(0) // dry masters for clean comparison
		var master []float32
		for done := 0; done < eng.TotalFrames(); {
			n := min(4096, eng.TotalFrames()-done)
			eng.ProcessFrames(n, func(m []float32) { master = append(master, m...) })
			done += n
		}
		return master
	}
	masterA, masterB := render(sculpted), render(bypassed)

	// Windows isolate each track in time; the control track has no EQ,
	// so its deltas must be ~0.
	probes := []struct {
		name  string
		freq  float64
		from  float64
		to    float64
		track int
		tolDB float64
	}{
		{"fundamental 220 Hz", 220.00, 1.0, 2.8, 0, 0.8},
		{"mud cut 440 Hz", 440.00, 1.0, 2.8, 0, 0.8},
		{"3rd harmonic 660 Hz", 660.00, 1.0, 2.8, 0, 0.8},
		{"air shelf 8 kHz", 8000.00, 1.0, 2.8, 0, 1.5},
		{"control fundamental 330 Hz", 329.63, 4.0, 5.8, 1, 0.5},
		{"control harmonic 659 Hz", 659.25, 4.0, 5.8, 1, 0.5},
	}
	eqs := make([]*dsp.EQ, len(sculpted.Tracks))
	for i, tr := range sculpted.Tracks {
		eqs[i] = dsp.NewEQ(sampleRate, EQSpecFor(tr.EQ))
	}
	for _, p := range probes {
		a := goertzelDB(masterA, p.freq, p.from, p.to, sampleRate)
		b := goertzelDB(masterB, p.freq, p.from, p.to, sampleRate)
		delta := a - b
		expected := 20 * math.Log10(eqs[p.track].ResponseAt(p.freq))
		if math.Abs(delta-expected) > p.tolDB {
			t.Errorf("%s: delta %+.2f dB, want %+.2f dB (tol %.1f)", p.name, delta, expected, p.tolDB)
		}
	}
}

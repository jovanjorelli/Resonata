package engine

import (
	"math"
	"reflect"
	"testing"

	"resonata/pkg/score"
)

// gridScore builds a one-track score of n notes at (k+offset) beats,
// 4/4 at 120 BPM.
func gridScore(n int, offsetBeats float64, vel float32, artic string) *score.Score {
	const spb = 0.5 // 120 BPM
	s := &score.Score{
		Metadata: score.Metadata{Title: "H", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID: "t", Name: "T", Volume: 1,
			Instrument: score.InstrumentDef{Type: "ocarina"},
		}},
	}
	for k := 0; k < n; k++ {
		s.Tracks[0].Notes = append(s.Tracks[0].Notes, score.NoteEvent{
			Time: (float64(k) + offsetBeats) * spb, Duration: 0.2,
			Pitch: 69, Velocity: vel,
			Articulation: score.Articulation{Type: artic},
		})
	}
	return s
}

// TestHumanizeDistribution is the statistical proof: onset jitter is
// Gaussian with the specified sigma, clamped, and downbeats stay locked
// at exactly 0.0 ms deviation.
func TestHumanizeDistribution(t *testing.T) {
	const n = 20000
	in := gridScore(n, 0.5, 0.8, "") // off-beat grid: every note gets jitter
	out := Humanize(in, 1.0, 42)

	dts := make([]float64, n)
	var sum, sumSq, maxDev float64
	for k := 0; k < n; k++ {
		dt := out.Tracks[0].Notes[k].Time - in.Tracks[0].Notes[k].Time
		dts[k] = dt
		sum += dt
		sumSq += dt * dt
		if math.Abs(dt) > maxDev {
			maxDev = math.Abs(dt)
		}
	}
	mean := sum / n
	sd := math.Sqrt(sumSq/n - mean*mean)

	// Mean ~0 (5 standard errors) and std-dev = timingSigma within 4%.
	if math.Abs(mean) > 5*sd/math.Sqrt(n) {
		t.Errorf("jitter mean = %v s, want ~0", mean)
	}
	if math.Abs(sd-timingSigma)/timingSigma > 0.04 {
		t.Errorf("jitter std-dev = %v s, want %v ±4%%", sd, timingSigma)
	}
	if maxDev > maxJitter+1e-9 {
		t.Errorf("jitter clamp violated: max |dt| = %v > %v", maxDev, maxJitter)
	}

	// Gaussian CDF fractions at 0.5σ, 1σ, 1.5σ, 2σ (sampling error at
	// n=20000 is ~0.4%, so ±2% is a safe bound for a true Gaussian).
	frac := func(kSig float64) float64 {
		c := 0
		for _, dt := range dts {
			if math.Abs(dt) <= kSig*sd {
				c++
			}
		}
		return float64(c) / n
	}
	for _, tc := range []struct{ k, want float64 }{
		{0.5, 0.3829}, {1.0, 0.6827}, {1.5, 0.8664}, {2.0, 0.9545},
	} {
		if got := frac(tc.k); math.Abs(got-tc.want) > 0.02 {
			t.Errorf("|z| <= %gσ fraction = %v, want %v (Gaussian)", tc.k, got, tc.want)
		}
	}

	// Downbeat lock: bar starts must not move at all (the orchestra
	// locks together on beat 1).
	down := &score.Score{
		Metadata: score.Metadata{Title: "D", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID: "t", Name: "T", Volume: 1,
			Instrument: score.InstrumentDef{Type: "ocarina"},
			Notes: []score.NoteEvent{
				{Time: 0, Duration: 0.5, Pitch: 60, Velocity: 0.8},
				{Time: 2, Duration: 0.5, Pitch: 62, Velocity: 0.8}, // bar 2
				{Time: 4, Duration: 0.5, Pitch: 64, Velocity: 0.8}, // bar 3
				{Time: 6, Duration: 0.5, Pitch: 65, Velocity: 0.8}, // bar 4
			},
		}},
	}
	dout := Humanize(down, 1.0, 7)
	for k := range down.Tracks[0].Notes {
		if dt := dout.Tracks[0].Notes[k].Time - down.Tracks[0].Notes[k].Time; dt != 0 {
			t.Errorf("downbeat %d moved by %v s, want exactly 0", k, dt)
		}
	}
}

// TestHumanizeMetricVelocity checks the 4/4 accent table, the ±5%
// jitter, and that strength 0 is an exact pass-through.
func TestHumanizeMetricVelocity(t *testing.T) {
	const bars = 400
	in := &score.Score{
		Metadata: score.Metadata{Title: "V", BPM: 120, TimeSignature: "4/4"},
		Tracks: []score.Track{{
			ID: "t", Name: "T", Volume: 1,
			Instrument: score.InstrumentDef{Type: "ocarina"},
		}},
	}
	// Beats 0-3 of every bar plus the "and" of beat 2.
	for b := 0; b < bars; b++ {
		for _, beat := range []float64{0, 1, 2, 3, 1.5} {
			in.Tracks[0].Notes = append(in.Tracks[0].Notes, score.NoteEvent{
				Time: (float64(b)*4 + beat) * 0.5, Duration: 0.2,
				Pitch: 69, Velocity: 0.8,
			})
		}
	}
	out := Humanize(in, 1.0, 3)

	// Mean velocity ratio per slot must match the metric weights
	// (±5% jitter over 400 samples -> s.e. 0.25%, allow 1.5%).
	want := []float64{1.0, 0.85, 0.95, 0.8, 0.75}
	sums := make([]float64, 5)
	for k := range in.Tracks[0].Notes {
		slot := k % 5
		sums[slot] += float64(out.Tracks[0].Notes[k].Velocity) / 0.8
	}
	for slot, w := range want {
		got := sums[slot] / bars
		if math.Abs(got-w) > 0.015 {
			t.Errorf("slot %d: mean velocity ratio %v, want metric weight %v", slot, got, w)
		}
	}

	// Strength 0: bit-exact pass-through.
	zero := Humanize(in, 0, 3)
	for k := range in.Tracks[0].Notes {
		if !reflect.DeepEqual(zero.Tracks[0].Notes[k], in.Tracks[0].Notes[k]) {
			t.Fatalf("strength 0 altered note %d: %+v vs %+v", k,
				zero.Tracks[0].Notes[k], in.Tracks[0].Notes[k])
		}
	}
}

// TestHumanizeDurations checks the articulation-driven duration rules.
func TestHumanizeDurations(t *testing.T) {
	stats := func(artic string, base float64) (mean, sd, lo, hi float64) {
		in := gridScore(600, 0.5, 0.8, artic)
		for k := range in.Tracks[0].Notes {
			in.Tracks[0].Notes[k].Duration = base
		}
		out := Humanize(in, 1.0, 11)
		lo, hi = math.MaxFloat64, 0
		var sum, sumSq float64
		for k := range in.Tracks[0].Notes {
			r := out.Tracks[0].Notes[k].Duration / base
			sum += r
			sumSq += r * r
			if r < lo {
				lo = r
			}
			if r > hi {
				hi = r
			}
		}
		mean = sum / 600
		sd = math.Sqrt(sumSq/600 - mean*mean)
		return
	}

	// Staccato: ±10% Gaussian around the written length.
	m, s, lo, hi := stats("staccato", 0.4)
	if math.Abs(m-1) > 0.015 {
		t.Errorf("staccato mean ratio %v, want ~1.0", m)
	}
	if math.Abs(s-0.10) > 0.012 {
		t.Errorf("staccato ratio σ %v, want ~0.10", s)
	}
	if lo < 0.5 || hi > 1.5 {
		t.Errorf("staccato range [%v, %v], want within [0.5, 1.5]", lo, hi)
	}

	// Legato: uniform +5..15% extension for bowing overlap.
	m, s, lo, hi = stats("legato", 1.0)
	if m < 1.09 || m > 1.11 {
		t.Errorf("legato mean ratio %v, want ~1.10", m)
	}
	if s < 0.02 || s > 0.04 {
		t.Errorf("legato ratio σ %v, want ~0.029 (uniform 5-15%%)", s)
	}
	if lo < 1.05-1e-6 || hi > 1.15+1e-6 {
		t.Errorf("legato range [%v, %v], want within [1.05, 1.15]", lo, hi)
	}

	// Unarticulated notes still get subtle ±3% variation.
	m, s, _, _ = stats("", 0.5)
	if math.Abs(m-1) > 0.01 || math.Abs(s-0.03) > 0.006 {
		t.Errorf("default duration mean %v σ %v, want ~1.0 / ~0.03", m, s)
	}
}

// TestHumanizeDeterminism checks reproducibility, input immutability,
// per-track independence, and output validity.
func TestHumanizeDeterminism(t *testing.T) {
	s := &score.Score{
		Metadata: score.Metadata{Title: "T", BPM: 90, TimeSignature: "3/4"},
		Tracks: []score.Track{
			{
				ID: "a", Name: "A", Volume: 0.8, ReverbSend: 0.3,
				Instrument: score.InstrumentDef{Type: "ocarina",
					Parameters: map[string]float32{"breath_noise": 0.3}},
				Notes: []score.NoteEvent{
					{Time: 0.5, Duration: 0.4, Pitch: 69, Velocity: 0.8},
					{Time: 1.0, Duration: 0.4, Pitch: 72, Velocity: 0.7,
						Articulation: score.Articulation{Type: "legato",
							Params: map[string]interface{}{"x": 1.0}}},
				},
			},
			{
				ID: "b", Name: "B", Volume: 0.7,
				Instrument: score.InstrumentDef{Type: "ocarina"},
				Notes: []score.NoteEvent{
					{Time: 0.5, Duration: 0.4, Pitch: 60, Velocity: 0.8},
					{Time: 1.0, Duration: 0.4, Pitch: 64, Velocity: 0.7},
				},
			},
		},
	}
	before := *s
	beforeNotes := make([]score.NoteEvent, len(s.Tracks[0].Notes))
	copy(beforeNotes, s.Tracks[0].Notes)

	a := Humanize(s, 0.8, 99)
	b := Humanize(s, 0.8, 99)
	for ti := range a.Tracks {
		for ni := range a.Tracks[ti].Notes {
			if !reflect.DeepEqual(a.Tracks[ti].Notes[ni], b.Tracks[ti].Notes[ni]) {
				t.Fatalf("same seed diverged at track %d note %d", ti, ni)
			}
		}
	}
	c := Humanize(s, 0.8, 7)
	differ := false
	for ni := range a.Tracks[0].Notes {
		if !reflect.DeepEqual(a.Tracks[0].Notes[ni], c.Tracks[0].Notes[ni]) {
			differ = true
		}
	}
	if !differ {
		t.Error("different seeds produced identical takes")
	}
	// Per-track independence: identical source notes diverge between the
	// two tracks (independent players).
	indep := false
	for ni := range a.Tracks[0].Notes {
		if a.Tracks[0].Notes[ni].Time != a.Tracks[1].Notes[ni].Time {
			indep = true
		}
	}
	if !indep {
		t.Error("tracks share one random stream")
	}
	// The input score must be untouched, including nested maps.
	if s.Metadata != before.Metadata || len(s.Tracks) != len(before.Tracks) {
		t.Fatal("metadata/tracks mutated")
	}
	for ni := range beforeNotes {
		if !reflect.DeepEqual(s.Tracks[0].Notes[ni], beforeNotes[ni]) {
			t.Fatalf("input note %d mutated", ni)
		}
	}
	s.Tracks[0].Instrument.Parameters["breath_noise"] = 0.9
	if a.Tracks[0].Instrument.Parameters["breath_noise"] != 0.3 {
		t.Error("parameter map is shared with the input")
	}
	// The humanized take must still validate and render.
	if err := score.Validate(a); err != nil {
		t.Fatalf("humanized score invalid: %v", err)
	}
	if _, err := New(a, 48000, 1024); err != nil {
		t.Fatalf("humanized score does not build: %v", err)
	}
}

// TestHumanizePRNG checks the generator statistically and proves the
// hot path allocates nothing.
func TestHumanizePRNG(t *testing.T) {
	r := newHumanRNG(1)
	const n = 40000
	var sum, sumSq float64
	umin, umax := 1.0, 0.0
	for i := 0; i < n; i++ {
		u := r.uniform()
		if u < umin {
			umin = u
		}
		if u > umax {
			umax = u
		}
		g := r.gauss()
		sum += g
		sumSq += g * g
	}
	if umin < 0 || umax >= 1 {
		t.Errorf("uniform range [%v, %v], want [0, 1)", umin, umax)
	}
	mean := sum / n
	sd := math.Sqrt(sumSq/n - mean*mean)
	if math.Abs(mean) > 5/math.Sqrt(n) {
		t.Errorf("gauss mean = %v, want ~0", mean)
	}
	if math.Abs(sd-1) > 0.03 {
		t.Errorf("gauss std-dev = %v, want ~1", sd)
	}
	if allocs := testing.AllocsPerRun(2000, func() {
		_ = r.uniform()
		_ = r.gauss()
	}); allocs != 0 {
		t.Fatalf("PRNG allocates %v per call, want 0", allocs)
	}
}

func TestBeatsPerBar(t *testing.T) {
	for sig, want := range map[string]int{"4/4": 4, "3/4": 3, "6/8": 6, "2/2": 2, "": 4, "junk": 4, "0/4": 4} {
		if got := BeatsPerBar(sig); got != want {
			t.Errorf("BeatsPerBar(%q) = %d, want %d", sig, got, want)
		}
	}
}

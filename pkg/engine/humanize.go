package engine

import (
	"maps"
	"math"
	"strconv"
	"strings"

	"resonata/pkg/score"
)

// Humanizer constants. Strength in [0,1] scales every deviation; at 0 the
// score passes through untouched.
const (
	timingSigma   = 0.012 // onset jitter std-dev at strength 1 (12 ms)
	maxJitter     = 0.050 // hard clamp on |jitter| to keep note order sane
	velocityJit   = 0.05  // ±5% velocity deviation at strength 1
	staccatoJit   = 0.10  // staccato duration ±10% at strength 1
	legatoMin     = 0.05  // legato extension lower bound (bow overlap)
	legatoRange   = 0.10  // legato extension span: +5..15%
	defaultDurJit = 0.03  // subtle bow/breath variation for other notes
	minDuration   = 0.01  // keeps the score valid after scaling
	downbeatTol   = 1e-6  // beats: tolerance for "exactly on beat 1"
)

// humanRNG is a pre-seeded LCG (Knuth's MMIX constants) with a Box-Muller
// Gaussian transform on top. It is fast and allocation-free, unlike a
// shared math/rand source with its mutex and interface boxing.
type humanRNG struct {
	state    uint64
	spare    float64
	hasSpare bool
}

// newHumanRNG seeds a stream; seed 0 falls back to a golden-ratio constant.
func newHumanRNG(seed uint64) *humanRNG {
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	return &humanRNG{state: seed}
}

// uniform returns the next sample in [0,1) from the LCG's high bits.
func (r *humanRNG) uniform() float64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return float64(r.state>>11) / float64(uint64(1)<<53)
}

// gauss returns a standard-normal sample via Box-Muller:
//
//	Z0 = sqrt(-2·ln U1) · cos(2π·U2)
//
// The transform's sine pair is cached, so every log/cos computation
// yields two samples without allocation.
func (r *humanRNG) gauss() float64 {
	if r.hasSpare {
		r.hasSpare = false
		return r.spare
	}
	u1 := r.uniform()
	for u1 <= 1e-12 { // keep ln(u1) finite
		u1 = r.uniform()
	}
	u2 := r.uniform()
	mag := math.Sqrt(-2 * math.Log(u1))
	r.spare = mag * math.Sin(2*math.Pi*u2)
	r.hasSpare = true
	return mag * math.Cos(2*math.Pi*u2)
}

// Humanize returns a deep copy of s with the "MIDI robot" feel removed:
//
//   - micro-timing: Gaussian onset jitter (σ = 12 ms · strength), forced
//     to exactly 0.0 ms on downbeats so the orchestra locks together
//   - metric velocity: base velocity × the beat's natural accent weight
//     (4/4: 1.0 / 0.85 / 0.95 / 0.8, off-beats 0.75), blended toward
//     flat by strength, then ±5% · strength Gaussian jitter
//   - durations: staccato ±10%, legato extended 5-15% for bowing
//     overlap, other notes a subtle ±3% breath/bow variation
//
// Each track draws from its own deterministic stream (seeded by seed and
// the track index), so players deviate independently but a take is fully
// reproducible. Strength 0 returns an untouched copy. The result always
// satisfies score.Validate when the input does.
func Humanize(s *score.Score, strength float32, seed uint64) *score.Score {
	if s == nil {
		return nil
	}
	out := copyScore(s)
	st := clampF64(float64(strength), 0, 1)
	if st == 0 {
		return out
	}
	secPerBeat := 60 / s.Metadata.BPM
	if !(secPerBeat > 0) || math.IsInf(secPerBeat, 0) {
		return out // unplayable tempo: leave the score untouched
	}
	beats := float64(BeatsPerBar(s.Metadata.TimeSignature))

	for ti := range out.Tracks {
		rng := newHumanRNG(seed + uint64(ti)*0x9E3779B97F4A7C15)
		tr := &out.Tracks[ti]
		for ni := range tr.Notes {
			n := &tr.Notes[ni]

			// Metric position of the WRITTEN note (jitter must not feed
			// back into the accent analysis).
			beatPos := n.Time / secPerBeat
			beatInBar := beatPos - math.Floor(beatPos/beats)*beats
			onDownbeat := beatInBar < downbeatTol || beats-beatInBar < downbeatTol

			// Micro-timing: downbeats stay locked, everything else
			// breathes.
			if !onDownbeat {
				jitter := clampF64(rng.gauss()*timingSigma*st, -maxJitter, maxJitter)
				n.Time = math.Max(0, n.Time+jitter)
			}

			// Metric-aware velocity with ±5% organic jitter.
			w := metricWeight(beatInBar, int(beats))
			w = 1 + (w-1)*st
			v := float64(n.Velocity) * w * (1 + velocityJit*st*rng.gauss())
			n.Velocity = float32(clampF64(v, 0, 1))

			// Articulation-driven durations (breath/bow changes).
			orig := n.Duration
			dur := orig
			switch strings.ToLower(n.Articulation.Type) {
			case "staccato":
				dur *= 1 + staccatoJit*st*rng.gauss()
			case "legato":
				dur *= 1 + st*(legatoMin+legatoRange*rng.uniform())
			default:
				dur *= 1 + defaultDurJit*st*rng.gauss()
			}
			n.Duration = clampF64(dur, minDuration, orig*4)
		}
	}
	return out
}

// metricWeight returns the natural accent weight of a beat position: in
// 4/4 beat 1 = 1.0, beat 2 = 0.85, beat 3 = 0.95, beat 4 = 0.8; the "and"
// off-beats weigh 0.75 and finer subdivisions 0.7. Other meters follow
// the same strong/medium/weak logic (downbeat 1.0, midpoint 0.95).
func metricWeight(beatInBar float64, beats int) float64 {
	if beats < 1 {
		beats = 4
	}
	whole := math.Round(beatInBar)
	frac := math.Abs(beatInBar - whole)
	idx := int(whole)
	if idx >= beats {
		idx = 0 // e.g. beatInBar 3.999... rounding to 4 in 4/4
	}
	switch {
	case frac > 0.4 && frac < 0.6:
		return 0.75 // the "and" of a beat
	case frac > 0.25:
		return 0.7 // finer subdivisions
	}
	switch beats {
	case 4:
		return [4]float64{1.0, 0.85, 0.95, 0.8}[idx]
	case 3:
		return [3]float64{1.0, 0.85, 0.95}[idx]
	case 2:
		return [2]float64{1.0, 0.85}[idx]
	}
	if idx == 0 {
		return 1.0
	}
	if idx == beats/2 {
		return 0.95
	}
	return 0.85
}

// BeatsPerBar extracts the numerator of a "N/D" time signature,
// defaulting to 4.
func BeatsPerBar(timeSignature string) int {
	num, _, ok := strings.Cut(timeSignature, "/")
	if !ok {
		return 4
	}
	n, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil || n < 1 {
		return 4
	}
	return n
}

// copyScore deep-copies a score so Humanize never mutates its input.
func copyScore(s *score.Score) *score.Score {
	out := &score.Score{Metadata: s.Metadata}
	out.Tracks = make([]score.Track, len(s.Tracks))
	for i, tr := range s.Tracks {
		cp := tr
		cp.Instrument.Parameters = maps.Clone(tr.Instrument.Parameters)
		cp.Notes = make([]score.NoteEvent, len(tr.Notes))
		copy(cp.Notes, tr.Notes)
		for j := range cp.Notes {
			cp.Notes[j].Articulation.Params = maps.Clone(cp.Notes[j].Articulation.Params)
		}
		out.Tracks[i] = cp
	}
	return out
}

// clampF64 bounds v to [lo, hi].
func clampF64(v, lo, hi float64) float64 {
	switch {
	case v < lo || v != v:
		return lo
	case v > hi:
		return hi
	}
	return v
}

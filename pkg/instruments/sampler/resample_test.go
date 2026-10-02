package sampler

import (
	"math"
	"math/rand"
	"testing"
)

// TestSplineAliasing is the fidelity contract: on a high-frequency tone
// resampled at a non-integer rate, the cubic Hermite spline's error
// against the analytic truth — and its curvature (kink) content — must
// be dramatically below linear interpolation, while its amplitude stays
// true. That is the interpolation-error (imaging) floor that makes
// linear resampling sound dull and aliased.
func TestSplineAliasing(t *testing.T) {
	const sr = 48000
	const f0 = 6000             // Hz, well below Nyquist, rich enough to expose error
	rate := math.Pow(2, 7.0/12) // +7 semitones = 1.4983 (non-integer)

	src := make([]float32, sr)
	for i := range src {
		src[i] = float32(math.Sin(2 * math.Pi * f0 * float64(i) / sr))
	}
	n := int(float64(sr)/rate) - 2
	cub := make([]float32, n)
	lin := make([]float32, n)
	if nc := ResampleTo(cub, src, rate, 0); nc != n {
		t.Fatalf("cubic wrote %d frames, want %d", nc, n)
	}
	if nl := ResampleLinearTo(lin, src, rate, 0); nl != n {
		t.Fatalf("linear wrote %d frames, want %d", nl, n)
	}

	// Analytic truth: the spline output should be the sine sampled at
	// phase m·rate.
	truth := make([]float64, n)
	for m := range truth {
		truth[m] = math.Sin(2 * math.Pi * f0 * (float64(m) * rate) / sr)
	}
	rmsErr := func(out []float32) float64 {
		var s float64
		for m := 0; m < n; m++ {
			d := float64(out[m]) - truth[m]
			s += d * d
		}
		return math.Sqrt(s / float64(n))
	}
	// Curvature error: linear interpolation is piecewise straight, so its
	// second difference is impulse noise at the knots; the C1 spline
	// tracks the true curvature.
	curvErr := func(out []float32) float64 {
		var s float64
		for m := 1; m+1 < n; m++ {
			got := float64(out[m+1]) - 2*float64(out[m]) + float64(out[m-1])
			want := truth[m+1] - 2*truth[m] + truth[m-1]
			s += (got - want) * (got - want)
		}
		return math.Sqrt(s / float64(n))
	}

	errC, errL := rmsErr(cub), rmsErr(lin)
	if errC >= errL/5 {
		t.Fatalf("rms error: cubic %v vs linear %v, want cubic < linear/5", errC, errL)
	}
	curvC, curvL := curvErr(cub), curvErr(lin)
	if curvC >= curvL/3 {
		t.Fatalf("curvature error: cubic %v vs linear %v, want cubic < linear/3", curvC, curvL)
	}
	t.Logf("rms error cubic %.5f vs linear %.5f (%.0fx better); curvature %.2e vs %.2e",
		errC, errL, errL/errC, curvC, curvL)

	// Amplitude truth: the spline holds the 9 kHz shifted tone at full
	// scale; linear interpolation droops.
	peak := func(out []float32) float64 {
		p := 0.0
		for _, v := range out {
			if a := math.Abs(float64(v)); a > p {
				p = a
			}
		}
		return p
	}
	pc, pl := peak(cub), peak(lin)
	if math.Abs(pc-1) > 0.01 {
		t.Errorf("cubic peak = %v, want ~1.0 (no droop)", pc)
	}
	if math.Abs(pc-1) >= math.Abs(pl-1) {
		t.Errorf("cubic peak error %v not better than linear %v", math.Abs(pc-1), math.Abs(pl-1))
	}
}

// TestCubicPointFormula checks the FMA/Horner implementation against the
// literal coefficient formulas and the spline's exact reproduction
// properties.
func TestCubicPointFormula(t *testing.T) {
	naive := func(p0, p1, p2, p3, x float64) float64 {
		c0 := p1
		c1 := (p2 - p0) / 2
		c2 := p0 - 2.5*p1 + 2*p2 - 0.5*p3
		c3 := 1.5*p1 - 0.5*p0 + 0.5*p3 - 1.5*p2
		return c0 + c1*x + c2*x*x + c3*x*x*x
	}
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 2000; i++ {
		p0 := rng.Float64()*4 - 2
		p1 := rng.Float64()*4 - 2
		p2 := rng.Float64()*4 - 2
		p3 := rng.Float64()*4 - 2
		x := rng.Float64()
		got := cubicPoint(p0, p1, p2, p3, x)
		want := naive(p0, p1, p2, p3, x)
		if math.Abs(got-want) > 1e-12*(1+math.Abs(want)) {
			t.Fatalf("cubicPoint(%v,%v,%v,%v,%v) = %v, want %v", p0, p1, p2, p3, x, got, want)
		}
	}
	// Endpoint interpolation: x=0 -> p1, x=1 -> p2, exactly.
	if y := cubicPoint(-1, 0.25, 0.75, 2, 0); y != 0.25 {
		t.Errorf("x=0 gave %v, want p1", y)
	}
	if y := cubicPoint(-1, 0.25, 0.75, 2, 1); math.Abs(y-0.75) > 1e-12 {
		t.Errorf("x=1 gave %v, want p2", y)
	}
	// Constants reproduce exactly.
	for _, x := range []float64{0, 0.3, 0.5, 1} {
		if y := cubicPoint(0.3, 0.3, 0.3, 0.3, x); math.Abs(y-0.3) > 1e-15 {
			t.Errorf("constant input gave %v at x=%v", y, x)
		}
	}
	// Linear ramps reproduce exactly: p = 0,1,2,3 -> y = 1+x.
	for _, x := range []float64{0, 0.25, 0.5, 0.75, 1} {
		if y := cubicPoint(0, 1, 2, 3, x); math.Abs(y-(1+x)) > 1e-12 {
			t.Errorf("ramp at x=%v gave %v, want %v", x, y, 1+x)
		}
	}
	// Quadratics reproduce exactly (Catmull-Rom is exact for degree 2):
	// samples of y = (1+x)² at x = -1,0,1,2 are p = 0,1,4,9.
	for _, x := range []float64{0, 0.3, 0.7, 1} {
		got := cubicPoint(0, 1, 4, 9, x)
		want := (1 + x) * (1 + x)
		if math.Abs(got-want) > 1e-12 {
			t.Errorf("quadratic at x=%v gave %v, want %v", x, got, want)
		}
	}
}

// TestResampleBoundaries covers edge clamping, exact integer phases,
// termination, step-overshoot bounds, and degenerate buffers.
func TestResampleBoundaries(t *testing.T) {
	// Unit rate at integer phases copies the source exactly (frac = 0
	// returns p1 through the Horner form).
	src := make([]float32, 64)
	for i := range src {
		src[i] = float32(math.Sin(float64(i) * 0.3))
	}
	dst := make([]float32, 64)
	if n := ResampleTo(dst, src, 1, 0); n != 64 {
		t.Fatalf("unit-rate wrote %d frames", n)
	}
	for i := range src {
		if dst[i] != src[i] {
			t.Fatalf("frame %d = %v, want exact copy %v", i, dst[i], src[i])
		}
	}

	// Start phase offsets the read position.
	dst2 := make([]float32, 8)
	ResampleTo(dst2, src, 1, 5.5)
	mid := float64(interpSample(src, 5.5))
	if math.Abs(float64(dst2[0])-mid) > 1e-6 {
		t.Errorf("startPhase ignored: %v vs %v", dst2[0], mid)
	}

	// Termination at the source end.
	short := make([]float32, 1000)
	n := ResampleTo(short, src, 0.25, 0) // 64 frames / 0.25 = 256 outputs
	if n != 256 {
		t.Errorf("slow rate wrote %d frames, want 256", n)
	}

	// Step response: bounded overshoot (Catmull-Rom rings at most ~8%
	// past a unit step) and no NaNs anywhere near the clamped edges.
	step := []float32{0, 0, 0, 1, 1, 1}
	out := make([]float32, 12)
	ResampleTo(out, step, 0.5, 0)
	for i, v := range out {
		if math.IsNaN(float64(v)) || v < -0.2 || v > 1.2 {
			t.Fatalf("step response frame %d = %v out of bounds", i, v)
		}
	}

	// Degenerate buffers clamp instead of panicking: a 1-frame source
	// yields exactly one output frame (phase 0), a 2-frame source spans
	// phases 0..1.5 at rate 0.5.
	one := []float32{0.5}
	small := make([]float32, 4)
	if k := ResampleTo(small, one, 1, 0); k != 1 {
		t.Fatalf("single-sample src wrote %d frames, want 1", k)
	}
	if small[0] != 0.5 {
		t.Fatalf("single-sample clamp gave %v", small[0])
	}
	two := []float32{0.25, 0.75}
	if k := ResampleTo(small, two, 0.5, 0); k != 4 {
		t.Fatalf("two-sample src wrote %d, want 4", k)
	}
	for _, v := range small {
		if math.IsNaN(float64(v)) || v < -0.2 || v > 1.2 {
			t.Fatalf("two-sample clamp produced %v", v)
		}
	}
	if k := ResampleTo(small, nil, 1, 0); k != 0 {
		t.Fatal("empty src should write nothing")
	}
	if k := ResampleTo(small, src, 0, 0); k != 0 {
		t.Fatal("zero rate should write nothing")
	}
}

// TestResampleZeroAlloc pins the hot-path contract.
func TestResampleZeroAlloc(t *testing.T) {
	src := make([]float32, 4096)
	for i := range src {
		src[i] = float32(i%97) / 97
	}
	dst := make([]float32, 2048)
	ResampleTo(dst, src, 2.03, 0) // warm
	if allocs := testing.AllocsPerRun(100, func() {
		ResampleTo(dst, src, 2.03, 0)
	}); allocs != 0 {
		t.Fatalf("ResampleTo allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		_ = interpSample(src, 123.456)
	}); allocs != 0 {
		t.Fatalf("interpSample allocates %v per call, want 0", allocs)
	}
}

// TestVoiceUsesSpline checks the playback engine actually routes through
// the spline: a curved source resampled by a voice must beat the linear
// reference in accuracy.
func TestVoiceUsesSpline(t *testing.T) {
	const sr = 48000
	// Source: a 5 kHz sine (strongly curved between taps).
	src := make([]float32, sr/2)
	for i := range src {
		src[i] = float32(math.Sin(2 * math.Pi * 5000 * float64(i) / sr))
	}
	rg := newRegion()
	rg.SamplePath = "curve.wav"
	rg.PitchKeyCenter = 69
	rg.Sample = &SampleData{Samples: src, SampleRate: sr, Channels: 1}
	rg.Transpose = 5 // rate 2^(5/12) = 1.3348
	s := New(sr)
	s.Load(&SFZFile{Regions: []Region{rg}})
	s.NoteOn(69, 1.0)

	buf := make([]float32, 4096)
	// Envelope is flat after the 5 ms attack: skip it, then capture.
	tmp := make([]float32, 512)
	s.voices[0].Process(tmp, 512.0/sr)
	s.voices[0].Process(buf, 4096.0/sr)

	// Truth for the captured window: phase resumed at 512·rate.
	rate := math.Pow(2, 5.0/12)
	startPhase := 512 * rate
	errVoice, errLin := 0.0, 0.0
	lin := make([]float32, 4096)
	ResampleLinearTo(lin, src, rate, startPhase)
	for m := 0; m < 4096; m++ {
		truth := math.Sin(2 * math.Pi * 5000 * ((startPhase + float64(m)*rate) / sr))
		dv := float64(buf[m]) - truth
		dl := float64(lin[m]) - truth
		errVoice += dv * dv
		errLin += dl * dl
	}
	if errVoice >= errLin/4 {
		t.Fatalf("voice interpolation error %v not clearly below linear %v: spline not engaged?",
			math.Sqrt(errVoice), math.Sqrt(errLin))
	}
}

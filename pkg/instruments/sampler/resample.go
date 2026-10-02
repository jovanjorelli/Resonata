package sampler

import "math"

// High-fidelity resampling: 4-point, 3rd-order cubic Hermite
// (Catmull-Rom) spline interpolation. Compared with linear interpolation
// the spline is C1-continuous and reproduces quadratic trends exactly,
// cutting the interpolation error floor by roughly an order of magnitude:
// pitched playback keeps crisp transients instead of the kinks and
// imaging that make linear resampling sound dull.
//
// Given four consecutive samples p0..p3 and the fractional position x in
// [0,1] between p1 and p2:
//
//	y(x) = c0 + c1·x + c2·x² + c3·x³
//	c0 = p1
//	c1 = (p2 − p0) / 2
//	c2 = p0 − 5p1/2 + 2p2 − p3/2
//	c3 = 3p1/2 − p0/2 + p3/2 − 3p2/2
//
// evaluated with Horner's method on math.Fma chains so the hot loop maps
// onto fused multiply-add instructions where the platform provides them.
// Every helper works on scalars and caller-owned slices: the resampling
// path performs zero allocations.

// cubicPoint evaluates the Catmull-Rom spline (coefficients above) via
// Horner's method: y = ((c3·x + c2)·x + c1)·x + c0.
func cubicPoint(p0, p1, p2, p3, x float64) float64 {
	c1 := 0.5 * (p2 - p0)
	c2 := math.FMA(-2.5, p1, math.FMA(2, p2, math.FMA(-0.5, p3, p0)))
	c3 := math.FMA(1.5, p1-p2, 0.5*(p3-p0))
	return math.FMA(math.FMA(math.FMA(c3, x, c2), x, c1), x, p1)
}

// readSample fetches src[i] with edge clamping, supplying the spline's
// p0/p3 neighbours when the phase is near the start (index 0 or 1) or
// the end of the buffer.
func readSample(src []float32, i int) float64 {
	if i < 0 {
		i = 0
	} else if i >= len(src) {
		i = len(src) - 1
	}
	return float64(src[i])
}

// readSampleBounded fetches src[i] clamped to the half-open frame window
// [lo, hi): taps outside the window replicate the edge frame. lo is
// inclusive, hi is exclusive; out-of-range windows fall back to src
// bounds. Used for loop-crossfade reads so the interpolation stencil
// never reaches outside the loop region.
func readSampleBounded(src []float32, i, lo, hi int) float64 {
	if hi <= lo {
		return readSample(src, i)
	}
	if i < lo {
		i = lo
	} else if i >= hi {
		i = hi - 1
	}
	if i < 0 {
		i = 0
	} else if i >= len(src) {
		i = len(src) - 1
	}
	return float64(src[i])
}

// interpSample returns the spline-interpolated source value at phase
// (measured in source frames). The caller guarantees 0 <= phase <
// len(src); boundary taps clamp.
func interpSample(src []float32, phase float64) float64 {
	index := int(phase)
	frac := phase - float64(index)
	return cubicPoint(
		readSample(src, index-1),
		readSample(src, index),
		readSample(src, index+1),
		readSample(src, index+2),
		frac)
}

// interpSampleBounded is the loop-aware spline read: the p0..p3 taps
// clamp to [lo, hi) so crossfade streams near the loop edges never read
// outside the loop region. lo is inclusive, hi exclusive.
func interpSampleBounded(src []float32, phase float64, lo, hi int) float64 {
	index := int(phase)
	frac := phase - float64(index)
	return cubicPoint(
		readSampleBounded(src, index-1, lo, hi),
		readSampleBounded(src, index, lo, hi),
		readSampleBounded(src, index+1, lo, hi),
		readSampleBounded(src, index+2, lo, hi),
		frac)
}

// interpSampleLinear is the legacy two-point reference, kept for A/B
// artifact measurements in unit tests.
func interpSampleLinear(src []float32, phase float64) float64 {
	index := int(phase)
	frac := phase - float64(index)
	s1 := readSample(src, index)
	s2 := readSample(src, index+1)
	return s1 + (s2-s1)*frac
}

// ResampleTo renders src into dst at the given playback rate (source
// frames advanced per output frame), starting at startPhase, using the
// cubic Hermite spline. It stops at the source end or when dst is full
// and returns the number of frames written. No allocation.
func ResampleTo(dst []float32, src []float32, rate, startPhase float64) int {
	return resampleTo(dst, src, rate, startPhase, false)
}

// ResampleLinearTo is the linear-interpolation variant of ResampleTo,
// the A/B reference for artifact measurements.
func ResampleLinearTo(dst []float32, src []float32, rate, startPhase float64) int {
	return resampleTo(dst, src, rate, startPhase, true)
}

// resampleTo is the shared block resampler.
func resampleTo(dst []float32, src []float32, rate, startPhase float64, linear bool) int {
	if len(src) == 0 || !(rate > 0) || math.IsNaN(startPhase) {
		return 0
	}
	phase := startPhase
	if phase < 0 {
		phase = 0
	}
	end := float64(len(src))
	written := 0
	for written < len(dst) && phase < end {
		if linear {
			dst[written] = float32(interpSampleLinear(src, phase))
		} else {
			dst[written] = float32(interpSample(src, phase))
		}
		written++
		phase += rate
	}
	return written
}

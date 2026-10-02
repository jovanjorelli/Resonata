package dsp

import (
	"math"
	"testing"
)

// impulseResponse drives the delay with a unit impulse and records wet
// output for total frames.
func impulseResponse(d *StereoDelay, total int) ([]float32, []float32) {
	outL := make([]float32, total)
	outR := make([]float32, total)
	outL[0], outR[0] = d.ProcessFrameWet(1, 0)
	for i := 1; i < total; i++ {
		outL[i], outR[i] = d.ProcessFrameWet(0, 0)
	}
	return outL, outR
}

func peakAt(b []float32, idx int) float32 {
	if idx < 0 || idx >= len(b) {
		return 0
	}
	return b[idx]
}

// TestDelayBPMSync checks the musical time formula to plus or minus one
// sample across subdivisions and tempos.
func TestDelayBPMSync(t *testing.T) {
	const sr = 48000
	cases := []struct {
		bpm  float64
		sub  Subdivision
		want int
	}{
		{120, SubdivisionQuarter, 24000},
		{120, SubdivisionEighth, 12000},
		{120, SubdivisionEighthD, 18000},
		{120, SubdivisionSixteenth, 6000},
		{100, SubdivisionQuarter, 28800},
		{90, SubdivisionEighthD, 24000},
	}
	for _, c := range cases {
		got := DelaySamplesFor(c.bpm, c.sub, sr)
		if got < c.want-1 || got > c.want+1 {
			t.Errorf("bpm %.0f sub %v: got %d samples, want %d +-1", c.bpm, c.sub, got, c.want)
		}
	}
	// Sixteenth triplet is one third of a beat.
	if got := DelaySamplesFor(120, SubdivisionSixteenthT, sr); got < 7999 || got > 8001 {
		t.Errorf("sixteenth triplet = %d samples, want 8000 +-1", got)
	}
	// Configure resolves subdivisions against BPM identically.
	d := NewStereoDelay(sr, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModeStereo, Subdivision: SubdivisionEighthD, Feedback: 0.3, DampingHz: 4000, Wet: 0.5}, 120)
	if d.LengthSamples() < 17999 || d.LengthSamples() > 18001 {
		t.Fatalf("configured length = %d, want 18000 +-1", d.LengthSamples())
	}
	// Manual seconds override wins over the subdivision.
	d.Configure(DelayConfig{DelaySeconds: 0.25, Feedback: 0.3, DampingHz: 4000, Wet: 0.5}, 120)
	if d.LengthSamples() != 12000 {
		t.Fatalf("manual override length = %d, want 12000", d.LengthSamples())
	}
}

// TestDelaySubdivisionNames checks the score DSL vocabulary.
func TestDelaySubdivisionNames(t *testing.T) {
	for name, want := range map[string]Subdivision{
		"1/1": SubdivisionWhole, "1/2": SubdivisionHalf, "1/4": SubdivisionQuarter,
		"1/8": SubdivisionEighth, "1/8d": SubdivisionEighthD,
		"1/16": SubdivisionSixteenth, "1/16t": SubdivisionSixteenthT,
	} {
		got, ok := ParseSubdivision(name)
		if !ok || got != want {
			t.Errorf("ParseSubdivision(%q) = %v/%v, want %v/true", name, got, ok, want)
		}
	}
	if _, ok := ParseSubdivision("1/32"); ok {
		t.Error("unknown subdivision accepted")
	}
}

// TestDelayStereoEcho checks discrete repeats at multiples of the tap
// with geometric feedback decay on the same channel.
func TestDelayStereoEcho(t *testing.T) {
	const sr = 48000
	d := NewStereoDelay(sr, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModeStereo, DelaySeconds: 0.1, Feedback: 0.5, DampingHz: 20000, Wet: 1}, 120)
	length := d.LengthSamples()
	if length != 4800 {
		t.Fatalf("length = %d, want 4800", length)
	}
	outL, outR := impulseResponse(d, length*4+8)
	// Damping scales each tap by (1-alpha): the first tap holds the
	// damped impulse and later taps decay geometrically through the
	// loop gain. Between taps the damper state relaxes toward zero.
	a := float64(DampingCoeff(20000, sr))
	tap1 := (1 - a)
	tap2 := tap1 * 0.5 * (1 - a)
	if got := peakAt(outL, length); math.Abs(float64(got)-tap1) > 0.02 {
		t.Errorf("first echo = %v, want near %v", got, tap1)
	}
	if got := peakAt(outL, 2*length); math.Abs(float64(got)-tap2) > 0.02 {
		t.Errorf("second echo = %v, want near %v", got, tap2)
	}
	// Stereo mode never crosses channels for a left-only impulse.
	for i, v := range outR {
		if absF32(v) > 1e-6 {
			t.Fatalf("right leaked %v at frame %d in stereo mode", v, i)
		}
	}
}

// TestDelayPingPong checks the cross-feedback matrix: a left impulse
// replays on the same channel at one tap, then bounces right at two
// taps, left at three, alternating across the stereo image.
func TestDelayPingPong(t *testing.T) {
	const sr = 48000
	d := NewStereoDelay(sr, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModePingPong, DelaySeconds: 0.05, Feedback: 0.7, DampingHz: 20000, Wet: 1}, 120)
	length := d.LengthSamples()
	outL, outR := impulseResponse(d, length*5+8)
	a := float64(DampingCoeff(20000, sr))
	tap1 := (1 - a)
	tap2 := tap1 * 0.7 * (1 - a)
	tap3 := tap2 * 0.7 * (1 - a)
	// Tap 1 replays left, tap 2 bounces right, tap 3 returns left.
	if got := peakAt(outL, length); math.Abs(float64(got)-tap1) > 0.02 {
		t.Errorf("pong tap1 L = %v, want near %v", got, tap1)
	}
	if got := peakAt(outR, length); absF32(got) > 1e-5 {
		t.Errorf("ping tap1 R = %v, want silence", got)
	}
	if got := peakAt(outR, 2*length); math.Abs(float64(got)-tap2) > 0.02 {
		t.Errorf("ping tap2 R = %v, want near %v", got, tap2)
	}
	if got := peakAt(outL, 2*length); absF32(got) > 1e-5 {
		t.Errorf("pong tap2 L = %v, want silence", got)
	}
	if got := peakAt(outL, 3*length); math.Abs(float64(got)-tap3) > 0.03 {
		t.Errorf("pong tap3 L = %v, want near %v", got, tap3)
	}
}

// TestDelayDamping checks analog warmth: stronger damping (lower cutoff)
// darkens repeats, measured as a lower sample-to-sample difference ratio
// on late echoes excited by broadband noise.
func TestDelayDamping(t *testing.T) {
	const sr = 48000
	hfRatio := func(fc float32) float64 {
		d := NewStereoDelay(sr, MaxDelaySeconds)
		d.Configure(DelayConfig{Mode: DelayModeStereo, DelaySeconds: 0.11, Feedback: 0.55, DampingHz: fc, Wet: 1}, 120)
		noise := NewNoise(9)
		total := sr * 2
		out := make([]float32, total)
		for i := 0; i < total; i++ {
			in := float32(0)
			if i < 2400 {
				in = noise.Next() * 0.8
			}
			wl, _ := d.ProcessFrameWet(in, in)
			out[i] = wl
		}
		diff := func(from, to int) float64 {
			var num, den float64
			for i := from; i < to-1; i++ {
				d := float64(out[i+1] - out[i])
				num += d * d
				den += float64(out[i]) * float64(out[i])
			}
			return math.Sqrt(num/float64(to-from-1)+1e-18) / (math.Sqrt(den/float64(to-from-1)) + 1e-12)
		}
		return diff(int(0.5*sr), int(0.7*sr))
	}
	bright := hfRatio(12000)
	dark := hfRatio(2500)
	if dark >= bright {
		t.Errorf("damping has no effect: dark HF ratio %v >= bright %v", dark, bright)
	}
	// Damping coefficient follows alpha = exp(-2*pi*fc/fs).
	if got := DampingCoeff(4000, sr); math.Abs(float64(got)-math.Exp(-2*math.Pi*4000/sr)) > 1e-6 {
		t.Errorf("damping coeff = %v, want exp formula", got)
	}
}

// TestDelayWetDryMix checks the mix law on the first dry frame and the
// planar/interleaved helpers against per-frame processing.
func TestDelayWetDryMix(t *testing.T) {
	const sr = 48000
	d := NewStereoDelay(sr, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModeStereo, DelaySeconds: 0.1, Feedback: 0, DampingHz: 8000, Wet: 0.4}, 120)
	// Empty network: first frame out = (1-wet)*in.
	l, r := d.ProcessFrame(0.5, -0.25)
	if math.Abs(float64(l)-0.3) > 1e-6 || math.Abs(float64(r)+0.15) > 1e-6 {
		t.Fatalf("dry mix = %v/%v, want 0.3/-0.15", l, r)
	}
	a := NewStereoDelay(sr, MaxDelaySeconds)
	b := NewStereoDelay(sr, MaxDelaySeconds)
	cfg := DelayConfig{Mode: DelayModePingPong, DelaySeconds: 0.07, Feedback: 0.4, DampingHz: 5000, Wet: 0.6}
	a.Configure(cfg, 120)
	b.Configure(cfg, 120)
	buf := NewStereoBuffer(64)
	buf.SetLen(64)
	for i := 0; i < 64; i++ {
		v := float32(math.Sin(float64(i) * 0.07))
		buf.Left[i], buf.Right[i] = v, v*0.5
	}
	a.ProcessStereo(buf)
	for i := 0; i < 64; i++ {
		v := float32(math.Sin(float64(i) * 0.07))
		el, er := b.ProcessFrame(v, v*0.5)
		if buf.Left[i] != el || buf.Right[i] != er {
			t.Fatalf("frame %d: stereo %v/%v != frame %v/%v", i, buf.Left[i], buf.Right[i], el, er)
		}
	}
	// Interleaved helper matches as well.
	c := NewStereoDelay(sr, MaxDelaySeconds)
	c.Configure(cfg, 120)
	inter := make([]float32, 128)
	for i := 0; i < 64; i++ {
		v := float32(math.Sin(float64(i) * 0.07))
		inter[2*i], inter[2*i+1] = v, v*0.5
	}
	c.ProcessInterleaved(inter)
	for i := 0; i < 64; i++ {
		if inter[2*i] != buf.Left[i] || inter[2*i+1] != buf.Right[i] {
			t.Fatalf("frame %d: interleaved mismatch", i)
		}
	}
}

// TestDelayReset checks state clearing.
func TestDelayReset(t *testing.T) {
	d := NewStereoDelay(48000, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModePingPong, DelaySeconds: 0.02, Feedback: 0.6, DampingHz: 4000, Wet: 1}, 120)
	d.ProcessFrameWet(1, 1)
	for i := 0; i < 3000; i++ {
		d.ProcessFrameWet(0, 0)
	}
	d.Reset()
	n := d.LengthSamples() + 8
	for i := 0; i < n; i++ {
		if l, r := d.ProcessFrameWet(0, 0); l != 0 || r != 0 {
			t.Fatalf("frame %d after Reset: %v/%v, want silence", i, l, r)
		}
	}
}

// TestDelayConfigClamp checks feedback, wet, and length clamping.
func TestDelayConfigClamp(t *testing.T) {
	d := NewStereoDelay(48000, 1.0)
	d.Configure(DelayConfig{Feedback: 5, Wet: 5, DelaySeconds: 99}, 120)
	if d.Feedback() != 0.98 {
		t.Errorf("feedback = %v, want clamp 0.98", d.Feedback())
	}
	if d.Wet() != 1 {
		t.Errorf("wet = %v, want clamp 1", d.Wet())
	}
	if d.LengthSamples() >= 65536 {
		t.Errorf("length = %d, want below power-of-two capacity", d.LengthSamples())
	}
	d.SetDelaySamples(-4)
	if d.LengthSamples() != 0 {
		t.Errorf("negative length = %d, want 0", d.LengthSamples())
	}
}

// TestDelayZeroAlloc checks the hot path performs no allocations.
func TestDelayZeroAlloc(t *testing.T) {
	d := NewStereoDelay(48000, MaxDelaySeconds)
	d.Configure(DelayConfig{Mode: DelayModePingPong, Subdivision: SubdivisionEighthD, Feedback: 0.6, DampingHz: 4000, Wet: 0.4}, 120)
	buf := NewStereoBuffer(256)
	buf.SetLen(256)
	for i := range buf.Left {
		buf.Left[i] = float32(i%13) / 13
		buf.Right[i] = buf.Left[i] * 0.5
	}
	d.ProcessStereo(buf) // warm
	if allocs := testing.AllocsPerRun(50, func() { d.ProcessStereo(buf) }); allocs != 0 {
		t.Fatalf("ProcessStereo allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() { d.ProcessFrameWet(0.1, 0.1) }); allocs != 0 {
		t.Fatalf("ProcessFrameWet allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() { d.ProcessFrame(0.1, 0.1) }); allocs != 0 {
		t.Fatalf("ProcessFrame allocates %v per call, want 0", allocs)
	}
	inter := make([]float32, 512)
	if allocs := testing.AllocsPerRun(50, func() { d.ProcessInterleaved(inter) }); allocs != 0 {
		t.Fatalf("ProcessInterleaved allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(20, func() {
		d.Configure(DelayConfig{Mode: DelayModeStereo, DelaySeconds: 0.3, Feedback: 0.5, DampingHz: 4000, Wet: 0.5}, 120)
	}); allocs != 0 {
		t.Fatalf("Configure allocates %v per call, want 0", allocs)
	}
}

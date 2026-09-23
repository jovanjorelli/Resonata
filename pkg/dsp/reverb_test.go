package dsp

import (
	"math"
	"testing"
)

// renderImpulse drives the reverb with a single unit impulse and returns
// the wet output for total frames.
func renderImpulse(r *Reverb, total int) ([]float32, []float32) {
	outL := make([]float32, total)
	outR := make([]float32, total)
	outL[0], outR[0] = r.ProcessFrameWet(1, 1)
	for i := 1; i < total; i++ {
		outL[i], outR[i] = r.ProcessFrameWet(0, 0)
	}
	return outL, outR
}

func rmsF(b []float32) float64 {
	var s float64
	for _, v := range b {
		s += float64(v) * float64(v)
	}
	if len(b) == 0 {
		return 0
	}
	return math.Sqrt(s / float64(len(b)))
}

func peakF(b []float32) float32 {
	p := float32(0)
	for _, v := range b {
		if a := absF32(v); a > p {
			p = a
		}
	}
	return p
}

// TestReverbDensity verifies the lush-tail contract: silence during the
// pre-delay, a dense cloud of reflections (many peaks, no gaps), a
// smoothly decaying envelope (stable, no buildup), and a decorrelated
// stereo tail.
func TestReverbDensity(t *testing.T) {
	const sr = 48000
	r := NewReverb(sr, ReverbParams{RoomSize: 0.85, Damping: 0.25, Wet: 1, Dry: 0, Width: 1})
	const total = sr * 3
	outL, outR := renderImpulse(r, total)

	// 1. Nothing may exit before the shortest line: the right bank's
	//    (1116+23)·stretch ≈ 1256 frames at RoomSize 0.85 (reads lag
	//    writes and pre-delay is zero).
	for i := 0; i < 1200; i++ {
		if absF32(outL[i]) > 1e-6 || absF32(outR[i]) > 1e-6 {
			t.Fatalf("frame %d leaks ahead of the delay network: %v/%v", i, outL[i], outR[i])
		}
	}
	first := -1
	for i := 1200; i < 3000; i++ {
		if absF32(outL[i]) > 1e-5 || absF32(outR[i]) > 1e-5 {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("no first reflection within 3000 frames")
	}

	peak := peakF(outL)
	if peak < 0.02 || peak > 1.5 {
		t.Fatalf("impulse peak = %v, want a sane wet level", peak)
	}

	// 2. Echo density: the 100-200 ms window must contain many distinct
	//    reflection peaks (sparse peaks would mean periodic flutter).
	lo, hi := int(0.1*sr), int(0.2*sr)
	win := outL[lo:hi]
	winMax := float64(peakF(win))
	thr := 0.05 * winMax
	peaks := 0
	for i := 1; i < len(win)-1; i++ {
		a := math.Abs(float64(win[i]))
		if a > thr && a >= math.Abs(float64(win[i-1])) && a > math.Abs(float64(win[i+1])) {
			peaks++
		}
	}
	if peaks < 25 {
		t.Errorf("echo density = %d peaks per 100 ms, want >= 25 (dense diffusion)", peaks)
	}

	// 3. No gaps: within the energetic early tail the signal must be
	// continuous (long silences would sound like discrete flutter echoes,
	// not a room). Measured max gap is <1 ms; allow a generous 200.
	maxGap, gap := 0, 0
	const gapThr = 1e-4
	for i := lo; i < int(1.0*sr); i++ {
		if absF32(outL[i]) < gapThr {
			gap++
			if gap > maxGap {
				maxGap = gap
			}
		} else {
			gap = 0
		}
	}
	if maxGap > 200 {
		t.Errorf("longest silent gap = %d frames (>4 ms), tail is not dense", maxGap)
	}

	// 4. Smooth monotone decay on a coarse scale (stability, no ringing
	//    buildup): successive 100 ms RMS bands must decrease, and the
	//    tail must still be alive seconds later (broadband RT60 ~2 s at
	//    RoomSize 0.85 with air damping).
	bands := [][2]int{{int(0.2 * sr), int(0.3 * sr)}, {int(1.0 * sr), int(1.1 * sr)},
		{int(2.0 * sr), int(2.1 * sr)}, {int(2.8 * sr), int(2.9 * sr)}}
	prev := math.MaxFloat64
	rmsVals := make([]float64, len(bands))
	for k, b := range bands {
		rmsVals[k] = rmsF(outL[b[0]:b[1]])
		if rmsVals[k] >= prev {
			t.Errorf("band %d did not decay: %v >= %v", k, rmsVals[k], prev)
		}
		prev = rmsVals[k]
	}
	if rmsVals[2] < 1e-6 {
		t.Errorf("tail died too early: rms %v at 2.0 s (RoomSize 0.85)", rmsVals[2])
	}
	if rmsVals[3] < 1e-7 {
		t.Errorf("tail inaudible at 2.8 s: rms %v", rmsVals[3])
	}

	// 5. Stereo decorrelation: the two bank outputs must diverge.
	a, b2 := outL[int(0.5*sr):int(1.5*sr)], outR[int(0.5*sr):int(1.5*sr)]
	var sab, saa, sbb float64
	for i := range a {
		sab += float64(a[i]) * float64(b2[i])
		saa += float64(a[i]) * float64(a[i])
		sbb += float64(b2[i]) * float64(b2[i])
	}
	corr := sab / math.Sqrt(saa*sbb+1e-30)
	if corr > 0.5 {
		t.Errorf("L/R tail correlation = %.2f, want decorrelated stereo spread", corr)
	}
	if peakF(outR) < 0.02 {
		t.Error("right bank is silent")
	}
}

// TestReverbHFDamping checks the air-absorption model: high-frequency
// content decays faster than the overall tail, and stronger damping
// darkens the late field.
func TestReverbHFDamping(t *testing.T) {
	const sr = 48000
	hfRatio := func(damping float32) (early, late float64) {
		r := NewReverb(sr, ReverbParams{RoomSize: 0.85, Damping: damping, Wet: 1, Dry: 0, Width: 1})
		noise := NewNoise(7)
		const burst = 2400 // 50 ms of broadband excitation
		const total = sr * 3
		out := make([]float32, total)
		for i := 0; i < total; i++ {
			in := float32(0)
			if i < burst {
				in = noise.Next() * 0.8
			}
			out[i], _ = r.ProcessFrameWet(in, in)
		}
		band := func(from, to int) float64 {
			diff := make([]float32, to-from-1)
			for i := from; i < to-1; i++ {
				diff[i-from] = out[i+1] - out[i]
			}
			return rmsF(diff) / (rmsF(out[from:to]) + 1e-12)
		}
		return band(int(0.25*sr), int(0.45*sr)), band(int(1.8*sr), int(2.2*sr))
	}

	brightE, brightL := hfRatio(0.05)
	darkE, darkL := hfRatio(0.55)
	if brightL >= brightE {
		t.Errorf("bright tail: HF ratio late %v >= early %v, want natural HF decay", brightL, brightE)
	}
	drop := func(e, l float64) float64 { return l / (e + 1e-12) }
	if drop(darkE, darkL) >= drop(brightE, brightL) {
		t.Errorf("damping has no darkening effect: dark drop %.2f vs bright drop %.2f",
			drop(darkE, darkL), drop(brightE, brightL))
	}
}

// TestReverbRoomSizeOrdering checks longer rooms decay longer.
func TestReverbRoomSizeOrdering(t *testing.T) {
	const sr = 48000
	decayFrames := func(roomSize float32) int {
		r := NewReverb(sr, ReverbParams{RoomSize: roomSize, Damping: 0.3, Wet: 1, Dry: 0, Width: 1})
		const total = sr * 8
		out, _ := renderImpulse(r, total)
		peak := float64(peakF(out))
		thr := float32(peak * 0.002)
		run := 0
		for i := range out {
			if absF32(out[i]) < thr {
				run++
				if run >= 1000 {
					return i - 1000
				}
			} else {
				run = 0
			}
		}
		return total
	}
	small, big := decayFrames(0.5), decayFrames(0.95)
	if big < 2*small {
		t.Fatalf("decay frames: room0.5=%d room0.95=%d, want the cathedral-like room >= 2x longer", small, big)
	}
	r := NewReverb(sr, ReverbParams{RoomSize: 0.95})
	rt := r.RT60Seconds()
	if rt < 2.5 || rt > 7 {
		t.Errorf("cathedral RT60 estimate = %.1f s, want a few seconds", rt)
	}
	r2 := NewReverb(sr, ReverbParams{RoomSize: 0.55})
	if r2.RT60Seconds() >= rt {
		t.Errorf("smaller room RT60 %.1f not shorter than %.1f", r2.RT60Seconds(), rt)
	}
}

// TestReverbAllpassFormula checks the Schroeder allpass against its
// defining recursion and its energy-preserving property.
func TestReverbAllpassFormula(t *testing.T) {
	a := reverbAllpass{line: newFDNLine(8), gain: 0.5, gain2: 1 - 0.25}
	a.line.length = 8
	out := make([]float32, 400)
	out[0] = a.process(1) // impulse
	for i := 1; i < len(out); i++ {
		out[i] = a.process(0)
	}
	if math.Abs(float64(out[0])+0.5) > 1e-6 {
		t.Errorf("y[0] = %v, want -g = -0.5", out[0])
	}
	if math.Abs(float64(out[8])-0.75) > 1e-6 {
		t.Errorf("y[8] = %v, want (1-g²) = 0.75", out[8])
	}
	var energy float64
	for _, v := range out {
		energy += float64(v) * float64(v)
	}
	if math.Abs(energy-1) > 1e-3 {
		t.Errorf("allpass energy = %v, want 1 (lossless)", energy)
	}
}

// TestReverbPresets checks the preset table.
func TestReverbPresets(t *testing.T) {
	for _, name := range []string{"cathedral", "hall", "chapel", "room", "plate", " CATHEDRAL "} {
		p, ok := PresetParams(name)
		if !ok {
			t.Fatalf("preset %q missing", name)
		}
		if p.RoomSize < 0 || p.RoomSize > 1 || p.Wet < 0 || p.Wet > 1 {
			t.Errorf("preset %q out of range: %+v", name, p)
		}
	}
	if _, ok := PresetParams("closet"); ok {
		t.Error("unknown preset accepted")
	}
}

// TestReverbStability hammers the network with full-scale noise and
// checks bounded output and eventual decay.
func TestReverbStability(t *testing.T) {
	const sr = 48000
	r := NewReverb(sr, ReverbParams{RoomSize: 0.95, Damping: 0.2, Wet: 1, Dry: 0, Width: 1})
	noise := NewNoise(11)
	peak := float32(0)
	for i := 0; i < sr*2; i++ {
		n := noise.Next()
		l, rt := r.ProcessFrameWet(n, n)
		if a := absF32(l); a > peak {
			peak = a
		}
		if a := absF32(rt); a > peak {
			peak = a
		}
	}
	if peak > 6 {
		t.Fatalf("output peak %v after 2 s of full-scale noise: unstable", peak)
	}
	tail := make([]float32, sr*3)
	for i := range tail {
		tail[i], _ = r.ProcessFrameWet(0, 0)
	}
	if late := rmsF(tail[2*sr:]); late > 0.02*float64(peak) {
		t.Fatalf("tail rms %v did not decay below 2%% of peak %v", late, peak)
	}
}

// TestReverbReset checks state clearing.
func TestReverbReset(t *testing.T) {
	r := NewReverb(48000, ReverbParams{RoomSize: 0.8, Wet: 1, Dry: 0, Width: 1})
	r.ProcessFrameWet(1, 1)
	for i := 0; i < 2000; i++ {
		r.ProcessFrameWet(0, 0)
	}
	r.Reset()
	for i := 0; i < 1400; i++ {
		if l, rt := r.ProcessFrameWet(0, 0); l != 0 || rt != 0 {
			t.Fatalf("frame %d after Reset: %v/%v, want silence", i, l, rt)
		}
	}
}

// TestReverbWetDryMix checks the ProcessFrame mix law and ProcessStereo.
func TestReverbWetDryMix(t *testing.T) {
	const sr = 48000
	p := ReverbParams{RoomSize: 0.7, Wet: 0.4, Dry: 0.6, Width: 1}
	r := NewReverb(sr, p)
	// Dry-only for the first frame (network latency): out = Dry·in.
	l, _ := r.ProcessFrame(0.5, 0.5)
	if math.Abs(float64(l)-0.3) > 1e-6 {
		t.Fatalf("first frame = %v, want Dry·in = 0.3", l)
	}
	// ProcessStereo must equal per-frame ProcessFrame on identical input.
	a := NewReverb(sr, p)
	b := NewReverb(sr, p)
	buf := NewStereoBuffer(64)
	buf.SetLen(64)
	for i := 0; i < 64; i++ {
		v := float32(math.Sin(float64(i) * 0.05))
		buf.Left[i], buf.Right[i] = v, v*0.5
	}
	a.ProcessStereo(buf)
	for i := 0; i < 64; i++ {
		v := float32(math.Sin(float64(i) * 0.05))
		el, er := b.ProcessFrame(v, v*0.5)
		if buf.Left[i] != el || buf.Right[i] != er {
			t.Fatalf("frame %d: ProcessStereo %v/%v != ProcessFrame %v/%v",
				i, buf.Left[i], buf.Right[i], el, er)
		}
	}
}

// TestReverbZeroAlloc checks the hot path.
func TestReverbZeroAlloc(t *testing.T) {
	r := NewReverb(48000, ReverbParams{RoomSize: 0.9, Wet: 1, Dry: 0, Width: 1})
	buf := NewStereoBuffer(256)
	buf.SetLen(256)
	for i := range buf.Left {
		buf.Left[i] = float32(i%13) / 13
		buf.Right[i] = buf.Left[i] * 0.5
	}
	r.ProcessStereo(buf) // warm
	if allocs := testing.AllocsPerRun(50, func() { r.ProcessStereo(buf) }); allocs != 0 {
		t.Fatalf("ProcessStereo allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(50, func() { r.ProcessFrameWet(0.1, 0.1) }); allocs != 0 {
		t.Fatalf("ProcessFrameWet allocates %v per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(20, func() {
		r.SetParams(ReverbParams{RoomSize: 0.7, Damping: 0.3, Wet: 0.5, Dry: 0.5, Width: 0.8, PreDelay: 0.02})
	}); allocs != 0 {
		t.Fatalf("SetParams allocates %v per call, want 0", allocs)
	}
}

package dsp

import (
	"math"
	"testing"
)

// feed renders n frames of src through the limiter in blocks, returning
// the planar output.
func feed(l *Limiter, srcL, srcR []float32) ([]float32, []float32) {
	outL := make([]float32, len(srcL))
	outR := make([]float32, len(srcR))
	buf := NewStereoBuffer(512)
	for done := 0; done < len(srcL); {
		n := min(512, len(srcL)-done)
		buf.SetLen(n)
		copy(buf.Left, srcL[done:done+n])
		copy(buf.Right, srcR[done:done+n])
		l.ProcessStereo(buf)
		copy(outL[done:done+n], buf.Left)
		copy(outR[done:done+n], buf.Right)
		done += n
	}
	return outL, outR
}

func TestLookaheadBuffer(t *testing.T) {
	d := NewLookaheadBuffer(240)
	if d.Delay() != 240 || d.Cap() != 256 || d.mask != 255 {
		t.Fatalf("geometry = delay %d cap %d mask %d, want 240/256/255", d.Delay(), d.Cap(), d.mask)
	}
	// Wrap-around behavior with a small ring.
	s := NewLookaheadBuffer(4) // capacity 8
	for i := 1; i <= 12; i++ {
		outL, outR := s.Write(float32(i), float32(-i))
		want := float32(i - 4)
		if i <= 4 {
			want = 0 // latency ramp-in
		}
		if outL != want || outR != -want {
			t.Fatalf("write %d: got %v/%v, want %v/%v", i, outL, outR, want, -want)
		}
	}
	s.Reset()
	if out, _ := s.Write(1, 1); out != 0 {
		t.Fatal("ring not cleared by Reset")
	}
}

func TestLimiterLookahead(t *testing.T) {
	const sr = 48000

	// Defaults: 5 ms lookahead = 240 frames at 48 kHz, ceiling 0.988.
	l := NewLimiter(sr)
	if l.DelayFrames() != 240 {
		t.Fatalf("DelayFrames = %d, want 240", l.DelayFrames())
	}
	if l.Ceiling() != DefaultCeiling || l.ThresholdDB() != -6 {
		t.Fatalf("defaults = ceiling %v threshold %v", l.Ceiling(), l.ThresholdDB())
	}

	// (a) Pure delay: a below-knee impulse exits exactly 240 frames
	// later, unmodified.
	l.SetAutoGain(false)
	l.SetThresholdDB(0) // knee starts at -3 dBFS; a 0.5 impulse stays under it
	in := make([]float32, 1024)
	in[100] = 0.5
	outL, outR := feed(l, in, in)
	for i := range outL {
		want := float32(0)
		if i == 340 {
			want = 0.5
		}
		if outL[i] != want || outR[i] != want {
			t.Fatalf("frame %d = %v/%v, want %v (impulse delayed by exactly 240)", i, outL[i], outR[i], want)
		}
	}

	// (b) Hard ceiling under extreme transients and sustained overload:
	// output must never exceed 0.988, whatever the input.
	l = NewLimiter(sr)
	l.SetAutoGain(false)
	const total = sr * 4
	bigL := make([]float32, total)
	bigR := make([]float32, total)
	for i := 0; i < total; i += 500 {
		bigL[i], bigR[i] = 4.0, -4.0 // isolated 4x-over spikes
	}
	for i := int(3.5 * sr); i < total; i++ { // sustained 3x-over square
		v := float32(3.0)
		if (i/100)%2 == 1 {
			v = -3.0
		}
		bigL[i], bigR[i] = v, v
	}
	outL, outR = feed(l, bigL, bigR)
	peak := float32(0)
	for i := range outL {
		if a := absF32(outL[i]); a > peak {
			peak = a
		}
		if a := absF32(outR[i]); a > peak {
			peak = a
		}
	}
	if peak > DefaultCeiling+1e-6 {
		t.Fatalf("ceiling violated: output peak %v > %v", peak, DefaultCeiling)
	}
	if peak < 0.3 {
		t.Fatalf("output peak %v: limiter silenced the program", peak)
	}

	// (c) Transient preservation: thanks to the 5 ms lookahead the gain
	// ducks BEFORE the peak exits, so the transient keeps its shape
	// (no flat-topped plateau pinned at the ceiling).
	l = NewLimiter(sr)
	l.SetAutoGain(false)
	spike := make([]float32, 2048)
	for i := range spike {
		spike[i] = float32(2.0 * math.Exp(-float64(i)/100))
	}
	outL, _ = feed(l, spike, spike)
	d := l.DelayFrames()
	run, maxRun := 0, 0
	for _, v := range outL {
		if absF32(v) >= DefaultCeiling-1e-6 {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	if maxRun > 2 {
		t.Fatalf("crushed transient: %d consecutive frames at the ceiling", maxRun)
	}
	// Decay shape is preserved: the output ratio between two frames of
	// the transient matches the input ratio (constant gain region).
	inRatio := float64(spike[2] / spike[20])
	outRatio := float64(outL[d+2] / outL[d+20])
	if math.Abs(outRatio-inRatio)/inRatio > 0.25 {
		t.Errorf("transient shape changed: out ratio %v vs in ratio %v", outRatio, inRatio)
	}
	if p := absF32(outL[d]); p < 0.3 || p > DefaultCeiling+1e-6 {
		t.Errorf("transient peak = %v, want a loud but limited transient", p)
	}

	// (d) Zero allocations in the processing loop.
	l = NewLimiter(sr)
	l.SetAutoGain(false)
	noise := make([]float32, 512)
	for i := range noise {
		noise[i] = float32(i%7) / 3
	}
	buf := NewStereoBuffer(512)
	buf.SetLen(512)
	copy(buf.Left, noise)
	copy(buf.Right, noise)
	l.ProcessStereo(buf) // warm
	if allocs := testing.AllocsPerRun(50, func() { l.ProcessStereo(buf) }); allocs != 0 {
		t.Fatalf("ProcessStereo allocates %v times per call, want 0", allocs)
	}
	inter := make([]float32, 1024)
	if allocs := testing.AllocsPerRun(50, func() { l.ProcessInterleaved(inter) }); allocs != 0 {
		t.Fatalf("ProcessInterleaved allocates %v times per call, want 0", allocs)
	}

	// (e) Silence in, silence out; Reset restores the pure delay.
	l = NewLimiter(sr)
	l.SetAutoGain(false)
	zeros := make([]float32, 512)
	outL, _ = feed(l, zeros, zeros)
	for i, v := range outL {
		if v != 0 {
			t.Fatalf("silence produced %v at frame %d", v, i)
		}
	}
	feed(l, spike[:512], spike[:512]) // dirty the state
	l.Reset()
	if l.Gain() != 1 || l.AutoGain() != 1 || l.env != 0 {
		t.Fatal("Reset left state behind")
	}
	in2 := make([]float32, 512)
	in2[10] = 0.25
	outL, _ = feed(l, in2, in2)
	if outL[250] != 0.25 {
		t.Fatalf("after Reset, frame 250 = %v, want the delayed impulse 0.25", outL[250])
	}

	// Delay geometry scales with the sample rate.
	l44 := NewLimiter(44100)
	if want := int(math.Round(DefaultLookahead * 44100)); l44.DelayFrames() != want {
		t.Fatalf("44.1 kHz delay = %d frames, want %d", l44.DelayFrames(), want)
	}
}

func TestLimiterAutoGain(t *testing.T) {
	const sr = 48000
	sine := func(amp float32, seconds float64) []float32 {
		n := int(seconds * sr)
		out := make([]float32, n)
		for i := range out {
			out[i] = amp * float32(math.Sin(2*math.Pi*1000*float64(i)/sr))
		}
		return out
	}
	lufsOf := func(v []float32) float64 {
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		return 10*math.Log10(sum/float64(len(v))) + loudnessCalibration
	}

	// Quiet input (-30.5 dBFS sine): auto-gain rides the +18 dB cap and
	// lands within ~2.5 dB of the -14 LUFS target.
	l := NewLimiter(sr)
	quiet := sine(0.03, 4)
	outL, _ := feed(l, quiet, quiet)
	if got := lufsOf(outL[3*sr:]); math.Abs(got-(-14)) > 2.5 {
		t.Errorf("quiet input: output loudness %.1f LUFS, want ~-14", got)
	}
	if l.AutoGain() > 8.001 || l.AutoGain() < 4 {
		t.Errorf("auto-gain = %v, want capped near 8x", l.AutoGain())
	}

	// Loud input (-0.9 dBFS sine): auto-gain pulls down to the target.
	l = NewLimiter(sr)
	loud := sine(0.9, 4)
	outL, _ = feed(l, loud, loud)
	if got := lufsOf(outL[3*sr:]); math.Abs(got-(-14)) > 2.5 {
		t.Errorf("loud input: output loudness %.1f LUFS, want ~-14", got)
	}
	if l.AutoGain() > 1 {
		t.Errorf("auto-gain = %v, want reduction for loud input", l.AutoGain())
	}

	// Gated: silence must not drag the estimate (or the gain) around.
	l = NewLimiter(sr)
	l.SetTargetLUFS(-14)
	zeros := make([]float32, sr)
	feed(l, zeros, zeros)
	if l.AutoGain() != 1 {
		t.Errorf("silence moved auto-gain to %v, want 1", l.AutoGain())
	}
}

func TestLimiterStereoLink(t *testing.T) {
	const sr = 48000
	n := sr / 2
	inL := make([]float32, n)
	inR := make([]float32, n)
	for i := 0; i < n; i++ {
		v := float32(0.1 * math.Sin(2*math.Pi*440*float64(i)/sr))
		inL[i], inR[i] = v, v
	}
	for i := 2400; i < 2440; i++ { // right-channel-only spike
		inR[i] += float32(2.0 * math.Exp(-float64(i-2400)/20))
	}
	l := NewLimiter(sr)
	l.SetAutoGain(false)
	outL, _ := feed(l, inL, inR)
	d := l.DelayFrames()

	rms := func(b []float32) float64 {
		var s float64
		for _, v := range b {
			s += float64(v) * float64(v)
		}
		return math.Sqrt(s / float64(len(b)))
	}
	before := rms(outL[1000:1200])
	during := rms(outL[d+2400 : d+2600])
	if during > before*0.8 {
		t.Fatalf("left channel not ducked by the right spike: %v vs %v", during, before)
	}
}

func TestLimiterInterleavedEquivalence(t *testing.T) {
	const sr = 48000
	n := 2048
	inL := make([]float32, n)
	inR := make([]float32, n)
	for i := 0; i < n; i++ {
		inL[i] = float32(math.Sin(2 * math.Pi * 220 * float64(i) / sr))
		inR[i] = float32(math.Sin(2*math.Pi*330*float64(i)/sr)) * 0.5
		if i%300 == 0 {
			inL[i] *= 3
		}
	}
	planar := NewLimiter(sr)
	planar.SetAutoGain(false)
	outL, outR := feed(planar, inL, inR)

	inter := NewLimiter(sr)
	inter.SetAutoGain(false)
	b := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		b[2*i], b[2*i+1] = inL[i], inR[i]
	}
	inter.ProcessInterleaved(b)
	for i := 0; i < n; i++ {
		if b[2*i] != outL[i] || b[2*i+1] != outR[i] {
			t.Fatalf("frame %d: interleaved %v/%v != planar %v/%v", i, b[2*i], b[2*i+1], outL[i], outR[i])
		}
	}
}

func TestLimiterGainRecovery(t *testing.T) {
	const sr = 48000
	l := NewLimiter(sr)
	l.SetAutoGain(false)
	loud := make([]float32, sr/4)
	for i := range loud {
		loud[i] = float32(2.0 * math.Sin(2*math.Pi*100*float64(i)/sr))
	}
	feed(l, loud, loud)
	if l.Gain() >= 1 {
		t.Fatalf("gain = %v, want reduction during overload", l.Gain())
	}
	feed(l, make([]float32, sr), make([]float32, sr)) // 1 s of silence
	if l.Gain() <= 0.99 {
		t.Fatalf("gain = %v after 1 s of silence, want recovery to ~1", l.Gain())
	}
}

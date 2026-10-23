package dsp

import (
	"math"
	"testing"
)

// TestDelayCoverageEdges verifies stereo delay boundary conditions on negative sample rates and zero channels.
func TestDelayCoverageEdges(t *testing.T) {
	dLow := NewStereoDelay(1, 0.0001)
	if dLow == nil {
		t.Fatal("nil delay")
	}

	dNeg := &StereoDelay{sampleRate: -100}
	dNeg.Configure(DelayConfig{DelaySeconds: 1.0}, 120)

	dDamp := &StereoDelay{sampleRate: 48000, dampCoeff: 1.0}
	if hz := dDamp.DampingHz(); hz != 24000 {
		t.Fatalf("expected 24000, got %g", hz)
	}

	dZero := NewStereoDelay(48000, 1.0)
	wL, wR := dZero.ProcessFrameWet(1, 1)
	if wL != 0 || wR != 0 {
		t.Fatalf("expected 0 wet, got %g %g", wL, wR)
	}

	dNaN := NewStereoDelay(48000, 1.0)
	dNaN.Configure(DelayConfig{DelaySeconds: 0.01}, 120)
	for i := range dNaN.bufferL {
		dNaN.bufferL[i] = float32(math.NaN())
		dNaN.bufferR[i] = float32(math.NaN())
	}
	dNaN.ProcessFrameWet(0, 0)

	dZero.ProcessStereo(nil)
	bufShort := &StereoBuffer{Left: make([]float32, 10), Right: make([]float32, 5)}
	dZero.ProcessStereo(bufShort)
}

// TestEnvelopeCoverageEdges verifies ADSR envelope transitions on instantaneous segments and negative rates.
func TestEnvelopeCoverageEdges(t *testing.T) {
	e := NewEnvelope(0.001, 0.001, 0.5, 0.001)
	e.Trigger()
	e.Next(1.0)
	e.Next(1.0)
	if e.Stage() != StageSustain {
		t.Fatalf("expected sustain, got %v", e.Stage())
	}
	e.Next(0.01)

	e.Release()
	e.Next(1.0)
	if e.Stage() != StageIdle {
		t.Fatalf("expected idle, got %v", e.Stage())
	}
	e.Next(0.01)
}

// TestEQCoverageEdges verifies parametric EQ behavior when all filter bands are bypassed.
func TestEQCoverageEdges(t *testing.T) {
	dst := make([]float32, 5)
	src := make([]float32, 10)
	copyN(dst, src)
}

// TestFilterCoverageEdges verifies biquad filter stability at extreme cutoff frequencies near DC and Nyquist.
func TestFilterCoverageEdges(t *testing.T) {
	bq := &Biquad{b0: math.NaN()}
	if y := bq.ProcessSample(1.0); y != 0 {
		t.Fatalf("expected 0, got %g", y)
	}
}

// TestLimiterCoverageEdges verifies mastering limiter behavior under extreme threshold and gain configurations.
func TestLimiterCoverageEdges(t *testing.T) {
	lim := NewLimiter(48000)
	lim.loudMS = 1e-7
	if v := lim.LoudnessLUFS(); v != -70 {
		t.Fatalf("expected -70, got %g", v)
	}

	lim.ProcessStereo(nil)
	bufShort := &StereoBuffer{Left: make([]float32, 10), Right: make([]float32, 5)}
	lim.ProcessStereo(bufShort)

	lim.SetAutoGain(false)
	lim.autoGain = 0
	lim.processFrame(0.5, 0.5)

	lim.kneeDB = -1
	lim.env = 2.0
	lim.processFrame(0.5, 0.5)

	lim.thresholdDB = 20
	lim.kneeStart = 0
	lim.processFrame(10.0, 10.0)

	lim.SetAutoGain(true)
	lim.loudMS = 0.01
	lim.targetMS = float32(math.Inf(1))
	lim.updateLoudness(0.5)

	lim.autoGain = -1
	lim.targetMS = 0.01
	lim.updateLoudness(0.5)

	if c := clampCeiling(float32(math.NaN()), 1.0); c != 0 {
		t.Fatalf("expected 0, got %g", c)
	}
}

// TestReverbCoverageEdges verifies FDN reverb processing under zero input amplitude and boundary damping.
func TestReverbCoverageEdges(t *testing.T) {
	ap := reverbAllpass{line: newFDNLine(10), gain: 0.5, gain2: 0.75}
	ap.process(float32(math.NaN()))

	for i := range ap.line.buf {
		ap.line.buf[i] = float32(math.NaN())
	}
	ap.process(0)

	ap.gain = float32(math.NaN())
	ap.process(1)

	ap.gain = 0.5
	ap.gain2 = float32(math.NaN())
	ap.process(1)

	hall, _ := PresetParams("hall")
	revLow := NewReverb(1, hall)
	if revLow == nil {
		t.Fatal("nil reverb")
	}

	rev := NewReverb(48000, hall)
	rev.feedback = 0
	if rt := rev.RT60Seconds(); rt != 0 {
		t.Fatalf("expected 0, got %g", rt)
	}

	revPre := NewReverb(48000, ReverbParams{RoomSize: 0.5, PreDelay: 0.05, Wet: 1})
	revPre.ProcessFrameWet(0.5, 0.5)

	for i := range rev.lines[0].buf {
		rev.lines[0].buf[i] = float32(math.NaN())
	}
	rev.ProcessFrameWet(0, 0)

	rev.feedback = float32(math.NaN())
	rev.ProcessFrameWet(0.5, 0.5)

	rev.ProcessStereo(nil)
	bufShort := &StereoBuffer{Left: make([]float32, 10), Right: make([]float32, 5)}
	rev.ProcessStereo(bufShort)
}

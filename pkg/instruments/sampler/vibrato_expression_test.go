package sampler

import (
	"math"
	"testing"

	"resonata/pkg/instruments"
)

// vibSineSampler builds a sampler holding a pure sine of freq Hz and
// dur seconds (no loop, keycenter 69 so pitch 69 plays at rate 1.0).
func vibSineSampler(freq, dur float64) *Sampler {
	n := int(dur * testRate)
	sine := make([]float32, n)
	for i := range sine {
		sine[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / testRate))
	}
	f := &SFZFile{Regions: []Region{{
		SamplePath: "v.wav", LoKey: 0, HiKey: 127, PitchKeyCenter: 69,
		LoVel: 0, HiVel: 127, LoopMode: LoopNoLoop,
		LoRand: 0, HiRand: 1,
		Sample: &SampleData{Samples: sine, SampleRate: testRate, Channels: 1},
	}}}
	s := New(testRate)
	s.Load(f)
	return s
}

// vibParams returns a full-volume payload with the given vibrato.
func vibParams(rate, depth float64) instruments.NoteParams {
	return instruments.NoteParams{
		HasVibrato: true, VibratoRate: rate, VibratoDepth: depth,
		Expression: 1, HasExpression: true,
	}
}

// renderVoice renders ms milliseconds through one voice in 48-frame
// blocks, recording the phase deviation from unmodulated playback
// (Phase minus elapsed frames at rate 1.0) after each block.
func renderVoice(v *Voice, ms int) (mono []float32, dev []float64) {
	mono = make([]float32, 0, ms*testRate/1000)
	dev = make([]float64, 0, ms*1000/1000)
	elapsed := 0
	mono48 := make([]float32, 48)
	for done := 0; done < ms*testRate/1000; {
		n := 48
		if ms*testRate/1000-done < n {
			n = ms*testRate/1000 - done
		}
		v.Process(mono48[:n], float64(n)/testRate)
		mono = append(mono, mono48[:n]...)
		elapsed += n
		dev = append(dev, v.Phase-float64(elapsed))
		done += n
	}
	return mono, dev
}

// vibDepthMult converts semitone depth to fractional rate swing.
func vibDepthMult(depth float64) float64 { return math.Pow(2, depth/12) - 1 }

// TestVibratoRateDepth proves the LFO oscillates near 6 Hz with the
// depth-implied phase swing: predicted peak-to-peak deviation
// 2*m/(2*pi*6) frames for depth multiplier m.
func TestVibratoRateDepth(t *testing.T) {
	s := vibSineSampler(440, 1.0)
	s.NoteOnParams(69, 0.9, vibParams(6.0, 0.05))
	v := soundingVoice(t, s)
	if !v.hasVibrato || v.vibRate != 6.0 {
		t.Fatalf("vibrato state = %v/%v, want true/6.0", v.hasVibrato, v.vibRate)
	}
	if v.vibTimer != 0 || v.vibPhase != 0 {
		t.Fatalf("vibrato starts at %v/%v, want 0/0", v.vibTimer, v.vibPhase)
	}
	_, dev := renderVoice(v, 600)
	// Analyze the settled last 300 ms (past delay, ramp, and drift).
	win := dev[len(dev)-300:]
	mean, lo, hi := 0.0, win[0], win[0]
	for _, d := range win {
		mean += d
		if d < lo {
			lo = d
		}
		if d > hi {
			hi = d
		}
	}
	mean /= float64(len(win))
	pp := hi - lo
	// Phase-deviation amplitude in frames is m·rate/ω: the per-frame
	// swing m integrates over rate frames/s against the LFO angular
	// frequency ω = 2π·6. Peak-to-peak is twice that.
	pred := 2 * vibDepthMult(0.05) * float64(testRate) / (2 * math.Pi * 6)
	if d := (pp - pred) / pred; d < -0.35 || d > 0.35 {
		t.Errorf("phase swing pp = %v frames, want ~%v (6 Hz, 0.05 st)", pp, pred)
	}
	crossings := 0
	prev := win[0] - mean
	for _, d := range win[1:] {
		cur := d - mean
		if (prev < 0) != (cur < 0) {
			crossings++
		}
		prev = cur
	}
	if crossings < 3 {
		t.Errorf("mean crossings = %d in 300 ms, want >= 3 (6 Hz oscillation)", crossings)
	}
	for _, d := range dev {
		if a := math.Abs(d); a > 12 {
			t.Fatalf("phase deviation %v exceeds 12 frames (runaway)", d)
		}
	}
}

// TestVibratoDelay proves silence for the first 150 ms: a vibrato voice
// renders bit-identical output to a plain voice for the first 100 ms,
// then diverges once engaged.
func TestVibratoDelay(t *testing.T) {
	mk := func() (*Sampler, *Voice) {
		s := vibSineSampler(440, 1.0)
		return s, nil
	}
	sv, _ := mk()
	sv.NoteOnParams(69, 0.9, vibParams(6.0, 0.05))
	vv := soundingVoice(t, sv)
	sc, _ := mk()
	sc.NoteOn(69, 0.9)
	vc := soundingVoice(t, sc)
	mv, _ := renderVoice(vv, 300)
	mc, _ := renderVoice(vc, 300)
	for i := 0; i < 100*testRate/1000; i++ {
		if mv[i] != mc[i] {
			t.Fatalf("frame %d differs before engagement: %v vs %v", i, mv[i], mc[i])
		}
	}
	var late float32
	for i := 200 * testRate / 1000; i < len(mv); i++ {
		if d := abs32(mv[i] - mc[i]); d > late {
			late = d
		}
	}
	if late < 1e-4 {
		t.Errorf("max post-engagement difference = %v, want divergence", late)
	}
}

// TestVibratoNone proves a plain note plays with zero phase deviation.
func TestVibratoNone(t *testing.T) {
	s := vibSineSampler(440, 1.0)
	s.NoteOn(69, 0.9)
	v := soundingVoice(t, s)
	if v.hasVibrato {
		t.Fatal("plain note engages vibrato")
	}
	_, dev := renderVoice(v, 500)
	for i, d := range dev {
		if d != 0 {
			t.Fatalf("deviation at block %d = %v, want exact 0", i, d)
		}
	}
}

// constSampler builds a constant-level sample voice payload helper.
func constSampler(level float32) *Sampler {
	n := testRate / 2 // 500 ms
	sd := &SampleData{Samples: make([]float32, n), SampleRate: testRate, Channels: 1}
	for i := range sd.Samples {
		sd.Samples[i] = level
	}
	rg := newRegion()
	rg.SamplePath, rg.Sample = "c.wav", sd
	s := New(testRate)
	s.Load(&SFZFile{Regions: []Region{rg}})
	return s
}

// sustainPeak renders 200 ms (past the 5 ms attack) and returns the
// peak over the settled last 100 ms.
func sustainPeak(t *testing.T, s *Sampler) float32 {
	t.Helper()
	v := soundingVoice(t, s)
	mono := make([]float32, 200*testRate/1000)
	v.Process(mono, float64(len(mono))/testRate)
	var peak float32
	for _, x := range mono[100*testRate/1000:] {
		if a := abs32(x); a > peak {
			peak = a
		}
	}
	return peak
}

// TestExpressionGain halves sustained output at expression 0.5.
func TestExpressionGain(t *testing.T) {
	half := constSampler(0.5)
	half.NoteOnParams(60, 0.8, instruments.NoteParams{Expression: 0.5, HasExpression: true})
	v := soundingVoice(t, half)
	if v.exprGain != 0.5 {
		t.Fatalf("exprGain = %v, want 0.5", v.exprGain)
	}
	full := constSampler(0.5)
	full.NoteOnParams(60, 0.8, instruments.NoteParams{Expression: 1, HasExpression: true})
	ratio := sustainPeak(t, half) / sustainPeak(t, full)
	if d := ratio - 0.5; d < -1e-5 || d > 1e-5 {
		t.Fatalf("expression ratio = %v, want 0.5", ratio)
	}
}

// TestExpressionDefault leaves output untouched without expression.
func TestExpressionDefault(t *testing.T) {
	plain := constSampler(0.5)
	plain.NoteOn(60, 0.8)
	v := soundingVoice(t, plain)
	if v.exprGain != 1 {
		t.Fatalf("exprGain = %v, want 1.0", v.exprGain)
	}
	full := constSampler(0.5)
	full.NoteOnParams(60, 0.8, instruments.NoteParams{Expression: 1, HasExpression: true})
	if a, b := sustainPeak(t, plain), sustainPeak(t, full); a != b {
		t.Fatalf("default peak %v != explicit-1.0 peak %v", a, b)
	}
}

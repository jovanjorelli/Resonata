package mixer

import (
	"math"
	"testing"

	"resonata/pkg/dsp"
)

// impulseInstrument renders a single unit impulse on the first frame it
// ever processes, then silence. Each track gets its own instance so
// muted tracks stay unfired until soloed.
type impulseInstrument struct{ fired bool }

func (s *impulseInstrument) Process(buffer *dsp.Buffer, _ float64) {
	for i := range buffer.Left {
		v := float32(0)
		if !s.fired && i == 0 {
			v = 1
			s.fired = true
		}
		buffer.Left[i], buffer.Right[i] = v, v
	}
}
func (s *impulseInstrument) NoteOn(pitch int, velocity float32)      {}
func (s *impulseInstrument) NoteOff(pitch int)                       {}
func (s *impulseInstrument) SetParameters(params map[string]float32) {}

// delayCfg builds a stereo echo config with a manual tap length.
func delayCfg(seconds, feedback float32) dsp.DelayConfig {
	return dsp.DelayConfig{
		Mode: dsp.DelayModeStereo, DelaySeconds: float64(seconds),
		Feedback: feedback, DampingHz: 15000, Wet: 1,
	}
}

// TestMixerDelayIndependence gives two tracks different tap lengths and
// feedback amounts, then solos each via mute and checks every echo lands
// on its own track's schedule with its own decay — never the other's.
func TestMixerDelayIndependence(t *testing.T) {
	const sr = 48000
	setup := func() *Mixer {
		m := New(sr, 2, 8192)
		m.SetReverbWet(0)
		a := m.AddTrack(&impulseInstrument{}, 0, 1)
		m.SetTrackDelay(a, delayCfg(0.05, 0.5), 120) // tap 2400, gentle decay
		b := m.AddTrack(&impulseInstrument{}, 0, 1)
		m.SetTrackDelay(b, delayCfg(0.07, 0.7), 120) // tap 3360, hotter decay
		if m.TrackDelay(a) == nil || m.TrackDelay(b) == nil {
			t.Fatal("tracks missing their echo instances")
		}
		if got := m.TrackDelay(a).LengthSamples(); got != 2400 {
			t.Fatalf("track A tap = %d, want 2400", got)
		}
		if got := m.TrackDelay(b).LengthSamples(); got != 3360 {
			t.Fatalf("track B tap = %d, want 3360", got)
		}
		return m
	}
	at := func(master []float32, frame int) float32 { return master[2*frame] }

	// Solo A: echoes at 2400/4800/7200, silence at B's 3360.
	m := setup()
	m.SetMuted(1, true)
	m.Process(8192)
	master := append([]float32(nil), m.Master()...)
	tap1, tap2 := at(master, 2400), at(master, 4800)
	if tap1 < 0.55 || tap1 > 0.66 {
		t.Fatalf("track A first echo = %v, want ~0.61", tap1)
	}
	ratioA := tap2 / tap1
	if ratioA < 0.35 || ratioA > 0.5 {
		t.Fatalf("track A decay ratio = %v, want ~0.43 (fb 0.5)", ratioA)
	}
	if v := at(master, 3360); math.Abs(float64(v)) > 1e-6 {
		t.Fatalf("track B tap leaks %v into track A solo", v)
	}

	// Solo B: echoes at 3360/6720 with hotter decay, silence at A's 2400.
	m = setup()
	m.SetMuted(0, true)
	m.Process(8192)
	master = append([]float32(nil), m.Master()...)
	tap1, tap2 = at(master, 3360), at(master, 6720)
	if tap1 < 0.55 || tap1 > 0.66 {
		t.Fatalf("track B first echo = %v, want ~0.61", tap1)
	}
	ratioB := tap2 / tap1
	if ratioB < 0.5 || ratioB > 0.7 {
		t.Fatalf("track B decay ratio = %v, want ~0.60 (fb 0.7)", ratioB)
	}
	if v := at(master, 2400); math.Abs(float64(v)) > 1e-6 {
		t.Fatalf("track A tap leaks %v into track B solo", v)
	}
	if ratioA >= ratioB {
		t.Fatalf("decay ratios not independent: A %v >= B %v", ratioA, ratioB)
	}
}

// TestMixerDelayIsolation checks three tracks where only one has an
// echo: with the echo track muted the tail is exactly silent, and the
// echoless tracks report nil instances.
func TestMixerDelayIsolation(t *testing.T) {
	const sr = 48000
	setup := func() *Mixer {
		m := New(sr, 3, 8192)
		m.SetReverbWet(0)
		m.AddTrack(&impulseInstrument{}, 0, 1)
		m.SetTrackDelay(0, delayCfg(0.05, 0.6), 120)
		m.AddTrack(&impulseInstrument{}, -0.5, 0.8)
		m.AddTrack(&impulseInstrument{}, 0.5, 0.8)
		return m
	}

	m := setup()
	if m.TrackDelay(0) == nil {
		t.Fatal("echo track has no instance")
	}
	if m.TrackDelay(1) != nil || m.TrackDelay(2) != nil {
		t.Fatal("echoless tracks report delay instances")
	}
	m.SetMuted(0, true)
	m.Process(8192)
	for i, v := range m.Master()[2:] { // past the dry impulses at frame 0
		if math.Abs(float64(v)) > 1e-6 {
			t.Fatalf("muted echo still rings: master[%d] = %v", i+2, v)
		}
	}

	m = setup()
	m.Process(8192)
	if v := m.Master()[2*2400]; math.Abs(float64(v)) < 0.1 {
		t.Fatalf("echo track produced no repeat: %v", v)
	}
}

// TestMixerDelayWetZeroBypass checks JSON authority at the mixer level:
// wet zero renders exactly the dry signal.
func TestMixerDelayWetZeroBypass(t *testing.T) {
	const sr = 48000
	dry := New(sr, 1, 256)
	dry.SetReverbWet(0)
	dry.AddTrack(&dcInstrument{level: 0.4}, 0, 1)
	dry.Process(256)

	wet := New(sr, 1, 256)
	wet.SetReverbWet(0)
	wet.AddTrack(&dcInstrument{level: 0.4}, 0, 1)
	wet.SetTrackDelay(0, dsp.DelayConfig{
		Mode: dsp.DelayModeStereo, DelaySeconds: 0.05,
		Feedback: 0.6, DampingHz: 4000, Wet: 0,
	}, 120)
	wet.Process(256)

	a, b := dry.Master(), wet.Master()
	if len(a) != len(b) {
		t.Fatalf("lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-6 {
			t.Fatalf("wet=0 colored master[%d]: %v vs dry %v", i, b[i], a[i])
		}
	}
}

// TestMixerDelayScale checks ten simultaneous echoes: memory stays
// bounded and the hot loop allocates nothing.
func TestMixerDelayScale(t *testing.T) {
	const sr = 48000
	m := New(sr, 10, 512)
	m.SetReverbWet(0)
	subs := []dsp.Subdivision{
		dsp.SubdivisionQuarter, dsp.SubdivisionEighth, dsp.SubdivisionEighthD,
		dsp.SubdivisionSixteenth, dsp.SubdivisionHalf, dsp.SubdivisionWhole,
		dsp.SubdivisionSixteenthT, dsp.SubdivisionQuarter, dsp.SubdivisionEighth,
		dsp.SubdivisionHalf,
	}
	for i := 0; i < 10; i++ {
		m.AddTrack(&dcInstrument{level: 0.05}, 0, 0.5)
		m.SetTrackDelay(i, dsp.DelayConfig{
			Mode: dsp.DelayModePingPong, Subdivision: subs[i],
			Feedback: 0.5, DampingHz: 4000, Wet: 0.5,
		}, 120)
		if m.TrackDelay(i) == nil {
			t.Fatalf("track %d missing its echo instance", i)
		}
	}
	var total int
	for i := 0; i < 10; i++ {
		total += m.TrackDelay(i).MemoryBytes()
	}
	if total <= 0 || total > 32<<20 {
		t.Fatalf("ten echoes hold %d bytes, want within (0, 32 MiB]", total)
	}
	m.Process(512) // warm every path
	if allocs := testing.AllocsPerRun(30, func() { m.Process(512) }); allocs != 0 {
		t.Fatalf("Process(512) with ten echoes allocates %v times per call, want 0", allocs)
	}
	if p := peakOf(m.Master()); p <= 0 || p > 1 {
		t.Fatalf("master peak = %v, want audible and bounded", p)
	}
}

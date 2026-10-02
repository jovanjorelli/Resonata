package mixer

import (
	"math"

	"resonata/pkg/dsp"
	"resonata/pkg/instruments"
)

// Master-chain defaults.
const (
	DefaultClipThreshold float32 = 0.9
	DefaultReverbWet     float32 = 0.25

	// maxTrackRooms caps the distinct per-track acoustic spaces beyond
	// the master reverb. Extra rooms merge into the nearest existing
	// one, bounding memory no matter the track count.
	maxTrackRooms = 8
)

// TrackBuffer is one mixer strip: an instrument plus its pre-allocated
// interleaved stereo bus and mix settings. Tracks with an echo own a
// dedicated delay instance allocated once at setup time.
type TrackBuffer struct {
	instrument instruments.Instrument
	buffer     []float32 // interleaved stereo, 2·maxBlock samples
	pan        float32   // -1 (left) to 1 (right)
	volume     float32   // linear gain, 0 to 1
	send       float32   // reverb send level, 0 to 1
	delay      *dsp.StereoDelay
	delayWet   float32 // echo return mix, 0 to 1
	muted      bool
	reverbIdx  int // send-bus room: 0 is the master reverb, -1 is dry
}

// Mixer sums instrument tracks into an interleaved stereo master bus and
// runs the master chain: per-track reverb sends are summed into one send
// bus per acoustic space and returned through that space's FDN reverb
// (pkg/dsp), each track's own echo (when present) adds its wet return
// straight into the master, and the tanh soft clipper runs last so it
// also catches the wet returns. Tracks without a room override share the
// master reverb; tracks sharing a room configuration share one instance.
// All buffers are pre-allocated in New (track buses come from a fixed
// arena, send buses from a fixed pool sized for every room), so Process
// performs no allocations.
type Mixer struct {
	tracks       []TrackBuffer
	masterBuffer []float32 // stereo interleaved
	sendPool     []float32 // (maxTrackRooms+1) stereo send buses, room 0 first
	sampleRate   int

	maxBlock  int               // frames per channel the buffers hold
	lastN     int               // frames rendered by the last Process
	pool      []float32         // track-bus arena handed out by AddTrack
	poolUsed  int               // consumed prefix of pool
	render    *dsp.StereoBuffer // instrument render scratch, reused per track
	clipper   SoftClipper
	reverb    *dsp.Reverb   // master FDN reverb, always reverbs[0]
	reverbs   []*dsp.Reverb // one FDN per acoustic space, index 0 is master
	reverbWet float32
}

// New creates a mixer at sampleRate Hz, pre-allocating maxTracks strips
// and buffers for up to maxBlock frames per Process call.
func New(sampleRate, maxTracks, maxBlock int) *Mixer {
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	if maxTracks < 1 {
		maxTracks = 1
	}
	if maxBlock < 1 {
		maxBlock = 1
	}
	hall, _ := dsp.PresetParams("hall")
	master := dsp.NewReverb(float64(sampleRate), hall)
	return &Mixer{
		tracks:       make([]TrackBuffer, 0, maxTracks),
		masterBuffer: make([]float32, 2*maxBlock),
		sendPool:     make([]float32, (maxTrackRooms+1)*2*maxBlock),
		sampleRate:   sampleRate,
		maxBlock:     maxBlock,
		pool:         make([]float32, maxTracks*2*maxBlock),
		render:       dsp.NewStereoBuffer(maxBlock),
		clipper:      SoftClipper{threshold: DefaultClipThreshold},
		reverb:       master,
		reverbs:      []*dsp.Reverb{master},
		reverbWet:    DefaultReverbWet,
	}
}

// AddTrack registers an instrument strip and returns its index, or -1
// when the mixer is full or the instrument is nil. Pan is clamped to
// [-1, 1] and volume to [0, 1]. The track bus is a slice of the
// pre-allocated arena, so AddTrack does not allocate either.
func (m *Mixer) AddTrack(instrument instruments.Instrument, pan, volume float32) int {
	if instrument == nil || len(m.tracks) == cap(m.tracks) {
		return -1
	}
	size := 2 * m.maxBlock
	buf := m.pool[m.poolUsed : m.poolUsed+size]
	m.poolUsed += size
	m.tracks = append(m.tracks, TrackBuffer{
		instrument: instrument,
		buffer:     buf,
		pan:        clamp32(pan, -1, 1),
		volume:     clamp32(volume, 0, 1),
	})
	return len(m.tracks) - 1
}

// TrackCount returns the number of registered strips.
func (m *Mixer) TrackCount() int { return len(m.tracks) }

// SampleRate reports the mixer sample rate in Hz.
func (m *Mixer) SampleRate() int { return m.sampleRate }

// SetPan adjusts strip i; out-of-range indices are ignored.
func (m *Mixer) SetPan(i int, pan float32) {
	if m.valid(i) {
		m.tracks[i].pan = clamp32(pan, -1, 1)
	}
}

// SetVolume adjusts strip i (linear gain, clamped to [0, 1]).
func (m *Mixer) SetVolume(i int, volume float32) {
	if m.valid(i) {
		m.tracks[i].volume = clamp32(volume, 0, 1)
	}
}

// SetMuted mutes or unmutes strip i.
func (m *Mixer) SetMuted(i int, muted bool) {
	if m.valid(i) {
		m.tracks[i].muted = muted
	}
}

// SetSend sets strip i's reverb send level, clamped to [0, 1].
func (m *Mixer) SetSend(i int, send float32) {
	if m.valid(i) {
		m.tracks[i].send = clamp32(send, 0, 1)
	}
}

// TrackSend reports strip i's reverb send level.
func (m *Mixer) TrackSend(i int) float32 {
	if !m.valid(i) {
		return 0
	}
	return m.tracks[i].send
}

// SetTrackDelay allocates a dedicated echo for strip i, configured from
// cfg at bpm. cfg.Wet sets the return mix; zero renders exact dry. The
// score-level send field is deprecated and ignored: every echo processes
// its track's full bus signal internally. Allocation happens here at
// setup time; the audio loop stays allocation-free. Out-of-range indices
// are ignored.
func (m *Mixer) SetTrackDelay(i int, cfg dsp.DelayConfig, bpm float64) {
	if !m.valid(i) {
		return
	}
	d := dsp.NewStereoDelay(m.sampleRate, dsp.MaxDelaySeconds)
	d.Configure(cfg, bpm)
	m.tracks[i].delay = d
	m.tracks[i].delayWet = d.Wet()
}

// roomBus returns the pre-allocated stereo send bus for room r.
func (m *Mixer) roomBus(r, n int) []float32 {
	base := r * 2 * m.maxBlock
	return m.sendPool[base : base+2*n]
}

// nearestRoom finds the configured space closest to p by Euclidean
// distance over size, damping, and width, ignoring the master at 0.
func (m *Mixer) nearestRoom(p dsp.ReverbParams) int {
	best, bestD := 1, math.MaxFloat64
	for r := 1; r < len(m.reverbs); r++ {
		q := m.reverbs[r].Params()
		ds := float64(q.RoomSize - p.RoomSize)
		dd := float64(q.Damping - p.Damping)
		dw := float64(q.Width - p.Width)
		if d := ds*ds + dd*dd + dw*dw; d < bestD {
			best, bestD = r, d
		}
	}
	return best
}

// SetTrackRoom assigns strip i's reverb send to the space tuned by p,
// creating and pre-allocating that room's FDN instance on first use so
// tracks sharing one configuration share one instance. Rooms beyond the
// cap merge into the nearest existing room. It returns the room index
// (0 is the master reverb). Allocation happens here at setup time; the
// audio loop stays allocation-free. Out-of-range indices are ignored.
func (m *Mixer) SetTrackRoom(i int, p dsp.ReverbParams) int {
	if !m.valid(i) {
		return 0
	}
	for r := 1; r < len(m.reverbs); r++ {
		if m.reverbs[r].Params() == p {
			m.tracks[i].reverbIdx = r
			return r
		}
	}
	if len(m.reverbs) > maxTrackRooms {
		r := m.nearestRoom(p)
		m.tracks[i].reverbIdx = r
		return r
	}
	m.reverbs = append(m.reverbs, dsp.NewReverb(float64(m.sampleRate), p))
	m.tracks[i].reverbIdx = len(m.reverbs) - 1
	return len(m.reverbs) - 1
}

// SetTrackRoomNone drops strip i's reverb send: the track renders dry
// no matter the send level.
func (m *Mixer) SetTrackRoomNone(i int) {
	if m.valid(i) {
		m.tracks[i].reverbIdx = -1
	}
}

// TrackRoom reports strip i's acoustic space: its parameters and true
// when the track overrides the master reverb. Tracks without an
// override (or out-of-range strips) report false.
func (m *Mixer) TrackRoom(i int) (dsp.ReverbParams, bool) {
	if !m.valid(i) {
		return dsp.ReverbParams{}, false
	}
	if r := m.tracks[i].reverbIdx; r > 0 && r < len(m.reverbs) {
		return m.reverbs[r].Params(), true
	}
	return dsp.ReverbParams{}, false
}

// TrackDelay exposes strip i's echo, or nil when the track has none.
func (m *Mixer) TrackDelay(i int) *dsp.StereoDelay {
	if !m.valid(i) {
		return nil
	}
	return m.tracks[i].delay
}

// Muted reports whether strip i is muted.
func (m *Mixer) Muted(i int) bool { return m.valid(i) && m.tracks[i].muted }

// SetReverbWet sets the master reverb return level in [0, 1]; 0 bypasses
// the network entirely.
func (m *Mixer) SetReverbWet(w float32) { m.reverbWet = clamp32(w, 0, 1) }

// ReverbWet reports the master reverb return level.
func (m *Mixer) ReverbWet() float32 { return m.reverbWet }

// SetReverbPreset retunes the master reverb to a built-in space
// (cathedral, hall, chapel, room, plate), reporting whether the name
// matched.
func (m *Mixer) SetReverbPreset(name string) bool {
	p, ok := dsp.PresetParams(name)
	if ok {
		m.reverb.SetParams(p)
	}
	return ok
}

// SetClipThreshold retunes the master soft clipper.
func (m *Mixer) SetClipThreshold(t float32) { m.clipper.SetThreshold(t) }

// ClipThreshold reports the soft clipper threshold.
func (m *Mixer) ClipThreshold() float32 { return m.clipper.Threshold() }

// Reverb exposes the master FDN reverb for preset/parameter tweaks.
func (m *Mixer) Reverb() *dsp.Reverb { return m.reverb }

// Process renders blockSize frames of every unmuted track into its bus,
// sums the buses into the interleaved stereo master plus one reverb send
// bus per acoustic space (send × track audio), returns each space's FDN
// wet signal into the master, adds each track echo's wet return, and
// soft-clips last. The result is available via Master until the next
// call. blockSize is clamped to [1, maxBlock]; nothing allocates.
func (m *Mixer) Process(blockSize int) {
	n := min(blockSize, m.maxBlock)
	if n < 1 {
		m.lastN = 0
		return
	}
	m.lastN = n
	dt := 1 / float64(m.sampleRate)

	// Clear the master and every send bus (reuse, never allocate).
	master := m.masterBuffer[:2*n]
	clear(master)
	for r := 0; r < len(m.reverbs); r++ {
		clear(m.roomBus(r, n))
	}

	// Render each unmuted track and sum it into the buses.
	for i := range m.tracks {
		tr := &m.tracks[i]
		if tr.muted {
			continue
		}
		m.render.SetLen(n)
		m.render.Clear()
		tr.instrument.Process(m.render, dt)
		// Interleave the planar render into the track's stereo bus.
		buf := tr.buffer[:2*n]
		for j := 0; j < n; j++ {
			buf[2*j] = m.render.Left[j]
			buf[2*j+1] = m.render.Right[j]
		}
		m.mixTrack(master, buf, tr.pan, tr.volume)
		if tr.send > 0 && tr.reverbIdx >= 0 && tr.reverbIdx < len(m.reverbs) {
			m.mixTrack(m.roomBus(tr.reverbIdx, n), buf, tr.pan, tr.volume*tr.send)
		}
		if tr.delay != nil {
			m.mixDelay(master, tr, buf, n)
		}
	}

	// Reverb returns: every space's send bus through its own FDN, scaled
	// by the master wet level. Spaces always process when wet so tails
	// ring out across blocks even after the sends go quiet.
	if m.reverbWet > 0 {
		for r := 0; r < len(m.reverbs); r++ {
			send := m.roomBus(r, n)
			rev := m.reverbs[r]
			for i := 0; i < n; i++ {
				wl, wr := rev.ProcessFrameWet(send[2*i], send[2*i+1])
				master[2*i] += wl * m.reverbWet
				master[2*i+1] += wr * m.reverbWet
			}
		}
	}

	// Master processing: the clipper runs last so it also tames the wet
	// returns.
	m.applySoftClipper(n)
}

// mixTrack sums an interleaved stereo track bus into dst, applying
// constant-power panning and a linear gain.
func (m *Mixer) mixTrack(dst []float32, buffer []float32, pan, volume float32) {
	left, right := PanGains(pan)
	lg, rg := left*volume, right*volume
	for i := 0; i+1 < len(buffer); i += 2 {
		dst[i] += buffer[i] * lg
		dst[i+1] += buffer[i+1] * rg
	}
}

// mixDelay runs the track's dedicated echo over its panned bus signal
// and adds the wet return straight into the master. Unlike dry audio,
// echo returns are not re-panned. A zero wet level renders exact dry and
// skips the network. Nothing allocates.
func (m *Mixer) mixDelay(master []float32, tr *TrackBuffer, buf []float32, n int) {
	if tr.delayWet <= 0 {
		return
	}
	left, right := PanGains(tr.pan)
	lg, rg := left*tr.volume, right*tr.volume
	for j := 0; j < n; j++ {
		wl, wr := tr.delay.ProcessFrameWet(buf[2*j]*lg, buf[2*j+1]*rg)
		master[2*j] += wl * tr.delayWet
		master[2*j+1] += wr * tr.delayWet
	}
}

// applySoftClipper runs the master bus through tanh saturation.
func (m *Mixer) applySoftClipper(n int) {
	m.clipper.ProcessSlice(m.masterBuffer[:2*n])
}

// ActiveVoices-style accessors for metering.

// TrackPeak returns the peak magnitude of strip i's bus from the last
// Process call; muted strips report 0.
func (m *Mixer) TrackPeak(i int) float32 {
	if !m.valid(i) || m.tracks[i].muted {
		return 0
	}
	peak := float32(0)
	for _, v := range m.tracks[i].buffer[:2*m.lastN] {
		if a := abs32(v); a > peak {
			peak = a
		}
	}
	return peak
}

// Master returns the interleaved stereo master bus rendered by the last
// Process call (2·frames samples). The slice is owned by the mixer and is
// overwritten by the next Process.
func (m *Mixer) Master() []float32 { return m.masterBuffer[:2*m.lastN] }

// Frames returns the frame count rendered by the last Process call.
func (m *Mixer) Frames() int { return m.lastN }

// valid reports whether i addresses a registered strip.
func (m *Mixer) valid(i int) bool { return i >= 0 && i < len(m.tracks) }

// clampF bounds v to [lo, hi].
func clampF(v, lo, hi float64) float64 {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// clamp32 bounds v to [lo, hi].
func clamp32(v, lo, hi float32) float32 {
	switch {
	case v < lo || v != v:
		return lo
	case v > hi:
		return hi
	}
	return v
}

// abs32 returns the magnitude of v.
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

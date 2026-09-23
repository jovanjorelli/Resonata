package midi

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"resonata/pkg/score"
)

// DefaultTicksPerQuarter is the PPQ resolution FromScore writes.
const DefaultTicksPerQuarter = 480

// GM program choices for Resonata instrument types on export.
const (
	programOcarina = 79 // GM "Ocarina"
	programPiano   = 0  // GM "Acoustic Grand Piano" (sampler stand-in)
)

// timeSeg is one integrated tempo segment for tick→seconds conversion.
type timeSeg struct {
	tick uint32
	sec  float64
	// seconds per tick within this segment
	secPerTick float64
}

// buildTimeMap integrates the tempo map into tick→seconds segments.
func buildTimeMap(tempos []Tempo, ppq int) []timeSeg {
	segs := make([]timeSeg, 0, len(tempos)+1)
	prev := timeSeg{tick: 0, sec: 0, secPerTick: 500000 / 1e6 / float64(ppq)}
	for _, t := range tempos {
		if t.Tick > prev.tick {
			segs = append(segs, timeSeg{
				tick:       prev.tick,
				sec:        prev.sec,
				secPerTick: prev.secPerTick,
			})
			prev.sec += float64(t.Tick-prev.tick) * prev.secPerTick
		}
		prev.tick = t.Tick
		prev.secPerTick = float64(t.MicrosPerQuarter) / 1e6 / float64(ppq)
	}
	segs = append(segs, prev)
	return segs
}

// tickTime converts an absolute tick to seconds through the tempo map.
func tickTime(segs []timeSeg, tick uint32) float64 {
	seg := segs[0]
	for _, s := range segs {
		if s.tick > tick {
			break
		}
		seg = s
	}
	return seg.sec + float64(tick-seg.tick)*seg.secPerTick
}

// ToScore converts a parsed SMF into a Resonata score. FF 51 tempo events
// (with FF 58 time signature and FF 03 names) drive the metadata and the
// tick→seconds mapping; notes are grouped per (source track, channel);
// CC7/CC10 restore volume and pan; General MIDI programs are approximated
// on the ocarina model per instrument family.
func ToScore(f *File) (*score.Score, error) {
	if f == nil || len(f.Tracks) == 0 || f.Header.TicksPerQuarter <= 0 {
		return nil, errors.New("midi: empty or untimed file")
	}
	ppq := f.Header.TicksPerQuarter

	tempos := f.Tempos()
	if len(tempos) == 0 {
		tempos = []Tempo{{Tick: 0, MicrosPerQuarter: 500000}} // 120 BPM
	}
	segs := buildTimeMap(tempos, ppq)

	bpm := BPMFromMicros(tempos[0].MicrosPerQuarter)
	if bpm > 1000 {
		bpm = 1000 // stay inside the score validator's range
	}

	ts := "4/4"
	if tss := f.TimeSignatures(); len(tss) > 0 && tss[0].Numerator > 0 {
		ts = fmt.Sprintf("%d/%d", tss[0].Numerator, tss[0].Denominator)
	}

	title := ""
	for _, tr := range f.Tracks {
		if n := strings.TrimSpace(TrackName(tr)); n != "" {
			title = n
			break
		}
	}
	if title == "" {
		title = "Imported MIDI"
	}

	out := &score.Score{
		Metadata: score.Metadata{Title: title, BPM: bpm, TimeSignature: ts},
	}

	for ti, tr := range f.Tracks {
		endTick := MaxTick(tr)
		notes := CollectNotes(tr, endTick)
		if len(notes) == 0 {
			continue // conductor/meta-only track
		}
		trackName := strings.TrimSpace(TrackName(tr))

		// Group notes by channel, preserving first-seen order.
		order := []int{}
		byCh := map[int][]Note{}
		for _, n := range notes {
			if _, ok := byCh[n.Channel]; !ok {
				order = append(order, n.Channel)
			}
			byCh[n.Channel] = append(byCh[n.Channel], n)
		}
		program, cc7, cc10 := channelState(tr)

		for _, ch := range order {
			percussion := ch == 9
			prog := program[ch]
			name := trackName
			if name == "" {
				if percussion {
					name = "Drums"
				} else {
					name = GMProgramNames[prog]
				}
			}
			if len(order) > 1 {
				name = fmt.Sprintf("%s (ch %d)", name, ch+1)
			}

			st := score.Track{
				ID:         fmt.Sprintf("t%d_c%d", ti, ch),
				Name:       name,
				Instrument: score.InstrumentDef{Type: "ocarina", Parameters: familyVoicing(prog, percussion)},
				Pan:        0,
				Volume:     0.8,
				ReverbSend: 0.2,
			}
			if v, ok := cc10[ch]; ok {
				st.Pan = float32(clampF64(float64(v)/63.5-1, -1, 1))
			}
			if v, ok := cc7[ch]; ok {
				st.Volume = float32(clampF64(float64(v)/127*0.9, 0, 1))
			}
			for _, n := range byCh[ch] {
				start := tickTime(segs, n.StartTick)
				end := tickTime(segs, n.EndTick)
				// CollectNotes guarantees end > start, so the duration
				// is at least one tick; no clamp is needed, and keeping
				// the raw tick-derived value makes exports round-trip
				// exactly.
				st.Notes = append(st.Notes, score.NoteEvent{
					Time:     start,
					Duration: end - start,
					Pitch:    clampInt(n.Pitch, 0, 127),
					Velocity: float32(clampInt(n.Velocity, 1, 127)) / 127,
				})
			}
			sort.SliceStable(st.Notes, func(i, j int) bool { return st.Notes[i].Time < st.Notes[j].Time })
			out.Tracks = append(out.Tracks, st)
		}
	}
	if len(out.Tracks) == 0 {
		return nil, errors.New("midi: file contains no notes")
	}
	return out, nil
}

// channelState extracts the first program change and the last CC7/CC10
// per channel from a track's events.
func channelState(tr []Event) (program map[int]int, cc7 map[int]int, cc10 map[int]int) {
	program = map[int]int{}
	cc7 = map[int]int{}
	cc10 = map[int]int{}
	for _, e := range tr {
		switch {
		case e.Type == EventProgramChange:
			if _, seen := program[e.Channel]; !seen {
				program[e.Channel] = e.Data1
			}
		case e.Type == EventControlChange && e.Data1 == 7:
			cc7[e.Channel] = e.Data2
		case e.Type == EventControlChange && e.Data1 == 10:
			cc10[e.Channel] = e.Data2
		}
	}
	return
}

// familyVoicing approximates a General MIDI family on the ocarina model
// (the synth stand-in available for arbitrary imports).
func familyVoicing(program int, percussion bool) map[string]float32 {
	if percussion || program/8 == 14 {
		return map[string]float32{"brightness": 0.9, "breath_noise": 0.5,
			"vibrato_rate": 1, "vibrato_depth": 0}
	}
	switch program / 8 {
	case 5, 6: // strings & ensembles
		return map[string]float32{"brightness": 0.45, "breath_noise": 0.35,
			"vibrato_rate": 4.5, "vibrato_depth": 0.015}
	case 7: // brass
		return map[string]float32{"brightness": 0.8, "breath_noise": 0.3,
			"vibrato_rate": 5, "vibrato_depth": 0.02}
	case 8, 9: // reeds & pipes (flutes, ocarina, sax)
		return map[string]float32{"brightness": 0.7, "breath_noise": 0.35,
			"vibrato_rate": 5.5, "vibrato_depth": 0.02}
	case 4: // bass
		return map[string]float32{"brightness": 0.3, "breath_noise": 0.3,
			"vibrato_rate": 3.5, "vibrato_depth": 0.008}
	case 0, 1, 2, 3: // pianos, organs, guitars
		return map[string]float32{"brightness": 0.6, "breath_noise": 0.2,
			"vibrato_rate": 2, "vibrato_depth": 0.005}
	}
	return map[string]float32{"brightness": 0.55, "breath_noise": 0.25,
		"vibrato_rate": 4, "vibrato_depth": 0.01}
}

// FromScore converts a Resonata score into a format-1 SMF: track 0 is a
// conductor track (tempo FF 51, time signature FF 58, title FF 03) and
// each score track becomes an MTrk with its name, a GM program change,
// CC7/CC10 mix restore, and note-on/off pairs. Seconds map to ticks at
// metadata.BPM; scores whose times came from a tick grid round-trip
// exactly. ppq <= 0 selects DefaultTicksPerQuarter.
func FromScore(s *score.Score, ppq int) *File {
	if ppq <= 0 {
		ppq = DefaultTicksPerQuarter
	}
	bpm := s.Metadata.BPM
	if !(bpm > 0) {
		bpm = 120
	}
	secPerTick := 60 / (bpm * float64(ppq))
	toTick := func(sec float64) uint32 {
		t := roundF(sec / secPerTick)
		if t < 0 {
			t = 0
		}
		if t > float64(math.MaxUint32-1) {
			t = float64(math.MaxUint32 - 1)
		}
		return uint32(t)
	}

	f := &File{Header: Header{Format: 1, TicksPerQuarter: ppq}}

	// Conductor track: tempo, time signature, title, end.
	endTick := uint32(0)
	for i := range s.Tracks {
		for _, n := range s.Tracks[i].Notes {
			if t := toTick(n.Time + n.Duration); t > endTick {
				endTick = t
			}
		}
	}
	conductor := []Event{
		{Tick: 0, Type: EventMeta, Data1: MetaTempo, Data: be24(MicrosFromBPM(bpm))},
	}
	conductor = append(conductor, Event{Tick: 0, Type: EventMeta, Data1: MetaTimeSignature,
		Data: timeSigPayload(s.Metadata.TimeSignature)})
	if title := strings.TrimSpace(s.Metadata.Title); title != "" {
		conductor = append(conductor, Event{Tick: 0, Type: EventMeta, Data1: MetaTrackName,
			Data: []byte(title)})
	}
	conductor = append(conductor, Event{Tick: endTick, Type: EventMeta, Data1: MetaEndOfTrack})
	f.Tracks = append(f.Tracks, conductor)

	// One track per score track.
	for i := range s.Tracks {
		tr := &s.Tracks[i]
		ch := i % 15
		if ch >= 9 {
			ch++ // skip the percussion channel
		}
		events := []Event{}
		name := tr.Name
		if name == "" {
			name = tr.ID
		}
		if name != "" {
			events = append(events, Event{Tick: 0, Type: EventMeta, Data1: MetaTrackName, Data: []byte(name)})
		}
		prog := programOcarina
		if strings.EqualFold(tr.Instrument.Type, "sampler") {
			prog = programPiano
		}
		events = append(events,
			Event{Tick: 0, Type: EventProgramChange, Channel: ch, Data1: prog},
			Event{Tick: 0, Type: EventControlChange, Channel: ch, Data1: 7,
				Data2: clampInt(int(roundF(float64(tr.Volume)/0.9*127)), 0, 127)},
			Event{Tick: 0, Type: EventControlChange, Channel: ch, Data1: 10,
				Data2: clampInt(int(roundF((float64(tr.Pan)+1)/2*127)), 0, 127)},
		)
		trackEnd := uint32(0)
		for _, n := range tr.Notes {
			on := toTick(n.Time)
			off := toTick(n.Time + n.Duration)
			if off <= on {
				off = on + 1
			}
			vel := clampInt(int(roundF(float64(n.Velocity)*127)), 1, 127)
			events = append(events,
				Event{Tick: on, Type: EventNoteOn, Channel: ch, Data1: n.Pitch, Data2: vel},
				Event{Tick: off, Type: EventNoteOff, Channel: ch, Data1: n.Pitch, Data2: 64},
			)
			if off > trackEnd {
				trackEnd = off
			}
		}
		events = append(events, Event{Tick: trackEnd, Type: EventMeta, Data1: MetaEndOfTrack})
		f.Tracks = append(f.Tracks, events)
	}
	f.Header.Tracks = len(f.Tracks)
	return f
}

// be24 encodes a 24-bit big-endian tempo payload.
func be24(v int) []byte {
	return []byte{byte(v >> 16), byte(v >> 8), byte(v)}
}

// timeSigPayload builds the FF 58 payload from an "N/D" signature
// (24 MIDI clocks per click, 8 32nds per quarter).
func timeSigPayload(ts string) []byte {
	num, den := 4, 4
	if a, b, ok := strings.Cut(ts, "/"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(a)); err == nil && n > 0 {
			num = n
		}
		if d, err := strconv.Atoi(strings.TrimSpace(b)); err == nil && d > 0 {
			den = d
		}
	}
	pow := 2
	for p := 0; p <= 5; p++ {
		if 1<<p == den {
			pow = p
			break
		}
	}
	return []byte{byte(num), byte(pow), 24, 8}
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

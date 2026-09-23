package midi

import (
	"encoding/binary"
	"sort"
)

// WriteVLQ encodes value as a variable-length quantity (7 bits per byte,
// MSB continuation), capped at the 28-bit SMF maximum.
func WriteVLQ(value uint32) []byte {
	return appendVLQ(nil, value)
}

// appendVLQ appends the VLQ encoding of v to dst without intermediate
// allocations beyond slice growth.
func appendVLQ(dst []byte, v uint32) []byte {
	if v > 0x0FFFFFFF {
		v = 0x0FFFFFFF
	}
	shift := uint(21)
	for shift > 0 && v&(0x7F<<shift) == 0 {
		shift -= 7
	}
	for {
		b := byte((v >> shift) & 0x7F)
		if shift > 0 {
			b |= 0x80
		}
		dst = append(dst, b)
		if shift == 0 {
			return dst
		}
		shift -= 7
	}
}

// eventRank orders events sharing one tick: note-offs and metadata first,
// note-ons last, so a retrigger at the same tick never leaves a stuck
// note and end-of-track stays at the end.
func eventRank(e Event) int {
	switch e.Type {
	case EventNoteOff:
		return 0
	case EventMeta:
		if e.Data1 == MetaEndOfTrack {
			return 5
		}
		return 1
	case EventSysex:
		return 2
	case EventNoteOn:
		return 4
	}
	return 3
}

// EncodeTrack serializes one track's absolute-tick events into MTrk
// payload bytes: stable sort by (tick, rank), delta-time VLQs, and
// running status for repeated channel statuses (sysex/meta cancel it,
// exactly as the reader expects).
func EncodeTrack(events []Event) []byte {
	sorted := make([]Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Tick != sorted[j].Tick {
			return sorted[i].Tick < sorted[j].Tick
		}
		return eventRank(sorted[i]) < eventRank(sorted[j])
	})

	buf := make([]byte, 0, len(sorted)*4+64)
	var lastTick uint32
	var lastStatus byte
	for _, e := range sorted {
		buf = appendVLQ(buf, e.Tick-lastTick)
		lastTick = e.Tick

		switch e.Type {
		case EventMeta:
			buf = append(buf, 0xFF, byte(e.Data1))
			buf = appendVLQ(buf, uint32(len(e.Data)))
			buf = append(buf, e.Data...)
			lastStatus = 0
		case EventSysex:
			status := byte(e.Data1)
			if status != 0xF0 && status != 0xF7 {
				status = 0xF0
			}
			buf = append(buf, status)
			buf = appendVLQ(buf, uint32(len(e.Data)))
			buf = append(buf, e.Data...)
			lastStatus = 0
		default:
			status := statusByte(e)
			if status != lastStatus {
				buf = append(buf, status)
				lastStatus = status
			}
			switch e.Type {
			case EventProgramChange, EventChannelPressure:
				buf = append(buf, byte(clampInt(e.Data1, 0, 127)))
			case EventPitchBend:
				v := clampInt(e.Value+8192, 0, 16383)
				buf = append(buf, byte(v&0x7F), byte((v>>7)&0x7F))
			default: // note on/off, poly pressure, CC: two data bytes
				buf = append(buf, byte(clampInt(e.Data1, 0, 127)), byte(clampInt(e.Data2, 0, 127)))
			}
		}
	}
	return buf
}

// WriteFile serializes a complete SMF: MThd plus one MTrk chunk per
// track. A format-0 header with multiple tracks is flattened into a
// single merged track. A non-positive PPQ falls back to 480.
func WriteFile(f *File) []byte {
	format := f.Header.Format
	tracks := f.Tracks
	if format == 0 && len(tracks) > 1 {
		merged := []Event{}
		for _, tr := range tracks {
			merged = append(merged, tr...)
		}
		tracks = [][]Event{merged}
	}
	ppq := f.Header.TicksPerQuarter
	if ppq <= 0 || ppq&0x8000 != 0 {
		ppq = 480
	}

	out := make([]byte, 0, 1024)
	out = append(out, chunkHeader...)
	out = binary.BigEndian.AppendUint32(out, 6)
	out = binary.BigEndian.AppendUint16(out, uint16(format))
	out = binary.BigEndian.AppendUint16(out, uint16(len(tracks)))
	out = binary.BigEndian.AppendUint16(out, uint16(ppq))
	for _, tr := range tracks {
		payload := EncodeTrack(tr)
		out = append(out, chunkTrack...)
		out = binary.BigEndian.AppendUint32(out, uint32(len(payload)))
		out = append(out, payload...)
	}
	return out
}

// statusByte reconstructs the channel status byte for an event.
func statusByte(e Event) byte {
	ch := byte(e.Channel & 0x0F)
	switch e.Type {
	case EventNoteOff:
		return 0x80 | ch
	case EventNoteOn:
		return 0x90 | ch
	case EventPolyPressure:
		return 0xA0 | ch
	case EventControlChange:
		return 0xB0 | ch
	case EventProgramChange:
		return 0xC0 | ch
	case EventChannelPressure:
		return 0xD0 | ch
	case EventPitchBend:
		return 0xE0 | ch
	}
	return 0
}

// clampInt bounds v to [lo, hi].
func clampInt(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

package midi

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ReadVLQ decodes a variable-length quantity at pos using the classic
// lenient signature: 7 bits per byte, MSB set means "continue". It never
// panics — truncated input stops at the end of data, and overlong VLQs
// cap at the 28-bit SMF maximum. Use ReadFile for strict error reporting.
func ReadVLQ(data []byte, pos int) (value uint32, newPos int) {
	v, p, _ := readVLQ(data, pos)
	if p > len(data) {
		p = len(data)
	}
	return v, p
}

// readVLQ decodes a VLQ with strict bounds and length checking.
func readVLQ(data []byte, pos int) (value uint32, newPos int, err error) {
	if pos < 0 || pos >= len(data) {
		return 0, pos, errors.New("midi: VLQ starts past end of data")
	}
	var v uint32
	for i := 0; ; i++ {
		if i == 4 {
			return 0, pos, errors.New("midi: VLQ longer than 4 bytes")
		}
		if pos >= len(data) {
			return 0, pos, errors.New("midi: truncated VLQ")
		}
		b := data[pos]
		pos++
		v = (v << 7) | uint32(b&0x7F)
		if b&0x80 == 0 {
			return v, pos, nil
		}
	}
}

// ReadFile parses an SMF (format 0 or 1) from raw bytes. Leading junk
// before MThd is skipped, unknown chunks between tracks are ignored,
// running status is tracked per track, and every read is bounds-checked:
// malformed files return errors, never panics. SMPTE timecode division
// is rejected with a clear message (PPQ timing only).
func ReadFile(data []byte) (*File, error) {
	f := &File{}

	// Locate MThd, tolerating prefixed junk.
	pos := -1
	for i := 0; i+8 <= len(data); i++ {
		if string(data[i:i+4]) == chunkHeader {
			pos = i
			break
		}
	}
	if pos < 0 {
		return nil, errors.New("midi: no MThd header found")
	}
	hdrLen := int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
	pos += 8
	if hdrLen < 6 || pos+6 > len(data) {
		return nil, errors.New("midi: truncated MThd chunk")
	}
	format := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	division := int(binary.BigEndian.Uint16(data[pos+4 : pos+6]))
	if division&0x8000 != 0 {
		return nil, errors.New("midi: SMPTE timecode division not supported (PPQ only)")
	}
	if division == 0 {
		return nil, errors.New("midi: zero PPQ division")
	}
	pos += hdrLen

	// Walk chunks: parse MTrk, skip anything else.
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		pos += 8
		if size < 0 || pos+size > len(data) {
			if id == chunkTrack {
				return nil, fmt.Errorf("midi: truncated %s chunk (need %d bytes, have %d)", id, size, len(data)-pos)
			}
			break // trailing junk after the last complete chunk
		}
		if id == chunkTrack {
			events, err := parseTrack(data[pos : pos+size])
			if err != nil {
				return nil, err
			}
			f.Tracks = append(f.Tracks, events)
		}
		pos += size
	}
	if len(f.Tracks) == 0 {
		return nil, errors.New("midi: no complete MTrk chunks found")
	}
	f.Header = Header{Format: format, Tracks: len(f.Tracks), TicksPerQuarter: division}
	return f, nil
}

// parseTrack decodes one MTrk payload into absolute-tick events using a
// running-status state machine. Per the SMF spec, sysex and meta events
// cancel running status; a data byte without an active status is an error.
func parseTrack(data []byte) ([]Event, error) {
	var events []Event
	pos := 0
	var tick uint32
	var lastStatus byte

	for pos < len(data) {
		delta, np, err := readVLQ(data, pos)
		if err != nil {
			return nil, fmt.Errorf("midi: at tick %d: %w", tick, err)
		}
		pos = np
		tick += delta
		if pos >= len(data) {
			return nil, fmt.Errorf("midi: truncated event after delta at tick %d", tick)
		}

		status := data[pos]
		if status < 0x80 {
			// Running status: the status byte is implied.
			if lastStatus == 0 {
				return nil, fmt.Errorf("midi: tick %d: data byte %#02x with no running status", tick, status)
			}
			status = lastStatus
		} else {
			pos++
			if status < 0xF0 {
				lastStatus = status
			} else {
				lastStatus = 0 // sysex/meta cancel running status
			}
		}

		ev := Event{Tick: tick}
		switch {
		case status == 0xFF:
			if pos >= len(data) {
				return nil, fmt.Errorf("midi: tick %d: truncated meta type", tick)
			}
			ev.Type = EventMeta
			ev.Data1 = int(data[pos])
			pos++
			payload, np, err := readPayload(data, pos)
			if err != nil {
				return nil, fmt.Errorf("midi: tick %d: meta %#02x: %w", tick, ev.Data1, err)
			}
			ev.Data = payload
			pos = np
		case status == 0xF0 || status == 0xF7:
			ev.Type = EventSysex
			ev.Data1 = int(status)
			payload, np, err := readPayload(data, pos)
			if err != nil {
				return nil, fmt.Errorf("midi: tick %d: sysex: %w", tick, err)
			}
			ev.Data = payload
			pos = np
		case status >= 0xF1 && status <= 0xFE:
			// System common/realtime leaks: consume the fixed-length
			// payload so the stream stays aligned.
			ev.Type = EventSysex
			ev.Data1 = int(status)
			n := systemCommonLen(status)
			if pos+n > len(data) {
				return nil, fmt.Errorf("midi: tick %d: truncated system message %#02x", tick, status)
			}
			ev.Data = append([]byte(nil), data[pos:pos+n]...)
			pos += n
		default:
			n := channelDataLen(status)
			if pos+n > len(data) {
				return nil, fmt.Errorf("midi: tick %d: truncated event %#02x", tick, status)
			}
			ev.Channel = int(status & 0x0F)
			d1 := int(data[pos] & 0x7F)
			ev.Data1 = d1
			switch status & 0xF0 {
			case 0x80:
				ev.Type = EventNoteOff
				ev.Data2 = int(data[pos+1] & 0x7F)
			case 0x90:
				ev.Type = EventNoteOn
				ev.Data2 = int(data[pos+1] & 0x7F)
			case 0xA0:
				ev.Type = EventPolyPressure
				ev.Data2 = int(data[pos+1] & 0x7F)
			case 0xB0:
				ev.Type = EventControlChange
				ev.Data2 = int(data[pos+1] & 0x7F)
			case 0xC0:
				ev.Type = EventProgramChange
			case 0xD0:
				ev.Type = EventChannelPressure
			case 0xE0:
				ev.Type = EventPitchBend
				ev.Value = int(data[pos+1]&0x7F)<<7 | d1
				ev.Value -= 8192 // center at zero; Data1 keeps the wire LSB
			}
			pos += n
		}
		events = append(events, ev)
	}
	return events, nil
}

// readPayload reads a VLQ length plus that many bytes, returning an owned
// copy of the payload.
func readPayload(data []byte, pos int) ([]byte, int, error) {
	length, np, err := readVLQ(data, pos)
	if err != nil {
		return nil, pos, err
	}
	if uint64(length) > uint64(len(data)-np) {
		return nil, np, fmt.Errorf("payload length %d exceeds remaining %d bytes", length, len(data)-np)
	}
	payload := append([]byte(nil), data[np:np+int(length)]...)
	return payload, np + int(length), nil
}

// channelDataLen is the data-byte count for a channel status.
func channelDataLen(status byte) int {
	switch status & 0xF0 {
	case 0xC0, 0xD0:
		return 1
	}
	return 2
}

// systemCommonLen is the payload length of system common messages that
// occasionally leak into files (realtime messages have zero bytes).
func systemCommonLen(status byte) int {
	switch status {
	case 0xF1, 0xF3:
		return 1
	case 0xF2:
		return 2
	}
	return 0
}

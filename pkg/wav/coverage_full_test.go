package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"
)

type failOnWrite struct{}

func (f *failOnWrite) Write(p []byte) (int, error)                  { return 0, errors.New("fail write") }
func (f *failOnWrite) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type failOnSecondWrite struct {
	writes int
}

func (f *failOnSecondWrite) Write(p []byte) (int, error) {
	f.writes++
	if f.writes > 1 {
		return 0, errors.New("fail second write")
	}
	return len(p), nil
}
func (f *failOnSecondWrite) Seek(offset int64, whence int) (int64, error) { return 0, nil }

type failOnSecondSeek struct {
	seeks int
}

func (f *failOnSecondSeek) Write(p []byte) (int, error) { return len(p), nil }
func (f *failOnSecondSeek) Seek(offset int64, whence int) (int64, error) {
	f.seeks++
	if f.seeks == 2 {
		return 0, errors.New("fail second seek")
	}
	return 0, nil
}

type scriptSeeker struct {
	data       []byte
	pos        int
	failOnSeek func(off int64, whence int, count int) bool
	failOnRead func(p []byte, count int) bool
	seekCount  int
	readCount  int
}

func (s *scriptSeeker) Read(p []byte) (int, error) {
	s.readCount++
	if s.failOnRead != nil && s.failOnRead(p, s.readCount) {
		return 0, errors.New("custom read fail")
	}
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

func (s *scriptSeeker) Seek(offset int64, whence int) (int64, error) {
	s.seekCount++
	if s.failOnSeek != nil && s.failOnSeek(offset, whence, s.seekCount) {
		return 0, errors.New("custom seek fail")
	}
	switch whence {
	case io.SeekStart:
		s.pos = int(offset)
	case io.SeekCurrent:
		s.pos += int(offset)
	case io.SeekEnd:
		s.pos = len(s.data) + int(offset)
	}
	return int64(s.pos), nil
}

// TestWavWriterCoverageEdges verifies WAV writer error conditions during header generation, frame writing, and size patching.
func TestWavWriterCoverageEdges(t *testing.T) {
	if _, err := NewWriterBits(&failOnWrite{}, PCM24, 24, 44100, 2); err == nil {
		t.Fatal("expected error on header write")
	}

	wSecond := &failOnSecondWrite{}
	wr, err := NewWriterBits(wSecond, PCM24, 24, 44100, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wr.WriteFrames([]float32{0.5, 0.5}); err == nil {
		t.Fatal("expected error on frame write")
	}

	var buf bytes.Buffer
	wrRiff, _ := NewWriterBits(nopWriteSeeker{&buf}, PCM16, 16, 44100, 1)
	wrRiff.frames = math.MaxUint32
	if err := wrRiff.Close(); err == nil {
		t.Fatal("expected error on >4GB RIFF size")
	}

	wSeek := &failOnSecondSeek{}
	wrPatch, _ := NewWriterBits(wSeek, PCM16, 16, 44100, 1)
	wrPatch.frames = 10
	if err := wrPatch.Close(); err == nil {
		t.Fatal("expected error on patchAt seek")
	}
}

type nopWriteSeeker struct {
	io.Writer
}

func (n nopWriteSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

// TestWavReaderCoverageEdges verifies WAV reader error handling for truncated chunks and malformed format blocks.
func TestWavReaderCoverageEdges(t *testing.T) {
	truncFmt := []byte("RIFF\x20\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00")
	if _, _, err := Decode(bytes.NewReader(truncFmt)); err == nil {
		t.Fatal("expected error on truncated fmt read")
	}

	truncFmtExt := []byte("RIFF\x30\x00\x00\x00WAVEfmt \x14\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00\x00\x00\x00\x00")
	sFmtExt := &scriptSeeker{
		data: truncFmtExt,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekCurrent && off == 4
		},
	}
	if _, _, err := Decode(sFmtExt); err == nil {
		t.Fatal("expected error on fmt ext skip")
	}

	truncSmpl := []byte("RIFF\x40\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00smpl\x24\x00\x00\x00\x00\x00")
	if _, _, err := Decode(bytes.NewReader(truncSmpl)); err == nil {
		t.Fatal("expected error on truncated smpl read")
	}

	smplHdr := make([]byte, 36)
	binary.LittleEndian.PutUint32(smplHdr[28:], 1)
	truncSmplLoop := append([]byte("RIFF\x50\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00smpl\x3c\x00\x00\x00"), smplHdr...)
	if _, _, err := Decode(bytes.NewReader(truncSmplLoop)); err == nil {
		t.Fatal("expected error on truncated smpl loop record")
	}

	sJunk := &scriptSeeker{
		data: []byte("RIFF\x30\x00\x00\x00WAVEjunk\x64\x00\x00\x00"),
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekCurrent && off == 0x64
		},
	}
	if _, _, err := Decode(sJunk); err == nil {
		t.Fatal("expected error on truncated unknown chunk")
	}

	validWav := []byte("RIFF\x2c\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x00\x00\x00\x00")

	s1 := &scriptSeeker{
		data: validWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekCurrent
		},
	}
	if _, _, err := Decode(s1); err == nil {
		t.Fatal("expected error for seek current in decode")
	}

	s2 := &scriptSeeker{
		data: validWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekEnd
		},
	}
	if _, _, err := Decode(s2); err == nil {
		t.Fatal("expected error for seek end in decode")
	}

	s3 := &scriptSeeker{
		data: validWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekStart && count == 3
		},
	}
	if _, _, err := Decode(s3); err == nil {
		t.Fatal("expected error for seek start in decode")
	}

	sEnd := &scriptSeeker{
		data: validWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekStart && count == 4
		},
	}
	if _, _, err := Decode(sEnd); err == nil {
		t.Fatal("expected error for seek to end of data chunk")
	}

	info := Info{Channels: 1, BitsPerSample: 16}
	if _, _, err := decodeData(bytes.NewReader(nil), info, -5); err == nil {
		t.Fatal("expected error on negative size")
	}

	s4 := &scriptSeeker{
		data: validWav,
		failOnRead: func(p []byte, count int) bool {
			return true
		},
	}
	if _, _, err := decodeData(s4, info, 4); err == nil {
		t.Fatal("expected error on custom read failure")
	}

	oddChunkWav := []byte("RIFF\x35\x00\x00\x00WAVEjunk\x01\x00\x00\x00\x00\x00fmt \x10\x00\x00\x00\x01\x00\x01\x00\x44\xac\x00\x00\x88\x58\x01\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x00\x00\x00\x00")
	sOddSeekStart := &scriptSeeker{
		data: oddChunkWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekStart && count == 4
		},
	}
	if _, _, err := Decode(sOddSeekStart); err == nil {
		t.Fatal("expected error on odd chunk seek start fail")
	}

	sOddSkip := &scriptSeeker{
		data: oddChunkWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return whence == io.SeekCurrent && count == 5
		},
	}
	if _, _, err := Decode(sOddSkip); err == nil {
		t.Fatal("expected error on odd chunk skip fail")
	}

	sOddErrPos := &scriptSeeker{
		data: oddChunkWav,
		failOnSeek: func(off int64, whence int, count int) bool {
			return count == 2 || count == 4
		},
	}
	if _, _, err := Decode(sOddErrPos); err == nil {
		t.Fatal("expected error on odd chunk errPos skip fail")
	}
}

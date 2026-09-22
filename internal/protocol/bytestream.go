// Package protocol implements the xBrawl TCP/UDP wire codecs byte-for-byte
// compatible with the Unity client (ByteStream.cs) and the C# emulator
// (Emulator.Protocol/Codecs/ByteStream.cs).
package protocol

import (
	"encoding/binary"
	"errors"
	"unicode/utf8"
)

const (
	MaxCollectionCount  = 100000
	MaxStringByteLength = 1024 * 1024
	MaxTCPFramePayload  = 32 * 1024 * 1024
	TCPHeaderSize       = 6
	MaxPendingSendBytes = 4 * 1024 * 1024
	MaxPendingSendMsgs  = 1024
	MaxMessagesPerTick  = 128
	ReceiveChunkSize    = 16 * 1024
)

var (
	ErrEndOfStream  = errors.New("bytestream: end of stream")
	ErrInvalidBool  = errors.New("bytestream: invalid boolean value")
	ErrBadCount     = errors.New("bytestream: invalid collection count")
	ErrStringTooBig = errors.New("bytestream: string exceeds 1MiB limit")
	ErrBadBytesLen  = errors.New("bytestream: invalid byte array length")
	ErrBadStrLen    = errors.New("bytestream: invalid string length")
)

type Writer struct {
	Buf []byte
}

func NewWriter() *Writer { return &Writer{Buf: make([]byte, 0, 128)} }

func (w *Writer) Bytes() []byte { return w.Buf }
func (w *Writer) Len() int      { return len(w.Buf) }
func (w *Writer) Reset()        { w.Buf = w.Buf[:0] }

func (w *Writer) WriteByte(v byte) error { w.Buf = append(w.Buf, v); return nil }
func (w *Writer) WriteBool(v bool) {
	if v {
		w.Buf = append(w.Buf, 1)
	} else {
		w.Buf = append(w.Buf, 0)
	}
}
func (w *Writer) WriteShort(v int16) {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], uint16(v))
	w.Buf = append(w.Buf, b[:]...)
}
func (w *Writer) WriteInt(v int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v))
	w.Buf = append(w.Buf, b[:]...)
}
func (w *Writer) WriteLong(v int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	w.Buf = append(w.Buf, b[:]...)
}
func (w *Writer) WriteFloat(v float32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], Float32bits(v))
	w.Buf = append(w.Buf, b[:]...)
}
func (w *Writer) WriteDouble(v float64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], Float64bits(v))
	w.Buf = append(w.Buf, b[:]...)
}

func (w *Writer) WriteBytes(b []byte) {
	if len(b) == 0 {
		w.WriteInt(0)
		return
	}
	w.WriteInt(int32(len(b)))
	w.Buf = append(w.Buf, b...)
}

func (w *Writer) WriteBytesNullable(b []byte, null bool) {
	if null {
		w.WriteInt(-1)
		return
	}
	w.WriteBytes(b)
}

func (w *Writer) WriteString(s string) {
	w.WriteStringNullable(&s, false)
}

func (w *Writer) WriteStringNull() { w.WriteInt(-1) }

func (w *Writer) WriteStringNullable(s *string, isNull bool) {
	if isNull || s == nil {
		w.WriteInt(-1)
		return
	}
	n := len(*s)
	if n == 0 {
		w.WriteInt(0)
		return
	}
	w.WriteInt(int32(n))
	w.Buf = append(w.Buf, (*s)...)
}

func (w *Writer) WriteRaw(b []byte) { w.Buf = append(w.Buf, b...) }

func (w *Writer) WriteIntCollection(items []int32) {
	w.WriteInt(int32(len(items)))
	for _, v := range items {
		w.WriteInt(v)
	}
}

type Reader struct {
	Buf []byte
	Off int
}

func NewReader(b []byte) *Reader { return &Reader{Buf: b} }

func (r *Reader) Left() int   { return len(r.Buf) - r.Off }
func (r *Reader) Ended() bool { return r.Off >= len(r.Buf) }
func (r *Reader) Offset() int { return r.Off }

func (r *Reader) ReadByte() (byte, error) {
	if r.Off < 0 || r.Off >= len(r.Buf) {
		return 0, ErrEndOfStream
	}
	v := r.Buf[r.Off]
	r.Off++
	return v, nil
}

func (r *Reader) MustByte() byte {
	v, err := r.ReadByte()
	if err != nil {
		panic(err)
	}
	return v
}

func (r *Reader) ReadBool() (bool, error) {
	v, err := r.ReadByte()
	if err != nil {
		return false, err
	}
	switch v {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, ErrInvalidBool
	}
}

func (r *Reader) ReadShort() (int16, error) {
	if r.Left() < 2 {
		return 0, ErrEndOfStream
	}
	v := int16(binary.BigEndian.Uint16(r.Buf[r.Off:]))
	r.Off += 2
	return v, nil
}

func (r *Reader) ReadInt() (int32, error) {
	if r.Left() < 4 {
		return 0, ErrEndOfStream
	}
	v := int32(binary.BigEndian.Uint32(r.Buf[r.Off:]))
	r.Off += 4
	return v, nil
}

func (r *Reader) ReadLong() (int64, error) {
	if r.Left() < 8 {
		return 0, ErrEndOfStream
	}
	v := int64(binary.BigEndian.Uint64(r.Buf[r.Off:]))
	r.Off += 8
	return v, nil
}

func (r *Reader) ReadFloat() (float32, error) {
	v, err := r.ReadInt()
	if err != nil {
		return 0, err
	}
	return Float32frombits(uint32(v)), nil
}

func (r *Reader) ReadDouble() (float64, error) {
	v, err := r.ReadLong()
	if err != nil {
		return 0, err
	}
	return Float64frombits(uint64(v)), nil
}

func (r *Reader) ReadRange(n int) ([]byte, error) {
	if n == 0 {
		return []byte{}, nil
	}
	if n < 0 {
		return nil, ErrBadBytesLen
	}
	if n > r.Left() {
		return nil, ErrEndOfStream
	}
	out := make([]byte, n)
	copy(out, r.Buf[r.Off:r.Off+n])
	r.Off += n
	return out, nil
}

func (r *Reader) ReadBytes() ([]byte, error) {
	n, err := r.ReadInt()
	if err != nil {
		return nil, err
	}
	if n == -1 {
		return nil, nil
	}
	if n < -1 {
		return nil, ErrBadBytesLen
	}
	return r.ReadRange(int(n))
}

func (r *Reader) ReadString() (string, bool, error) {
	n, err := r.ReadInt()
	if err != nil {
		return "", false, err
	}
	if n > MaxStringByteLength {
		return "", false, ErrStringTooBig
	}
	if n == -1 {
		return "", true, nil
	}
	if n < -1 {
		return "", false, ErrBadStrLen
	}
	if int(n) > r.Left() {
		return "", false, ErrEndOfStream
	}
	s := string(r.Buf[r.Off : r.Off+int(n)])
	r.Off += int(n)
	if !utf8.ValidString(s) {
		// Client uses Encoding.UTF8.GetString which replaces invalid sequences;
		// we keep raw bytes as string to stay wire-compatible.
	}
	return s, false, nil
}

func (r *Reader) ReadCollectionCount() (int, error) {
	n, err := r.ReadInt()
	if err != nil {
		return 0, err
	}
	if n < 0 || n > MaxCollectionCount {
		return 0, ErrBadCount
	}
	if int(n) > r.Left() {
		return 0, ErrEndOfStream
	}
	return int(n), nil
}

// BitStream is a bit-exact port of Emulator.Protocol/Codecs/BitStream.cs
// (itself mirrored from the Unity client's Utils/BitStream.cs).
// Bits are packed LSB-first inside each byte.
package protocol

import (
	"errors"
	"fmt"
)

var (
	ErrBitTruncated = errors.New("bitstream: truncated bit stream")
	ErrBitRange     = errors.New("bitstream: value out of range")
)

type BitWriter struct {
	Buf []byte
	pos int
	bit uint
}

func NewBitWriter(size int) *BitWriter {
	if size <= 0 {
		size = 64
	}
	return &BitWriter{Buf: make([]byte, size)}
}

func (w *BitWriter) Reset() {

	n := w.pos + 1
	if n > len(w.Buf) {
		n = len(w.Buf)
	}
	clear(w.Buf[:n])
	w.pos = 0
	w.bit = 0
}

func (w *BitWriter) Offset() int {
	if w.bit != 0 {
		return w.pos + 1
	}
	return w.pos
}

func (w *BitWriter) Bytes() []byte { return w.Buf[:w.Offset()] }

func (w *BitWriter) ensure() {
	if w.pos >= len(w.Buf) {
		n := make([]byte, len(w.Buf)*2+16)
		copy(n, w.Buf)
		w.Buf = n
	}
}

func (w *BitWriter) WriteBit(v int) {
	w.ensure()
	w.Buf[w.pos] |= byte(v << w.bit)
	w.bit++
	if w.bit == 8 {
		w.bit = 0
		w.pos++
		w.ensure()
	}
}

func (w *BitWriter) WriteBool(v bool) {
	if v {
		w.WriteBit(1)
	} else {
		w.WriteBit(0)
	}
}

func (w *BitWriter) WriteUnsigned(value, size int) {
	if size <= 0 || size > 31 {
		panic(fmt.Sprintf("bitstream: bad size %d", size))
	}
	if value < 0 || (size < 32 && value > ^(-1<<size)) || (size == 32 && uint32(value) > 0xFFFFFFFF) {
		panic(fmt.Sprintf("BitStream out of range (value=%d, size=%d)", value, size))
	}
	for size > 0 {
		w.ensure()
		bits := int(8 - w.bit)
		w.Buf[w.pos] |= byte((value << w.bit) & 0xff)
		if bits > size {
			w.bit += uint(size)
			value = 0
			size = 0
		} else {
			w.pos++
			w.bit = 0
			value >>= bits
			size -= bits
		}
	}
}

func (w *BitWriter) WriteSigned(value, size int) {
	if value >= 0 {
		w.WriteBit(1)
		w.WriteUnsigned(value, size)
	} else {
		w.WriteBit(0)
		w.WriteUnsigned(-value, size)
	}
}

func leadingZeros32(x int32) int {
	u := uint32(x)
	u |= u >> 1
	u |= u >> 2
	u |= u >> 4
	u |= u >> 8
	u |= u >> 16
	u -= (u >> 1) & 0x55555555
	u = ((u >> 2) & 0x33333333) + (u & 0x33333333)
	u = ((u >> 4) + u) & 0x0f0f0f0f
	u += u >> 8
	u += u >> 16
	return 32 - int(u&0x3f)
}

func (w *BitWriter) WriteSized(value, size int) {
	vs := 1
	if value != 0 {
		vs = 32 - leadingZeros32(int32(value))
		if vs < 1 {
			vs = 1
		}
	}
	w.WriteUnsigned(vs-1, size)
	w.WriteUnsigned(value, vs)
}

func (w *BitWriter) WriteOptionalSized(value, size int) {
	if value != 0 {
		w.WriteBit(0)
		w.WriteSized(value, size)
	} else {
		w.WriteBit(1)
	}
}

func (w *BitWriter) WritePositiveVIntMax131072(v int) { w.WriteSized(v, 3) }
func (w *BitWriter) WritePositiveVIntMax65535(v int)  { w.WriteSized(v, 4) }
func (w *BitWriter) WritePositiveVIntMax255OftenZero(v int) {
	w.WriteOptionalSized(v, 3)
}
func (w *BitWriter) WritePositiveVIntMax65535OftenZero(v int) {
	w.WriteOptionalSized(v, 4)
}
func (w *BitWriter) WritePositiveIntMax3(v int)   { w.WriteUnsigned(v, 2) }
func (w *BitWriter) WritePositiveIntMax7(v int)   { w.WriteUnsigned(v, 3) }
func (w *BitWriter) WritePositiveIntMax15(v int)  { w.WriteUnsigned(v, 4) }
func (w *BitWriter) WriteU4(v int)                { w.WriteUnsigned(v, 4) }
func (w *BitWriter) WritePositiveIntMax31(v int)  { w.WriteUnsigned(v, 5) }
func (w *BitWriter) WritePositiveIntMax63(v int)  { w.WriteUnsigned(v, 6) }
func (w *BitWriter) WriteU6(v int)                { w.WriteUnsigned(v, 6) }
func (w *BitWriter) WritePositiveIntMax127(v int) { w.WriteUnsigned(v, 7) }
func (w *BitWriter) WritePositiveIntMax255(v int) { w.WriteUnsigned(v, 8) }
func (w *BitWriter) WritePositiveIntMax511(v int) { w.WriteUnsigned(v, 9) }
func (w *BitWriter) WritePositiveIntMax1023(v int) {
	w.WriteUnsigned(v, 10)
}
func (w *BitWriter) WriteU11(v int) { w.WriteUnsigned(v, 11) }
func (w *BitWriter) WritePositiveIntMax4095(v int) {
	w.WriteUnsigned(v, 12)
}
func (w *BitWriter) WritePositiveIntMax8191(v int) {
	w.WriteUnsigned(v, 13)
}
func (w *BitWriter) WritePositiveIntMax16383(v int) {
	w.WriteUnsigned(v, 14)
}
func (w *BitWriter) WritePositiveIntMax32767(v int) {
	w.WriteUnsigned(v, 15)
}
func (w *BitWriter) WriteU16(v int) { w.WriteUnsigned(v, 16) }
func (w *BitWriter) WritePositiveIntMax131072(v int) {
	w.WriteUnsigned(v, 17)
}
func (w *BitWriter) WriteU18(v int) { w.WriteUnsigned(v, 18) }
func (w *BitWriter) WritePositiveIntMax524287(v int) {
	w.WriteUnsigned(v, 19)
}
func (w *BitWriter) WriteU20(v int) { w.WriteUnsigned(v, 20) }
func (w *BitWriter) WritePositiveIntMax2097152(v int) {
	w.WriteUnsigned(v, 21)
}
func (w *BitWriter) WritePositiveIntMax2097152OftenZero(v int) {
	if v == 0 {
		w.WriteBit(1)
		return
	}
	w.WriteBit(0)
	w.WriteUnsigned(v, 21)
}
func (w *BitWriter) WriteU22(v int) { w.WriteUnsigned(v, 22) }
func (w *BitWriter) WriteU23(v int) { w.WriteUnsigned(v, 23) }
func (w *BitWriter) WriteU24(v int) { w.WriteUnsigned(v, 24) }
func (w *BitWriter) WriteU25(v int) { w.WriteUnsigned(v, 25) }
func (w *BitWriter) WriteU26(v int) { w.WriteUnsigned(v, 26) }
func (w *BitWriter) WriteU27(v int) { w.WriteUnsigned(v, 27) }
func (w *BitWriter) WriteU28(v int) { w.WriteUnsigned(v, 28) }
func (w *BitWriter) WriteU29(v int) { w.WriteUnsigned(v, 29) }
func (w *BitWriter) WriteU30(v int) { w.WriteUnsigned(v, 30) }
func (w *BitWriter) WritePositiveIntMax2147483647(v int) {
	w.WriteUnsigned(v, 31)
}
func (w *BitWriter) WriteIntMax1(v int)     { w.WriteSigned(v, 1) }
func (w *BitWriter) WriteS1(v int)          { w.WriteSigned(v, 1) }
func (w *BitWriter) WriteS2(v int)          { w.WriteSigned(v, 2) }
func (w *BitWriter) WriteIntMax7(v int)     { w.WriteSigned(v, 3) }
func (w *BitWriter) WriteIntMax15(v int)    { w.WriteSigned(v, 4) }
func (w *BitWriter) WriteS5(v int)          { w.WriteSigned(v, 5) }
func (w *BitWriter) WriteS6(v int)          { w.WriteSigned(v, 6) }
func (w *BitWriter) WriteS7(v int)          { w.WriteSigned(v, 7) }
func (w *BitWriter) WriteS8(v int)          { w.WriteSigned(v, 8) }
func (w *BitWriter) WriteS9(v int)          { w.WriteSigned(v, 9) }
func (w *BitWriter) WriteIntMax1023(v int)  { w.WriteSigned(v, 10) }
func (w *BitWriter) WriteS10(v int)         { w.WriteSigned(v, 10) }
func (w *BitWriter) WriteS11(v int)         { w.WriteSigned(v, 11) }
func (w *BitWriter) WriteS12(v int)         { w.WriteSigned(v, 12) }
func (w *BitWriter) WriteS13(v int)         { w.WriteSigned(v, 13) }
func (w *BitWriter) WriteS14(v int)         { w.WriteSigned(v, 14) }
func (w *BitWriter) WriteIntMax32767(v int) { w.WriteSigned(v, 15) }
func (w *BitWriter) WriteIntMax65535(v int) { w.WriteSigned(v, 16) }
func (w *BitWriter) WriteS17(v int)         { w.WriteSigned(v, 17) }
func (w *BitWriter) WriteS18(v int)         { w.WriteSigned(v, 18) }
func (w *BitWriter) WriteS19(v int)         { w.WriteSigned(v, 19) }
func (w *BitWriter) WriteS20(v int)         { w.WriteSigned(v, 20) }
func (w *BitWriter) WriteS21(v int)         { w.WriteSigned(v, 21) }
func (w *BitWriter) WriteS22(v int)         { w.WriteSigned(v, 22) }
func (w *BitWriter) WriteS23(v int)         { w.WriteSigned(v, 23) }
func (w *BitWriter) WriteS24(v int)         { w.WriteSigned(v, 24) }
func (w *BitWriter) WriteS25(v int)         { w.WriteSigned(v, 25) }
func (w *BitWriter) WriteS26(v int)         { w.WriteSigned(v, 26) }
func (w *BitWriter) WriteS27(v int)         { w.WriteSigned(v, 27) }
func (w *BitWriter) WriteS28(v int)         { w.WriteSigned(v, 28) }
func (w *BitWriter) WriteS29(v int)         { w.WriteSigned(v, 29) }
func (w *BitWriter) WriteS30(v int)         { w.WriteSigned(v, 30) }
func (w *BitWriter) WriteS31(v int)         { w.WriteSigned(v, 31) }

type BitReader struct {
	Buf []byte
	pos int
	bit uint
}

func NewBitReader(b []byte) *BitReader { return &BitReader{Buf: b} }

func (r *BitReader) ReadBit() (int, error) {
	if r.pos >= len(r.Buf) {
		return 0, ErrBitTruncated
	}
	v := (r.Buf[r.pos] >> r.bit) & 1
	r.bit++
	if r.bit == 8 {
		r.bit = 0
		r.pos++
	}
	return int(v), nil
}

func (r *BitReader) ReadBool() (bool, error) {
	v, err := r.ReadBit()
	return v != 0, err
}

func (r *BitReader) ReadUnsigned(size int) (int, error) {
	if int64(r.pos)*8+int64(r.bit)+int64(size) > int64(len(r.Buf))*8 {
		return 0, ErrBitTruncated
	}
	value := 0
	remaining := size
	for remaining > 0 {
		buffValue := (r.Buf[r.pos] >> r.bit) & 0xff
		bits := int(8 - r.bit)
		off := size - remaining
		var readCount int
		if bits > remaining {
			readCount = remaining
			r.bit += uint(remaining)
		} else {
			readCount = bits
			r.pos++
			r.bit = 0
		}
		mask := (1 << readCount) - 1
		value |= (int(buffValue) & mask) << off
		remaining -= readCount
	}
	return value, nil
}

func (r *BitReader) ReadSigned(size int) (int, error) {
	sign, err := r.ReadBit()
	if err != nil {
		return 0, err
	}
	mag, err := r.ReadUnsigned(size)
	if err != nil {
		return 0, err
	}
	return mag * (2*sign - 1), nil
}

func (r *BitReader) ReadSized(size int) (int, error) {
	vs, err := r.ReadUnsigned(size)
	if err != nil {
		return 0, err
	}
	return r.ReadUnsigned(vs + 1)
}

func (r *BitReader) ReadOptionalSized(size int) (int, error) {
	b, err := r.ReadBit()
	if err != nil {
		return 0, err
	}
	if b == 1 {
		return 0, nil
	}
	return r.ReadSized(size)
}

func (r *BitReader) ReadPositiveVIntMax131072() (int, error) { return r.ReadSized(3) }
func (r *BitReader) ReadPositiveVIntMax65535() (int, error)  { return r.ReadSized(4) }
func (r *BitReader) ReadPositiveVIntMax255OftenZero() (int, error) {
	return r.ReadOptionalSized(3)
}
func (r *BitReader) ReadPositiveVIntMax65535OftenZero() (int, error) {
	return r.ReadOptionalSized(4)
}
func (r *BitReader) ReadPositiveIntMax3() (int, error)   { return r.ReadUnsigned(2) }
func (r *BitReader) ReadPositiveIntMax7() (int, error)   { return r.ReadUnsigned(3) }
func (r *BitReader) ReadPositiveIntMax15() (int, error)  { return r.ReadUnsigned(4) }
func (r *BitReader) ReadU4() (int, error)                { return r.ReadUnsigned(4) }
func (r *BitReader) ReadPositiveIntMax31() (int, error)  { return r.ReadUnsigned(5) }
func (r *BitReader) ReadPositiveIntMax63() (int, error)  { return r.ReadUnsigned(6) }
func (r *BitReader) ReadPositiveIntMax127() (int, error) { return r.ReadUnsigned(7) }
func (r *BitReader) ReadPositiveIntMax255() (int, error) { return r.ReadUnsigned(8) }
func (r *BitReader) ReadPositiveIntMax511() (int, error) { return r.ReadUnsigned(9) }
func (r *BitReader) ReadPositiveIntMax1023() (int, error) {
	return r.ReadUnsigned(10)
}
func (r *BitReader) ReadU11() (int, error) { return r.ReadUnsigned(11) }
func (r *BitReader) ReadPositiveIntMax4095() (int, error) {
	return r.ReadUnsigned(12)
}
func (r *BitReader) ReadPositiveIntMax8191() (int, error) {
	return r.ReadUnsigned(13)
}
func (r *BitReader) ReadPositiveIntMax16383() (int, error) {
	return r.ReadUnsigned(14)
}
func (r *BitReader) ReadPositiveIntMax32767() (int, error) {
	return r.ReadUnsigned(15)
}
func (r *BitReader) ReadU16() (int, error) { return r.ReadUnsigned(16) }
func (r *BitReader) ReadPositiveIntMax131072() (int, error) {
	return r.ReadUnsigned(17)
}
func (r *BitReader) ReadU18() (int, error) { return r.ReadUnsigned(18) }
func (r *BitReader) ReadPositiveIntMax524287() (int, error) {
	return r.ReadUnsigned(19)
}
func (r *BitReader) ReadU20() (int, error) { return r.ReadUnsigned(20) }
func (r *BitReader) ReadPositiveIntMax2097152() (int, error) {
	return r.ReadUnsigned(21)
}
func (r *BitReader) ReadPositiveIntMax2097152OftenZero() (int, error) {
	b, err := r.ReadBit()
	if err != nil {
		return 0, err
	}
	if b == 1 {
		return 0, nil
	}
	return r.ReadUnsigned(21)
}
func (r *BitReader) ReadU22() (int, error) { return r.ReadUnsigned(22) }
func (r *BitReader) ReadU23() (int, error) { return r.ReadUnsigned(23) }
func (r *BitReader) ReadU24() (int, error) { return r.ReadUnsigned(24) }
func (r *BitReader) ReadU25() (int, error) { return r.ReadUnsigned(25) }
func (r *BitReader) ReadU26() (int, error) { return r.ReadUnsigned(26) }
func (r *BitReader) ReadU27() (int, error) { return r.ReadUnsigned(27) }
func (r *BitReader) ReadU28() (int, error) { return r.ReadUnsigned(28) }
func (r *BitReader) ReadU29() (int, error) { return r.ReadUnsigned(29) }
func (r *BitReader) ReadU30() (int, error) { return r.ReadUnsigned(30) }
func (r *BitReader) ReadPositiveIntMax2147483647() (int, error) {
	return r.ReadUnsigned(31)
}
func (r *BitReader) ReadIntMax1() (int, error)     { return r.ReadSigned(1) }
func (r *BitReader) ReadIntMax7() (int, error)     { return r.ReadSigned(3) }
func (r *BitReader) ReadIntMax15() (int, error)    { return r.ReadSigned(4) }
func (r *BitReader) ReadS5() (int, error)          { return r.ReadSigned(5) }
func (r *BitReader) ReadS6() (int, error)          { return r.ReadSigned(6) }
func (r *BitReader) ReadS7() (int, error)          { return r.ReadSigned(7) }
func (r *BitReader) ReadS8() (int, error)          { return r.ReadSigned(8) }
func (r *BitReader) ReadIntMax511() (int, error)   { return r.ReadSigned(9) }
func (r *BitReader) ReadIntMax1023() (int, error)  { return r.ReadSigned(10) }
func (r *BitReader) ReadS11() (int, error)         { return r.ReadSigned(11) }
func (r *BitReader) ReadS12() (int, error)         { return r.ReadSigned(12) }
func (r *BitReader) ReadS13() (int, error)         { return r.ReadSigned(13) }
func (r *BitReader) ReadS14() (int, error)         { return r.ReadSigned(14) }
func (r *BitReader) ReadIntMax32767() (int, error) { return r.ReadSigned(15) }
func (r *BitReader) ReadIntMax65535() (int, error) { return r.ReadSigned(16) }
func (r *BitReader) ReadS17() (int, error)         { return r.ReadSigned(17) }
func (r *BitReader) ReadS18() (int, error)         { return r.ReadSigned(18) }
func (r *BitReader) ReadS19() (int, error)         { return r.ReadSigned(19) }
func (r *BitReader) ReadS20() (int, error)         { return r.ReadSigned(20) }
func (r *BitReader) ReadS21() (int, error)         { return r.ReadSigned(21) }
func (r *BitReader) ReadS22() (int, error)         { return r.ReadSigned(22) }
func (r *BitReader) ReadS23() (int, error)         { return r.ReadSigned(23) }
func (r *BitReader) ReadS24() (int, error)         { return r.ReadSigned(24) }
func (r *BitReader) ReadS25() (int, error)         { return r.ReadSigned(25) }
func (r *BitReader) ReadS26() (int, error)         { return r.ReadSigned(26) }
func (r *BitReader) ReadS27() (int, error)         { return r.ReadSigned(27) }
func (r *BitReader) ReadS28() (int, error)         { return r.ReadSigned(28) }
func (r *BitReader) ReadS29() (int, error)         { return r.ReadSigned(29) }
func (r *BitReader) ReadS30() (int, error)         { return r.ReadSigned(30) }
func (r *BitReader) ReadS31() (int, error)         { return r.ReadSigned(31) }
func (r *BitReader) ReadS2() (int, error)          { return r.ReadSigned(2) }

func (r *BitReader) ReadVInt() (int, error) {
	tmp, err := r.ReadPositiveIntMax255()
	if err != nil {
		return 0, err
	}
	result := tmp & 63
	if (tmp & 64) != 0 {
		if (tmp & 128) != 0 {
			tmp, err = r.ReadPositiveIntMax255()
			if err != nil {
				return 0, err
			}
			result |= (tmp & 127) << 6
			if (tmp & 128) != 0 {
				tmp, err = r.ReadPositiveIntMax255()
				if err != nil {
					return 0, err
				}
				result |= (tmp & 127) << 13
				if (tmp & 128) != 0 {
					tmp, err = r.ReadPositiveIntMax255()
					if err != nil {
						return 0, err
					}
					result |= (tmp & 127) << 20
					if (tmp & 128) != 0 {
						tmp, err = r.ReadPositiveIntMax255()
						if err != nil {
							return 0, err
						}
						result |= (tmp & 127) << 27
						result -= 2147483647
						result -= 1
					} else {
						result -= 134217728
					}
				} else {
					result -= 1048576
				}
			} else {
				result -= 8192
			}
		} else {
			result -= 64
		}
	} else {
		if (tmp & 128) == 0 {
			return result, nil
		}
		tmp, err = r.ReadPositiveIntMax255()
		if err != nil {
			return 0, err
		}
		result |= (tmp & 127) << 6
		if (tmp & 128) == 0 {
			return result, nil
		}
		tmp, err = r.ReadPositiveIntMax255()
		if err != nil {
			return 0, err
		}
		result |= (tmp & 127) << 13
		if (tmp & 128) == 0 {
			return result, nil
		}
		tmp, err = r.ReadPositiveIntMax255()
		if err != nil {
			return 0, err
		}
		result |= (tmp & 127) << 20
		if (tmp & 128) == 0 {
			return result, nil
		}
		tmp, err = r.ReadPositiveIntMax255()
		if err != nil {
			return 0, err
		}
		result |= (tmp & 127) << 27
	}
	return result, nil
}

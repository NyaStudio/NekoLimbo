package protocol

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
)

var ErrVarIntTooBig = errors.New("VarInt is too big")

// ReadVarInt reads a VarInt from the reader.
func ReadVarInt(r io.ByteReader) (int32, error) {
	var result int32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= int32(b&0x7F) << (i * 7)
		if b&0x80 == 0 {
			return result, nil
		}
	}
	return 0, ErrVarIntTooBig
}

// VarIntSize returns the number of bytes needed to encode a VarInt.
func VarIntSize(v int32) int {
	u := uint32(v)
	size := 1
	for u >= 0x80 {
		size++
		u >>= 7
	}
	return size
}

// PacketReader reads MC protocol types from a byte slice.
type PacketReader struct {
	data []byte
	pos  int
}

func NewPacketReader(data []byte) *PacketReader {
	return &PacketReader{data: data}
}

func (r *PacketReader) ReadByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *PacketReader) Remaining() []byte {
	return r.data[r.pos:]
}

func (r *PacketReader) ReadBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *PacketReader) ReadVarInt() (int32, error) {
	return ReadVarInt(r)
}

func (r *PacketReader) ReadString() (string, error) {
	length, err := r.ReadVarInt()
	if err != nil {
		return "", err
	}
	b, err := r.ReadBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *PacketReader) ReadUUID() ([16]byte, error) {
	var uuid [16]byte
	b, err := r.ReadBytes(16)
	if err != nil {
		return uuid, err
	}
	copy(uuid[:], b)
	return uuid, nil
}

func (r *PacketReader) ReadUint16() (uint16, error) {
	b, err := r.ReadBytes(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(b), nil
}

func (r *PacketReader) ReadInt32() (int32, error) {
	b, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(b)), nil
}

func (r *PacketReader) ReadInt64() (int64, error) {
	b, err := r.ReadBytes(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(b)), nil
}

func (r *PacketReader) ReadFloat32() (float32, error) {
	b, err := r.ReadBytes(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.BigEndian.Uint32(b)), nil
}

func (r *PacketReader) ReadFloat64() (float64, error) {
	b, err := r.ReadBytes(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
}

func (r *PacketReader) ReadBool() (bool, error) {
	b, err := r.ReadByte()
	return b != 0, err
}

// PacketWriter writes MC protocol types into a byte buffer.
type PacketWriter struct {
	data []byte
}

func NewPacketWriter() *PacketWriter {
	return &PacketWriter{}
}

func (w *PacketWriter) Bytes() []byte {
	return w.data
}

func (w *PacketWriter) Len() int {
	return len(w.data)
}

func (w *PacketWriter) WriteByte(b byte) {
	w.data = append(w.data, b)
}

func (w *PacketWriter) WriteBytes(b []byte) {
	w.data = append(w.data, b...)
}

func (w *PacketWriter) WriteVarInt(v int32) {
	u := uint32(v)
	for u >= 0x80 {
		w.data = append(w.data, byte(u&0x7F)|0x80)
		u >>= 7
	}
	w.data = append(w.data, byte(u))
}

func (w *PacketWriter) WriteString(s string) {
	w.WriteVarInt(int32(len(s)))
	w.data = append(w.data, s...)
}

func (w *PacketWriter) WriteUUID(uuid [16]byte) {
	w.data = append(w.data, uuid[:]...)
}

func (w *PacketWriter) WriteBool(b bool) {
	if b {
		w.data = append(w.data, 1)
	} else {
		w.data = append(w.data, 0)
	}
}

func (w *PacketWriter) WriteInt8(v int8) {
	w.data = append(w.data, byte(v))
}

func (w *PacketWriter) WriteUint8(v uint8) {
	w.data = append(w.data, v)
}

func (w *PacketWriter) WriteInt16(v int16) {
	w.data = binary.BigEndian.AppendUint16(w.data, uint16(v))
}

func (w *PacketWriter) WriteInt32(v int32) {
	w.data = binary.BigEndian.AppendUint32(w.data, uint32(v))
}

func (w *PacketWriter) WriteUint32(v uint32) {
	w.data = binary.BigEndian.AppendUint32(w.data, v)
}

func (w *PacketWriter) WriteInt64(v int64) {
	w.data = binary.BigEndian.AppendUint64(w.data, uint64(v))
}

func (w *PacketWriter) WriteFloat32(v float32) {
	w.data = binary.BigEndian.AppendUint32(w.data, math.Float32bits(v))
}

func (w *PacketWriter) WriteFloat64(v float64) {
	w.data = binary.BigEndian.AppendUint64(w.data, math.Float64bits(v))
}

func (w *PacketWriter) WritePosition(x, y, z int) {
	val := (int64(x&0x3FFFFFF) << 38) | (int64(z&0x3FFFFFF) << 12) | int64(y&0xFFF)
	w.WriteInt64(val)
}

func (w *PacketWriter) WritePacketID(id int) {
	w.WriteVarInt(int32(id))
}

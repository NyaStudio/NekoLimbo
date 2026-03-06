package world

import (
	"encoding/binary"
	"io"
	"math"
)

const (
	TagEnd       = 0
	TagByte      = 1
	TagShort     = 2
	TagInt       = 3
	TagLong      = 4
	TagFloat     = 5
	TagDouble    = 6
	TagByteArray = 7
	TagString    = 8
	TagList      = 9
	TagCompound  = 10
	TagIntArray  = 11
	TagLongArray = 12
)

type NBTList struct {
	ElementType byte
	Values      []interface{}
}

func readNBTByte(r io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

func readNBTShort(r io.Reader) (int16, error) {
	var b [2]byte
	_, err := io.ReadFull(r, b[:])
	return int16(binary.BigEndian.Uint16(b[:])), err
}

func readNBTInt(r io.Reader) (int32, error) {
	var b [4]byte
	_, err := io.ReadFull(r, b[:])
	return int32(binary.BigEndian.Uint32(b[:])), err
}

func readNBTLong(r io.Reader) (int64, error) {
	var b [8]byte
	_, err := io.ReadFull(r, b[:])
	return int64(binary.BigEndian.Uint64(b[:])), err
}

func readNBTFloat(r io.Reader) (float32, error) {
	var b [4]byte
	_, err := io.ReadFull(r, b[:])
	return math.Float32frombits(binary.BigEndian.Uint32(b[:])), err
}

func readNBTDouble(r io.Reader) (float64, error) {
	var b [8]byte
	_, err := io.ReadFull(r, b[:])
	return math.Float64frombits(binary.BigEndian.Uint64(b[:])), err
}

func readNBTString(r io.Reader) (string, error) {
	length, err := readNBTShort(r)
	if err != nil {
		return "", err
	}
	if length < 0 {
		return "", io.ErrUnexpectedEOF
	}
	buf := make([]byte, length)
	_, err = io.ReadFull(r, buf)
	return string(buf), err
}

func readNBTPayload(r io.Reader, tagType byte) (interface{}, error) {
	switch tagType {
	case TagByte:
		return readNBTByte(r)
	case TagShort:
		return readNBTShort(r)
	case TagInt:
		return readNBTInt(r)
	case TagLong:
		return readNBTLong(r)
	case TagFloat:
		return readNBTFloat(r)
	case TagDouble:
		return readNBTDouble(r)
	case TagByteArray:
		length, err := readNBTInt(r)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length)
		_, err = io.ReadFull(r, buf)
		return buf, err
	case TagString:
		return readNBTString(r)
	case TagList:
		return readNBTList(r)
	case TagCompound:
		return readNBTCompound(r)
	case TagIntArray:
		length, err := readNBTInt(r)
		if err != nil {
			return nil, err
		}
		arr := make([]int32, length)
		for i := range arr {
			arr[i], err = readNBTInt(r)
			if err != nil {
				return nil, err
			}
		}
		return arr, nil
	case TagLongArray:
		length, err := readNBTInt(r)
		if err != nil {
			return nil, err
		}
		arr := make([]int64, length)
		for i := range arr {
			arr[i], err = readNBTLong(r)
			if err != nil {
				return nil, err
			}
		}
		return arr, nil
	}
	return nil, io.ErrUnexpectedEOF
}

func readNBTList(r io.Reader) (NBTList, error) {
	elemType, err := readNBTByte(r)
	if err != nil {
		return NBTList{}, err
	}
	length, err := readNBTInt(r)
	if err != nil {
		return NBTList{}, err
	}
	values := make([]interface{}, length)
	for i := range values {
		values[i], err = readNBTPayload(r, elemType)
		if err != nil {
			return NBTList{}, err
		}
	}
	return NBTList{ElementType: elemType, Values: values}, nil
}

func readNBTCompound(r io.Reader) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for {
		tagType, err := readNBTByte(r)
		if err != nil {
			return nil, err
		}
		if tagType == TagEnd {
			break
		}
		name, err := readNBTString(r)
		if err != nil {
			return nil, err
		}
		value, err := readNBTPayload(r, tagType)
		if err != nil {
			return nil, err
		}
		result[name] = value
	}
	return result, nil
}

// ReadNBT reads a named root compound tag.
func ReadNBT(r io.Reader) (map[string]interface{}, error) {
	tagType, err := readNBTByte(r)
	if err != nil {
		return nil, err
	}
	if tagType != TagCompound {
		return nil, io.ErrUnexpectedEOF
	}
	// Read and discard root name
	_, err = readNBTString(r)
	if err != nil {
		return nil, err
	}
	return readNBTCompound(r)
}

// WriteAnonymousNBT writes a compound as anonymous NBT (type byte + content, no name).
func WriteAnonymousNBT(w io.Writer, compound map[string]interface{}) error {
	if _, err := w.Write([]byte{TagCompound}); err != nil {
		return err
	}
	return writeNBTCompound(w, compound)
}

func writeNBTCompound(w io.Writer, compound map[string]interface{}) error {
	for name, value := range compound {
		tagType := nbtTagType(value)
		if _, err := w.Write([]byte{tagType}); err != nil {
			return err
		}
		if err := writeNBTString(w, name); err != nil {
			return err
		}
		if err := writeNBTPayload(w, tagType, value); err != nil {
			return err
		}
	}
	_, err := w.Write([]byte{TagEnd})
	return err
}

func writeNBTString(w io.Writer, s string) error {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(len(s)))
	if _, err := w.Write(b); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func writeNBTPayload(w io.Writer, tagType byte, value interface{}) error {
	switch tagType {
	case TagLongArray:
		arr := value.([]int64)
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(len(arr)))
		if _, err := w.Write(b); err != nil {
			return err
		}
		for _, v := range arr {
			b := make([]byte, 8)
			binary.BigEndian.PutUint64(b, uint64(v))
			if _, err := w.Write(b); err != nil {
				return err
			}
		}
	case TagCompound:
		return writeNBTCompound(w, value.(map[string]interface{}))
	}
	return nil
}

func nbtTagType(v interface{}) byte {
	switch v.(type) {
	case byte:
		return TagByte
	case int16:
		return TagShort
	case int32:
		return TagInt
	case int64:
		return TagLong
	case float32:
		return TagFloat
	case float64:
		return TagDouble
	case string:
		return TagString
	case []byte:
		return TagByteArray
	case []int32:
		return TagIntArray
	case []int64:
		return TagLongArray
	case NBTList:
		return TagList
	case map[string]interface{}:
		return TagCompound
	}
	return TagEnd
}

package protocol

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"io"
	"net"
	"sync"
)

// Conn wraps a net.Conn with MC protocol packet framing and compression.
type Conn struct {
	r          *bufio.Reader
	w          *bufio.Writer
	raw        net.Conn
	mu         sync.Mutex
	Compressed bool
	Threshold  int
}

func NewConn(c net.Conn) *Conn {
	return &Conn{
		r:   bufio.NewReaderSize(c, 4096),
		w:   bufio.NewWriterSize(c, 65536),
		raw: c,
	}
}

func (c *Conn) Close() error {
	return c.raw.Close()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.raw.RemoteAddr()
}

func (c *Conn) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.w.Flush()
}

// ReadPacket reads one framed packet from the connection.
func (c *Conn) ReadPacket() (int, *PacketReader, error) {
	length, err := ReadVarInt(c.r)
	if err != nil {
		return 0, nil, err
	}
	if length <= 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}

	raw := make([]byte, length)
	if _, err := io.ReadFull(c.r, raw); err != nil {
		return 0, nil, err
	}

	var packetData []byte
	if c.Compressed {
		pr := NewPacketReader(raw)
		dataLength, err := pr.ReadVarInt()
		if err != nil {
			return 0, nil, err
		}
		remaining := pr.Remaining()

		if dataLength > 0 {
			zr, err := zlib.NewReader(bytes.NewReader(remaining))
			if err != nil {
				return 0, nil, err
			}
			packetData = make([]byte, dataLength)
			_, err = io.ReadFull(zr, packetData)
			zr.Close()
			if err != nil {
				return 0, nil, err
			}
		} else {
			packetData = remaining
		}
	} else {
		packetData = raw
	}

	pr := NewPacketReader(packetData)
	packetID, err := pr.ReadVarInt()
	if err != nil {
		return 0, nil, err
	}

	return int(packetID), NewPacketReader(pr.Remaining()), nil
}

// WritePacket writes a framed packet to the connection.
func (c *Conn) WritePacket(id int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Build raw packet: [VarInt packetID][data]
	pw := NewPacketWriter()
	pw.WriteVarInt(int32(id))
	pw.WriteBytes(data)
	packet := pw.Bytes()

	if c.Compressed {
		if len(packet) >= c.Threshold {
			var compressed bytes.Buffer
			zw := zlib.NewWriter(&compressed)
			zw.Write(packet)
			zw.Close()

			inner := NewPacketWriter()
			inner.WriteVarInt(int32(len(packet)))
			inner.WriteBytes(compressed.Bytes())

			frame := NewPacketWriter()
			frame.WriteVarInt(int32(inner.Len()))
			frame.WriteBytes(inner.Bytes())
			_, err := c.w.Write(frame.Bytes())
			return err
		}

		inner := NewPacketWriter()
		inner.WriteVarInt(0) // not compressed
		inner.WriteBytes(packet)

		frame := NewPacketWriter()
		frame.WriteVarInt(int32(inner.Len()))
		frame.WriteBytes(inner.Bytes())
		_, err := c.w.Write(frame.Bytes())
		return err
	}

	frame := NewPacketWriter()
	frame.WriteVarInt(int32(len(packet)))
	frame.WriteBytes(packet)
	_, err := c.w.Write(frame.Bytes())
	return err
}

// SendPacket builds and writes a packet using a callback.
func (c *Conn) SendPacket(id int, fn func(w *PacketWriter)) error {
	w := NewPacketWriter()
	fn(w)
	return c.WritePacket(id, w.Bytes())
}

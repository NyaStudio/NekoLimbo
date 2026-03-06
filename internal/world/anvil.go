package world

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// LoadRegionChunks reads all chunks from an Anvil region file (.mca).
// Returns parsed chunk NBT data keyed by global (chunkX, chunkZ).
func LoadRegionChunks(path string, regionX, regionZ int) (map[[2]int]map[string]interface{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read location table (first 4096 bytes, 1024 entries of 4 bytes)
	var locations [1024]uint32
	if err := binary.Read(f, binary.BigEndian, &locations); err != nil {
		return nil, fmt.Errorf("reading locations: %w", err)
	}

	chunks := make(map[[2]int]map[string]interface{})

	for i, loc := range locations {
		if loc == 0 {
			continue
		}

		offset := int64(loc>>8) * 4096
		// sectorCount := loc & 0xFF

		localX := i % 32
		localZ := i / 32
		globalX := regionX*32 + localX
		globalZ := regionZ*32 + localZ

		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			continue
		}

		var chunkLength int32
		if err := binary.Read(f, binary.BigEndian, &chunkLength); err != nil {
			continue
		}
		if chunkLength <= 1 {
			continue
		}

		var compressionType byte
		if err := binary.Read(f, binary.BigEndian, &compressionType); err != nil {
			continue
		}

		compressedData := make([]byte, chunkLength-1)
		if _, err := io.ReadFull(f, compressedData); err != nil {
			continue
		}

		var reader io.ReadCloser
		switch compressionType {
		case 1: // GZip
			reader, err = gzip.NewReader(bytes.NewReader(compressedData))
		case 2: // Zlib
			reader, err = zlib.NewReader(bytes.NewReader(compressedData))
		case 3: // Uncompressed
			reader = io.NopCloser(bytes.NewReader(compressedData))
		default:
			continue
		}
		if err != nil {
			continue
		}

		nbt, err := ReadNBT(reader)
		reader.Close()
		if err != nil {
			continue
		}

		chunks[[2]int{globalX, globalZ}] = nbt
	}

	return chunks, nil
}

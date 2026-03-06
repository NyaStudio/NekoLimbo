package world

import (
	"bytes"

	"nekolimbo/internal/protocol"
)

const (
	directBlockBits        = 15
	directBiomeBits        = 7
	maxIndirectBlockBits   = 8
	maxIndirectBiomeBits   = 3
	minIndirectBlockBits   = 4
	minIndirectBiomeBits   = 1
	blockEntriesPerSection = 4096 // 16*16*16
	biomeEntriesPerSection = 64   // 4*4*4
)

// Chunk holds pre-serialized packet data for a single chunk column.
type Chunk struct {
	X, Z       int
	PacketData []byte
}

// GetBlockStateID returns the global block state ID for a given block name and properties.
func GetBlockStateID(name string, properties map[string]string) int32 {
	def, ok := blockRegistry[name]
	if !ok {
		return 0 // air
	}
	if len(def.Properties) == 0 {
		return int32(def.MinStateID)
	}
	offset := 0
	for i, prop := range def.Properties {
		stride := 1
		for j := i + 1; j < len(def.Properties); j++ {
			stride *= len(def.Properties[j].Values)
		}
		valueStr := properties[prop.Name]
		valueIdx := 0
		for k, v := range prop.Values {
			if v == valueStr {
				valueIdx = k
				break
			}
		}
		offset += valueIdx * stride
	}
	return int32(def.MinStateID + offset)
}

// GetBiomeID returns the global biome registry ID.
func GetBiomeID(name string) int32 {
	if id, ok := biomeRegistry[name]; ok {
		return int32(id)
	}
	return 0
}

// BuildChunkPacketData constructs the complete data for a map_chunk packet.
func BuildChunkPacketData(chunkX, chunkZ int, nbt map[string]interface{}, numSections int, defaultBiome int32) []byte {
	w := protocol.NewPacketWriter()
	w.WriteInt32(int32(chunkX))
	w.WriteInt32(int32(chunkZ))

	// Heightmaps
	heightmapNBT := extractHeightmaps(nbt)
	var hbuf bytes.Buffer
	WriteAnonymousNBT(&hbuf, heightmapNBT)
	w.WriteBytes(hbuf.Bytes())

	// Build section data
	sectionData := buildSectionData(nbt, numSections, defaultBiome)
	w.WriteVarInt(int32(len(sectionData)))
	w.WriteBytes(sectionData)

	// Block entities (empty)
	w.WriteVarInt(0)

	// Light masks (all empty)
	w.WriteVarInt(0) // sky light mask
	w.WriteVarInt(0) // block light mask
	w.WriteVarInt(0) // empty sky light mask
	w.WriteVarInt(0) // empty block light mask
	w.WriteVarInt(0) // sky light arrays
	w.WriteVarInt(0) // block light arrays

	return w.Bytes()
}

func extractHeightmaps(nbt map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	hm, ok := nbt["Heightmaps"]
	if !ok {
		return result
	}
	hmMap, ok := hm.(map[string]interface{})
	if !ok {
		return result
	}
	if mb, ok := hmMap["MOTION_BLOCKING"]; ok {
		result["MOTION_BLOCKING"] = mb
	}
	if ws, ok := hmMap["WORLD_SURFACE"]; ok {
		result["WORLD_SURFACE"] = ws
	}
	return result
}

func buildSectionData(nbt map[string]interface{}, numSections int, defaultBiome int32) []byte {
	// Parse sections from NBT
	sectionMap := make(map[int]map[string]interface{})
	if sectionsNBT, ok := nbt["sections"]; ok {
		list := sectionsNBT.(NBTList)
		for _, v := range list.Values {
			section := v.(map[string]interface{})
			y := nbtByte(section, "Y")
			sectionMap[int(int8(y))] = section
		}
	}

	w := protocol.NewPacketWriter()
	for i := 0; i < numSections; i++ {
		section, ok := sectionMap[i]
		if !ok {
			writeEmptySection(w, defaultBiome)
			continue
		}
		writeSection(w, section, defaultBiome)
	}
	return w.Bytes()
}

func writeEmptySection(w *protocol.PacketWriter, defaultBiome int32) {
	w.WriteInt16(0) // block count = 0
	// Block states: single value = air (0)
	w.WriteUint8(0)
	w.WriteVarInt(0)
	w.WriteVarInt(0)
	// Biomes: single value
	w.WriteUint8(0)
	w.WriteVarInt(defaultBiome)
	w.WriteVarInt(0)
}

func writeSection(w *protocol.PacketWriter, section map[string]interface{}, defaultBiome int32) {
	// Block states
	var blockPalette []int32
	var blockData []int64
	var blockFileBits int

	if bs, ok := section["block_states"]; ok {
		bsMap := bs.(map[string]interface{})
		blockPalette, blockFileBits, blockData = parseBlockStates(bsMap)
	}

	blockCount := computeBlockCount(blockPalette, blockData, blockFileBits)
	w.WriteInt16(blockCount)
	writePalettedContainer(w, blockPalette, blockData, blockFileBits, blockEntriesPerSection, true)

	// Biomes
	var biomePalette []int32
	var biomeData []int64
	var biomeFileBits int

	if b, ok := section["biomes"]; ok {
		bMap := b.(map[string]interface{})
		biomePalette, biomeFileBits, biomeData = parseBiomes(bMap)
	}

	if len(biomePalette) == 0 {
		biomePalette = []int32{defaultBiome}
	}
	writePalettedContainer(w, biomePalette, biomeData, biomeFileBits, biomeEntriesPerSection, false)
}

func parseBlockStates(bsMap map[string]interface{}) (palette []int32, bits int, data []int64) {
	if paletteNBT, ok := bsMap["palette"]; ok {
		list := paletteNBT.(NBTList)
		palette = make([]int32, len(list.Values))
		for i, entry := range list.Values {
			compound := entry.(map[string]interface{})
			name := compound["Name"].(string)
			var props map[string]string
			if p, ok := compound["Properties"]; ok {
				pMap := p.(map[string]interface{})
				props = make(map[string]string, len(pMap))
				for k, v := range pMap {
					props[k] = v.(string)
				}
			}
			palette[i] = GetBlockStateID(name, props)
		}
	}
	if dataNBT, ok := bsMap["data"]; ok {
		data = dataNBT.([]int64)
	}
	if len(palette) > 1 && len(data) > 0 {
		bits = inferBitsPerEntry(len(data), blockEntriesPerSection)
	}
	return
}

func parseBiomes(bMap map[string]interface{}) (palette []int32, bits int, data []int64) {
	if paletteNBT, ok := bMap["palette"]; ok {
		list := paletteNBT.(NBTList)
		palette = make([]int32, len(list.Values))
		for i, entry := range list.Values {
			palette[i] = GetBiomeID(entry.(string))
		}
	}
	if dataNBT, ok := bMap["data"]; ok {
		data = dataNBT.([]int64)
	}
	if len(palette) > 1 && len(data) > 0 {
		bits = inferBitsPerEntry(len(data), biomeEntriesPerSection)
	}
	return
}

func computeBlockCount(palette []int32, data []int64, bits int) int16 {
	if len(palette) == 0 {
		return 0
	}
	if len(palette) == 1 {
		if palette[0] == 0 {
			return 0
		}
		return 4096
	}
	// Find air index in palette
	airIdx := -1
	for i, id := range palette {
		if id == 0 {
			airIdx = i
			break
		}
	}
	if airIdx == -1 {
		return 4096 // no air
	}
	count := int16(0)
	entries := unpackEntries(data, bits, blockEntriesPerSection)
	for _, idx := range entries {
		if idx != airIdx {
			count++
		}
	}
	return count
}

func writePalettedContainer(w *protocol.PacketWriter, palette []int32, data []int64, fileBits int, totalEntries int, isBlocks bool) {
	minBits, maxBits, dirBits := minIndirectBiomeBits, maxIndirectBiomeBits, directBiomeBits
	if isBlocks {
		minBits, maxBits, dirBits = minIndirectBlockBits, maxIndirectBlockBits, directBlockBits
	}

	if len(palette) <= 1 {
		w.WriteUint8(0)
		id := int32(0)
		if len(palette) == 1 {
			id = palette[0]
		}
		w.WriteVarInt(id)
		w.WriteVarInt(0)
		return
	}

	bits := minBits
	for (1 << bits) < len(palette) {
		bits++
	}

	if bits > maxBits {
		// Direct mode
		w.WriteUint8(uint8(dirBits))
		entries := unpackEntries(data, fileBits, totalEntries)
		for i, idx := range entries {
			if idx < len(palette) {
				entries[i] = int(palette[idx])
			}
		}
		packed := packEntries(entries, dirBits)
		w.WriteVarInt(int32(len(packed)))
		for _, l := range packed {
			w.WriteInt64(l)
		}
		return
	}

	// Indirect mode
	w.WriteUint8(uint8(bits))
	w.WriteVarInt(int32(len(palette)))
	for _, id := range palette {
		w.WriteVarInt(id)
	}

	if fileBits == bits {
		w.WriteVarInt(int32(len(data)))
		for _, l := range data {
			w.WriteInt64(l)
		}
	} else {
		entries := unpackEntries(data, fileBits, totalEntries)
		packed := packEntries(entries, bits)
		w.WriteVarInt(int32(len(packed)))
		for _, l := range packed {
			w.WriteInt64(l)
		}
	}
}

func unpackEntries(data []int64, bitsPerEntry int, count int) []int {
	if bitsPerEntry <= 0 || len(data) == 0 {
		return make([]int, count)
	}
	entries := make([]int, count)
	entriesPerLong := 64 / bitsPerEntry
	mask := int64((1 << bitsPerEntry) - 1)
	idx := 0
	for _, long := range data {
		for j := 0; j < entriesPerLong && idx < count; j++ {
			entries[idx] = int(long & mask)
			long >>= bitsPerEntry
			idx++
		}
	}
	return entries
}

func packEntries(entries []int, bitsPerEntry int) []int64 {
	entriesPerLong := 64 / bitsPerEntry
	numLongs := (len(entries) + entriesPerLong - 1) / entriesPerLong
	data := make([]int64, numLongs)
	idx := 0
	for i := range data {
		var long int64
		for j := 0; j < entriesPerLong && idx < len(entries); j++ {
			long |= int64(entries[idx]&((1<<bitsPerEntry)-1)) << (j * bitsPerEntry)
			idx++
		}
		data[i] = long
	}
	return data
}

func ceilLog2(n int) int {
	if n <= 1 {
		return 0
	}
	bits := 0
	n--
	for n > 0 {
		bits++
		n >>= 1
	}
	return bits
}

// inferBitsPerEntry determines the bits-per-entry used in a packed long array
// by matching the data array length against expected lengths for each bit width.
func inferBitsPerEntry(dataLen, totalEntries int) int {
	for bits := 1; bits <= 15; bits++ {
		entriesPerLong := 64 / bits
		numLongs := (totalEntries + entriesPerLong - 1) / entriesPerLong
		if numLongs == dataLen {
			return bits
		}
	}
	return 4 // fallback
}

func nbtByte(m map[string]interface{}, key string) byte {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case byte:
			return val
		case int8:
			return byte(val)
		}
	}
	return 0
}

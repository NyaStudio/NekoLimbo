package world

import (
	"bytes"
	"encoding/json"
	"strings"

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
	X, Z          int
	PacketData    []byte
	BlockEntities []BlockEntityUpdate
}

type BlockEntityUpdate struct {
	X, Y, Z int
	TypeID  int32
	NBT     []byte
}

// GetBlockStateID returns the global block state ID for a given block name and properties.
func GetBlockStateID(name string, properties map[string]string) int32 {
	def, ok := blockRegistry[name]
	if !ok {
		return 0 // air
	}
	if len(def.Properties) == 0 {
		return int32(def.DefaultState)
	}
	defaultOffset := def.DefaultState - def.MinStateID
	offset := 0
	for i, prop := range def.Properties {
		stride := 1
		for j := i + 1; j < len(def.Properties); j++ {
			stride *= len(def.Properties[j].Values)
		}

		valueIdx := (defaultOffset / stride) % len(prop.Values)
		if properties != nil {
			if valueStr, ok := properties[prop.Name]; ok {
				for k, v := range prop.Values {
					if v == valueStr {
						valueIdx = k
						break
					}
				}
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
func BuildChunkPacketData(chunkX, chunkZ int, nbt map[string]interface{}, numSections int, defaultBiome int32, hasSkyLight bool) []byte {
	w := protocol.NewPacketWriter()
	w.WriteInt32(int32(chunkX))
	w.WriteInt32(int32(chunkZ))

	// Heightmaps
	heightmapNBT := extractHeightmaps(nbt)
	var hbuf bytes.Buffer
	WriteAnonymousNBT(&hbuf, heightmapNBT)
	w.WriteBytes(hbuf.Bytes())

	sectionMap := buildSectionMap(nbt)

	// Build section data
	sectionData := buildSectionData(sectionMap, numSections, defaultBiome)
	w.WriteVarInt(int32(len(sectionData)))
	w.WriteBytes(sectionData)

	writeBlockEntities(w, nbt, chunkX, chunkZ)

	light := buildLightData(sectionMap, numSections, hasSkyLight)
	writeBitSet(w, light.skyLightMask)
	writeBitSet(w, light.blockLightMask)
	writeBitSet(w, light.emptySkyLightMask)
	writeBitSet(w, light.emptyBlockLightMask)
	writeLightArrays(w, light.skyLightArrays)
	writeLightArrays(w, light.blockLightArrays)

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

func buildSectionMap(nbt map[string]interface{}) map[int]map[string]interface{} {
	sectionMap := make(map[int]map[string]interface{})
	if sectionsNBT, ok := nbt["sections"]; ok {
		if list, ok := sectionsNBT.(NBTList); ok {
			for _, v := range list.Values {
				section, ok := v.(map[string]interface{})
				if !ok {
					continue
				}
				y := nbtByte(section, "Y")
				sectionMap[int(int8(y))] = section
			}
		}
	}
	return sectionMap
}

func buildSectionData(sectionMap map[int]map[string]interface{}, numSections int, defaultBiome int32) []byte {
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
	if blockCount == 0 && len(blockPalette) > 1 {
		for _, id := range blockPalette {
			if id != 0 {
				blockCount = 1
				break
			}
		}
	}
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

type chunkLightData struct {
	skyLightMask        []uint64
	blockLightMask      []uint64
	emptySkyLightMask   []uint64
	emptyBlockLightMask []uint64
	skyLightArrays      [][]byte
	blockLightArrays    [][]byte
}

type chunkBlockEntity struct {
	x        int
	packedXZ byte
	y        int16
	z        int
	typeID   int32
	nbt      []byte
}

func buildLightData(sectionMap map[int]map[string]interface{}, numSections int, hasSkyLight bool) chunkLightData {
	bitCount := numSections + 2
	wordCount := (bitCount + 63) / 64
	light := chunkLightData{
		skyLightMask:        make([]uint64, wordCount),
		blockLightMask:      make([]uint64, wordCount),
		emptySkyLightMask:   make([]uint64, wordCount),
		emptyBlockLightMask: make([]uint64, wordCount),
	}

	for i := 0; i < numSections; i++ {
		bit := i + 1 // section masks include one slot below and above the world
		section, ok := sectionMap[i]

		if hasSkyLight {
			if ok {
				if sky := nbtByteArray(section, "SkyLight", "sky_light"); len(sky) == 2048 {
					setBit(light.skyLightMask, bit)
					light.skyLightArrays = append(light.skyLightArrays, sky)
				} else {
					setBit(light.emptySkyLightMask, bit)
				}
			} else {
				setBit(light.emptySkyLightMask, bit)
			}
		}

		if ok {
			if block := nbtByteArray(section, "BlockLight", "block_light"); len(block) == 2048 {
				setBit(light.blockLightMask, bit)
				light.blockLightArrays = append(light.blockLightArrays, block)
			} else {
				setBit(light.emptyBlockLightMask, bit)
			}
		} else {
			setBit(light.emptyBlockLightMask, bit)
		}
	}

	if hasSkyLight {
		setBit(light.emptySkyLightMask, 0)
		setBit(light.emptySkyLightMask, numSections+1)
	}
	setBit(light.emptyBlockLightMask, 0)
	setBit(light.emptyBlockLightMask, numSections+1)

	return light
}

func writeBitSet(w *protocol.PacketWriter, bits []uint64) {
	end := len(bits)
	for end > 0 && bits[end-1] == 0 {
		end--
	}
	w.WriteVarInt(int32(end))
	for i := 0; i < end; i++ {
		w.WriteInt64(int64(bits[i]))
	}
}

func writeLightArrays(w *protocol.PacketWriter, arrays [][]byte) {
	w.WriteVarInt(int32(len(arrays)))
	for _, arr := range arrays {
		w.WriteVarInt(int32(len(arr)))
		w.WriteBytes(arr)
	}
}

func setBit(bits []uint64, idx int) {
	if idx < 0 {
		return
	}
	word := idx / 64
	if word >= len(bits) {
		return
	}
	bits[word] |= uint64(1) << (idx % 64)
}

func writeBlockEntities(w *protocol.PacketWriter, nbt map[string]interface{}, chunkX, chunkZ int) {
	entities := collectBlockEntities(nbt, chunkX, chunkZ)
	w.WriteVarInt(int32(len(entities)))
	for _, entity := range entities {
		w.WriteUint8(entity.packedXZ)
		w.WriteInt16(entity.y)
		w.WriteVarInt(entity.typeID)
		if len(entity.nbt) == 0 {
			w.WriteUint8(TagEnd)
			continue
		}
		w.WriteBytes(entity.nbt)
	}
}

func collectBlockEntities(nbt map[string]interface{}, chunkX, chunkZ int) []chunkBlockEntity {
	rawEntities, ok := nbt["block_entities"]
	if !ok {
		return nil
	}
	list, ok := rawEntities.(NBTList)
	if !ok || len(list.Values) == 0 {
		return nil
	}

	entities := make([]chunkBlockEntity, 0, len(list.Values))
	for _, raw := range list.Values {
		entity, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		id, ok := entity["id"].(string)
		if !ok {
			continue
		}
		typeID, ok := GetBlockEntityTypeID(id)
		if !ok {
			continue
		}

		x, okX := nbtInt(entity, "x")
		y, okY := nbtInt(entity, "y")
		z, okZ := nbtInt(entity, "z")
		if !okX || !okY || !okZ {
			continue
		}

		localX := x - (chunkX << 4)
		localZ := z - (chunkZ << 4)
		if localX < 0 || localX > 15 || localZ < 0 || localZ > 15 {
			localX = x & 15
			localZ = z & 15
		}

		entities = append(entities, chunkBlockEntity{
			x:        x,
			packedXZ: byte(((localX & 0x0F) << 4) | (localZ & 0x0F)),
			y:        int16(y),
			z:        z,
			typeID:   typeID,
			nbt:      encodeBlockEntityNBT(entity),
		})
	}

	return entities
}

func buildBlockEntityUpdates(entities []chunkBlockEntity) []BlockEntityUpdate {
	if len(entities) == 0 {
		return nil
	}
	updates := make([]BlockEntityUpdate, 0, len(entities))
	for _, entity := range entities {
		updates = append(updates, BlockEntityUpdate{
			X:      entity.x,
			Y:      int(entity.y),
			Z:      entity.z,
			TypeID: entity.typeID,
			NBT:    entity.nbt,
		})
	}
	return updates
}

func encodeBlockEntityNBT(entity map[string]interface{}) []byte {
	id, _ := entity["id"].(string)
	data := make(map[string]interface{}, len(entity))
	for k, v := range entity {
		switch k {
		case "id", "x", "y", "z", "keepPacked":
			continue
		default:
			data[k] = v
		}
	}
	normalizeBlockEntityData(id, data)
	return writeAnonymousNBTBytes(data)
}

func writeAnonymousNBTBytes(data map[string]interface{}) []byte {
	var buf bytes.Buffer
	if err := WriteAnonymousNBT(&buf, data); err != nil {
		return []byte{TagEnd}
	}
	return buf.Bytes()
}

func normalizeBlockEntityData(id string, data map[string]interface{}) {
	switch id {
	case "minecraft:sign", "minecraft:hanging_sign", "sign", "hanging_sign":
		normalizeSignBlockEntityData(data)
	}
}

func normalizeSignBlockEntityData(data map[string]interface{}) {
	data["is_waxed"] = normalizeNBTByte(data["is_waxed"])

	// Handle pre-1.20 sign format: Text1-Text4 → front_text/back_text
	if _, hasFront := data["front_text"]; !hasFront {
		if _, hasText1 := data["Text1"]; hasText1 {
			data["front_text"] = map[string]interface{}{
				"color":            normalizeSignTextColor(data["Color"]),
				"has_glowing_text": normalizeNBTByte(data["GlowingText"]),
				"messages": NBTList{ElementType: TagString, Values: []interface{}{
					oldSignField(data["Text1"]),
					oldSignField(data["Text2"]),
					oldSignField(data["Text3"]),
					oldSignField(data["Text4"]),
				}},
			}
			data["back_text"] = emptySignSide()
			delete(data, "Text1")
			delete(data, "Text2")
			delete(data, "Text3")
			delete(data, "Text4")
			delete(data, "Color")
			delete(data, "GlowingText")
		}
	}

	data["front_text"] = normalizeSignTextSide(data["front_text"])
	data["back_text"] = normalizeSignTextSide(data["back_text"])
}

func oldSignField(raw interface{}) string {
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		if s != "" && s != `""` && json.Valid([]byte(s)) {
			return s
		}
	}
	return `{"text":""}`
}

func emptySignSide() map[string]interface{} {
	return map[string]interface{}{
		"color":            "black",
		"has_glowing_text": byte(0),
		"messages":         newEmptySignTextList(),
	}
}

func normalizeSignTextSide(raw interface{}) map[string]interface{} {
	side, ok := raw.(map[string]interface{})
	if !ok {
		side = make(map[string]interface{})
	}

	messages := normalizeSignTextMessages(side["messages"])
	filtered := normalizeSignTextMessages(side["filtered_messages"])
	if isEmptySignTextList(filtered) {
		filtered = cloneSignTextList(messages)
	}

	side["color"] = normalizeSignTextColor(side["color"])
	side["has_glowing_text"] = normalizeNBTByte(side["has_glowing_text"])
	side["messages"] = messages
	side["filtered_messages"] = filtered
	return side
}

// normalizeSignTextMessages ensures exactly 4 JSON text component strings.
// Block entity NBT sends sign messages as TAG_List(TAG_String) where each
// string is a JSON-encoded text component (e.g. '{"text":"Hello"}').
func normalizeSignTextMessages(raw interface{}) NBTList {
	list, ok := raw.(NBTList)
	if !ok {
		return newEmptySignTextList()
	}

	values := make([]interface{}, 4)
	for i := range values {
		values[i] = `{"text":""}`
		if i < len(list.Values) {
			values[i] = normalizeSignMessage(list.Values[i])
		}
	}
	return NBTList{ElementType: TagString, Values: values}
}

func newEmptySignTextList() NBTList {
	return NBTList{ElementType: TagString, Values: []interface{}{
		`{"text":""}`, `{"text":""}`, `{"text":""}`, `{"text":""}`,
	}}
}

// normalizeSignMessage converts a single sign message (from either old-format
// TAG_String with JSON or new-format TAG_Compound) into a JSON string.
func normalizeSignMessage(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == `""` {
			return `{"text":""}`
		}
		if json.Valid([]byte(trimmed)) {
			return trimmed
		}
		// Plain text (1.20.3+ simple string format), wrap in JSON
		b, _ := json.Marshal(map[string]string{"text": trimmed})
		return string(b)
	case map[string]interface{}:
		return textComponentToJSON(v)
	default:
		return `{"text":""}`
	}
}

// textComponentToJSON serializes an NBT text component compound to JSON.
func textComponentToJSON(component map[string]interface{}) string {
	b, err := json.Marshal(toJSONValue(component))
	if err != nil {
		return `{"text":""}`
	}
	return string(b)
}

// toJSONValue converts NBT-typed values to JSON-compatible Go values.
func toJSONValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(val))
		for k, v := range val {
			m[k] = toJSONValue(v)
		}
		return m
	case NBTList:
		arr := make([]interface{}, len(val.Values))
		for i, elem := range val.Values {
			arr[i] = toJSONValue(elem)
		}
		return arr
	case []interface{}:
		arr := make([]interface{}, len(val))
		for i, elem := range val {
			arr[i] = toJSONValue(elem)
		}
		return arr
	case byte:
		if val == 0 {
			return false
		}
		if val == 1 {
			return true
		}
		return int(val)
	case int8:
		return int(val)
	case int16:
		return int(val)
	case int32:
		return int(val)
	default:
		return val
	}
}

func isEmptySignTextList(list NBTList) bool {
	if len(list.Values) == 0 {
		return true
	}
	for _, raw := range list.Values {
		s, ok := raw.(string)
		if !ok {
			return false
		}
		trimmed := strings.TrimSpace(s)
		if trimmed != "" && trimmed != `""` && trimmed != `{"text":""}` {
			return false
		}
	}
	return true
}

func cloneSignTextList(list NBTList) NBTList {
	values := make([]interface{}, len(list.Values))
	copy(values, list.Values)
	return NBTList{ElementType: list.ElementType, Values: values}
}

func normalizeSignTextColor(raw interface{}) string {
	if color, ok := raw.(string); ok && color != "" {
		return color
	}
	return "black"
}

func normalizeNBTByte(raw interface{}) byte {
	switch value := raw.(type) {
	case byte:
		return value
	case int8:
		return byte(value)
	case int:
		return byte(value)
	case int16:
		return byte(value)
	case int32:
		return byte(value)
	case bool:
		if value {
			return 1
		}
	}
	return 0
}

func nbtByteArray(m map[string]interface{}, keys ...string) []byte {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if arr, ok := v.([]byte); ok {
				return arr
			}
		}
	}
	return nil
}

func nbtInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return val, true
	case int8:
		return int(val), true
	case int16:
		return int(val), true
	case int32:
		return int(val), true
	case int64:
		return int(val), true
	case byte:
		return int(val), true
	case uint16:
		return int(val), true
	case uint32:
		return int(val), true
	case uint64:
		return int(val), true
	}
	return 0, false
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
	mask := uint64((1 << bitsPerEntry) - 1)
	idx := 0
	for _, long := range data {
		value := uint64(long)
		for j := 0; j < entriesPerLong && idx < count; j++ {
			entries[idx] = int(value & mask)
			value >>= bitsPerEntry
			idx++
		}
	}
	return entries
}

func packEntries(entries []int, bitsPerEntry int) []int64 {
	entriesPerLong := 64 / bitsPerEntry
	numLongs := (len(entries) + entriesPerLong - 1) / entriesPerLong
	data := make([]int64, numLongs)
	mask := uint64((1 << bitsPerEntry) - 1)
	idx := 0
	for i := range data {
		var value uint64
		for j := 0; j < entriesPerLong && idx < len(entries); j++ {
			value |= (uint64(entries[idx]) & mask) << (j * bitsPerEntry)
			idx++
		}
		data[i] = int64(value)
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

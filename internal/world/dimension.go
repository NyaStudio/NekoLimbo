package world

//go:generate go run ../../tools/gen_registry/main.go

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

type DimensionInfo struct {
	Name         string // e.g., "minecraft:overworld"
	TypeID       int    // registry ID for minecraft:dimension_type
	DirPath      string // path to dimension directory (contains region/)
	MinY         int
	Height       int
	Sections     int
	HasSkyLight  bool
	DefaultBiome int32
}

type World struct {
	Dimensions []*DimensionInfo
	Active     *DimensionInfo
	Chunks     map[[2]int]*Chunk
}

var dimDefs = []struct {
	subDir       string
	name         string
	typeID       int
	minY, height int
	hasSkyLight  bool
	defaultBiome string
}{
	{"", "minecraft:overworld", 0, -64, 384, true, "minecraft:plains"},
	{"DIM-1", "minecraft:the_nether", 3, 0, 256, false, "minecraft:nether_wastes"},
	{"DIM1", "minecraft:the_end", 2, 0, 256, false, "minecraft:the_end"},
}

var regionFileRe = regexp.MustCompile(`^r\.(-?\d+)\.(-?\d+)\.mca$`)

// LoadWorld loads the world from the given path, using the specified dimension.
func LoadWorld(worldPath, dimension string) *World {
	w := &World{
		Chunks: make(map[[2]int]*Chunk),
	}

	dimName := "minecraft:" + dimension

	for _, def := range dimDefs {
		dimPath := worldPath
		if def.subDir != "" {
			dimPath = filepath.Join(worldPath, def.subDir)
		}
		regionDir := filepath.Join(dimPath, "region")
		if _, err := os.Stat(regionDir); os.IsNotExist(err) {
			continue
		}

		dim := &DimensionInfo{
			Name:         def.name,
			TypeID:       def.typeID,
			DirPath:      dimPath,
			MinY:         def.minY,
			Height:       def.height,
			Sections:     def.height / 16,
			HasSkyLight:  def.hasSkyLight,
			DefaultBiome: GetBiomeID(def.defaultBiome),
		}
		w.Dimensions = append(w.Dimensions, dim)
		log.Printf("Found dimension: %s (%s)", dim.Name, regionDir)

		if dim.Name == dimName {
			w.Active = dim
		}
	}

	if len(w.Dimensions) == 0 {
		log.Fatal("No dimensions found in world directory")
	}

	if w.Active == nil {
		log.Fatalf("Dimension %q not found in world (available: %v)", dimName, dimensionNames(w.Dimensions))
	}
	log.Printf("Active dimension: %s (typeID=%d, sections=%d)", w.Active.Name, w.Active.TypeID, w.Active.Sections)

	// Load chunks for the active dimension
	regionDir := filepath.Join(w.Active.DirPath, "region")
	entries, err := os.ReadDir(regionDir)
	if err != nil {
		log.Fatalf("Failed to read region directory: %v", err)
	}

	minSection := w.Active.MinY / 16
	for _, entry := range entries {
		matches := regionFileRe.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		regionX, _ := strconv.Atoi(matches[1])
		regionZ, _ := strconv.Atoi(matches[2])

		regionPath := filepath.Join(regionDir, entry.Name())
		chunkNBTs, err := LoadRegionChunks(regionPath, regionX, regionZ)
		if err != nil {
			log.Printf("Warning: failed to load region %s: %v", entry.Name(), err)
			continue
		}

		for pos, nbt := range chunkNBTs {
			// Check chunk status
			if status, ok := nbt["Status"]; ok {
				statusStr := status.(string)
				if statusStr != "minecraft:full" && statusStr != "full" {
					continue
				}
			}

			// Adjust section Y indices for negative minY
			adjustedNBT := nbt
			if minSection < 0 {
				adjustedNBT = adjustSectionIndices(nbt, minSection)
			}

			blockEntities := collectBlockEntities(adjustedNBT, pos[0], pos[1])
			chunk := &Chunk{
				X:             pos[0],
				Z:             pos[1],
				PacketData:    BuildChunkPacketData(pos[0], pos[1], adjustedNBT, w.Active.Sections, w.Active.DefaultBiome, w.Active.HasSkyLight),
				BlockEntities: buildBlockEntityUpdates(blockEntities),
			}
			w.Chunks[pos] = chunk
		}

		fmt.Printf("  Loaded region r.%d.%d.mca: %d chunks\n", regionX, regionZ, len(chunkNBTs))
	}

	log.Printf("Total chunks loaded: %d", len(w.Chunks))
	return w
}

func dimensionNames(dims []*DimensionInfo) []string {
	names := make([]string, len(dims))
	for i, d := range dims {
		names[i] = d.Name
	}
	return names
}

func adjustSectionIndices(nbt map[string]interface{}, minSection int) map[string]interface{} {
	result := make(map[string]interface{}, len(nbt))
	for k, v := range nbt {
		result[k] = v
	}

	if sectionsNBT, ok := nbt["sections"]; ok {
		list := sectionsNBT.(NBTList)
		newValues := make([]interface{}, len(list.Values))
		for i, v := range list.Values {
			section := v.(map[string]interface{})
			newSection := make(map[string]interface{}, len(section))
			for k, sv := range section {
				newSection[k] = sv
			}
			// Shift Y by -minSection so it becomes 0-based
			y := int(int8(nbtByte(section, "Y")))
			newSection["Y"] = byte(y - minSection)
			newValues[i] = newSection
		}
		result["sections"] = NBTList{ElementType: list.ElementType, Values: newValues}
	}

	return result
}

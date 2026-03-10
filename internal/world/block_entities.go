package world

var blockEntityTypeIDs = map[string]int32{
	"minecraft:banner":                  0,
	"minecraft:barrel":                  1,
	"minecraft:beacon":                  2,
	"minecraft:bed":                     3,
	"minecraft:beehive":                 4,
	"minecraft:bell":                    5,
	"minecraft:blast_furnace":           6,
	"minecraft:brewing_stand":           7,
	"minecraft:brushable_block":         8,
	"minecraft:calibrated_sculk_sensor": 9,
	"minecraft:campfire":                10,
	"minecraft:chest":                   11,
	"minecraft:chiseled_bookshelf":      12,
	"minecraft:command_block":           13,
	"minecraft:comparator":              14,
	"minecraft:conduit":                 15,
	"minecraft:crafter":                 16,
	"minecraft:creaking_heart":          17,
	"minecraft:daylight_detector":       18,
	"minecraft:decorated_pot":           19,
	"minecraft:dispenser":               20,
	"minecraft:dropper":                 21,
	"minecraft:enchanting_table":        22,
	"minecraft:end_gateway":             23,
	"minecraft:end_portal":              24,
	"minecraft:ender_chest":             25,
	"minecraft:furnace":                 26,
	"minecraft:hanging_sign":            27,
	"minecraft:hopper":                  28,
	"minecraft:jigsaw":                  29,
	"minecraft:jukebox":                 30,
	"minecraft:lectern":                 31,
	"minecraft:mob_spawner":             32,
	"minecraft:piston":                  33,
	"minecraft:sculk_catalyst":          34,
	"minecraft:sculk_sensor":            35,
	"minecraft:sculk_shrieker":          36,
	"minecraft:shulker_box":             37,
	"minecraft:sign":                    38,
	"minecraft:skull":                   39,
	"minecraft:smoker":                  40,
	"minecraft:structure_block":         41,
	"minecraft:trapped_chest":           42,
	"minecraft:trial_spawner":           43,
	"minecraft:vault":                   44,

	// Alias used by some tooling/data versions.
	"minecraft:spawner": 32,
}

func GetBlockEntityTypeID(id string) (int32, bool) {
	if typeID, ok := blockEntityTypeIDs[id]; ok {
		return typeID, true
	}
	if typeID, ok := blockEntityTypeIDs["minecraft:"+id]; ok {
		return typeID, true
	}
	return 0, false
}

package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"nekolimbo/internal/protocol"
	"nekolimbo/internal/world"
)

const (
	ProtocolVersion        = 769
	VersionName            = "1.21.4"
	CompressionThreshold   = 256
	VelocityForwardVersion = 4
)

type Property struct {
	Name      string
	Value     string
	Signature string
	HasSig    bool
}

type Connection struct {
	conn   *protocol.Conn
	server *Server

	protocolVersion int32
	username        string
	uuid            [16]byte
	properties      []Property
}

type knownPack struct {
	namespace string
	id        string
	version   string
}

func (c *Connection) Handle() {
	defer c.conn.Close()

	nextState, err := c.handleHandshake()
	if err != nil {
		return
	}

	switch nextState {
	case 1:
		c.handleStatus()
	case 2:
		if err := c.handleLogin(); err != nil {
			log.Printf("[%s] Login failed: %v", c.conn.RemoteAddr(), err)
			return
		}
		if err := c.handleConfiguration(); err != nil {
			log.Printf("[%s] Configuration failed: %v", c.username, err)
			return
		}
		c.handlePlay()
	}
}

// --- Handshake ---

func (c *Connection) handleHandshake() (int, error) {
	id, reader, err := c.conn.ReadPacket()
	if err != nil {
		return 0, err
	}
	if id != 0x00 {
		return 0, fmt.Errorf("expected handshake, got 0x%02x", id)
	}

	c.protocolVersion, _ = reader.ReadVarInt()
	_, _ = reader.ReadString() // server address
	_, _ = reader.ReadUint16() // port
	nextState, _ := reader.ReadVarInt()
	return int(nextState), nil
}

// --- Status ---

type statusResponse struct {
	Version     statusVersion     `json:"version"`
	Players     statusPlayers     `json:"players"`
	Description statusDescription `json:"description"`
}
type statusVersion struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}
type statusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}
type statusDescription struct {
	Text string `json:"text"`
}

func (c *Connection) handleStatus() {
	cfg := c.server.Config

	for {
		id, reader, err := c.conn.ReadPacket()
		if err != nil {
			return
		}
		switch id {
		case 0x00: // Status Request
			resp := statusResponse{
				Version:     statusVersion{Name: VersionName, Protocol: ProtocolVersion},
				Players:     statusPlayers{Max: cfg.Server.MaxPlayers, Online: int(c.server.playerCount.Load())},
				Description: statusDescription{Text: cfg.Server.MOTD},
			}
			jsonBytes, _ := json.Marshal(resp)
			c.conn.SendPacket(0x00, func(w *protocol.PacketWriter) {
				w.WriteString(string(jsonBytes))
			})
			c.conn.Flush()

		case 0x01: // Ping Request
			payload, _ := reader.ReadInt64()
			c.conn.SendPacket(0x01, func(w *protocol.PacketWriter) {
				w.WriteInt64(payload)
			})
			c.conn.Flush()
			return
		}
	}
}

// --- Login ---

func (c *Connection) handleLogin() error {
	id, reader, err := c.conn.ReadPacket()
	if err != nil {
		return err
	}
	if id != 0x00 {
		return fmt.Errorf("expected login start, got 0x%02x", id)
	}
	c.username, _ = reader.ReadString()
	c.uuid, _ = reader.ReadUUID()

	// Velocity modern forwarding
	if c.server.Config.Velocity.Enabled {
		if err := c.handleVelocityForwarding(); err != nil {
			return fmt.Errorf("velocity forwarding: %w", err)
		}
	}

	// Set compression
	c.conn.SendPacket(0x03, func(w *protocol.PacketWriter) {
		w.WriteVarInt(CompressionThreshold)
	})
	c.conn.Flush()
	c.conn.Compressed = true
	c.conn.Threshold = CompressionThreshold

	// Login Success
	c.conn.SendPacket(0x02, func(w *protocol.PacketWriter) {
		w.WriteUUID(c.uuid)
		w.WriteString(c.username)
		w.WriteVarInt(int32(len(c.properties)))
		for _, prop := range c.properties {
			w.WriteString(prop.Name)
			w.WriteString(prop.Value)
			w.WriteBool(prop.HasSig)
			if prop.HasSig {
				w.WriteString(prop.Signature)
			}
		}
	})
	c.conn.Flush()

	// Wait for Login Acknowledged
	id, _, err = c.conn.ReadPacket()
	if err != nil {
		return err
	}
	if id != 0x03 {
		return fmt.Errorf("expected login acknowledged, got 0x%02x", id)
	}

	log.Printf("[%s] %s logged in", c.conn.RemoteAddr(), c.username)
	c.server.playerCount.Add(1)
	return nil
}

func (c *Connection) handleVelocityForwarding() error {
	// Send Login Plugin Request
	c.conn.SendPacket(0x04, func(w *protocol.PacketWriter) {
		w.WriteVarInt(1) // message ID
		w.WriteString("velocity:player_info")
		w.WriteByte(byte(VelocityForwardVersion))
	})
	c.conn.Flush()

	// Read Login Plugin Response
	id, reader, err := c.conn.ReadPacket()
	if err != nil {
		return err
	}
	if id != 0x02 {
		return fmt.Errorf("expected login plugin response, got 0x%02x", id)
	}

	msgID, _ := reader.ReadVarInt()
	if msgID != 1 {
		return fmt.Errorf("unexpected message ID: %d", msgID)
	}

	hasData, _ := reader.ReadBool()
	if !hasData {
		return fmt.Errorf("client did not respond to velocity forwarding (not behind proxy?)")
	}

	data := reader.Remaining()
	if len(data) < 32 {
		return fmt.Errorf("forwarding data too short")
	}

	// Verify HMAC
	signature := data[:32]
	forwardedData := data[32:]

	mac := hmac.New(sha256.New, []byte(c.server.Config.Velocity.Secret))
	mac.Write(forwardedData)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return fmt.Errorf("invalid HMAC signature (wrong secret?)")
	}

	// Parse forwarded data
	fwd := protocol.NewPacketReader(forwardedData)
	version, _ := fwd.ReadVarInt()
	_ = version
	addr, _ := fwd.ReadString()
	_ = addr
	c.uuid, _ = fwd.ReadUUID()
	c.username, _ = fwd.ReadString()

	propCount, _ := fwd.ReadVarInt()
	c.properties = make([]Property, propCount)
	for i := range c.properties {
		c.properties[i].Name, _ = fwd.ReadString()
		c.properties[i].Value, _ = fwd.ReadString()
		c.properties[i].HasSig, _ = fwd.ReadBool()
		if c.properties[i].HasSig {
			c.properties[i].Signature, _ = fwd.ReadString()
		}
	}

	log.Printf("[%s] Velocity forwarding: %s (%s) from %s", c.conn.RemoteAddr(), c.username, uuidToString(c.uuid), addr)
	return nil
}

// --- Configuration ---

func (c *Connection) handleConfiguration() error {
	// Send Known Packs
	c.conn.SendPacket(0x0e, func(w *protocol.PacketWriter) {
		w.WriteVarInt(1) // 1 pack
		w.WriteString("minecraft")
		w.WriteString("core")
		w.WriteString(VersionName)
	})
	c.conn.Flush()

	gotKnownPacks := false
	for {
		id, reader, err := c.conn.ReadPacket()
		if err != nil {
			return err
		}

		switch id {
		case 0x07: // Known Packs response
			gotKnownPacks = true
			packs, err := readKnownPacks(reader)
			if err != nil {
				log.Printf("[%s] Failed to parse known packs, sending full registry data: %v", c.username, err)
			} else {
				log.Printf("[%s] Known packs from client: %s", c.username, formatKnownPacks(packs))
				if !hasCorePack(packs, VersionName) {
					log.Printf("[%s] Known packs mismatch, sending full registry data", c.username)
				}
			}
			c.sendRegistryData(true)
			c.sendUpdateTags()
			c.conn.SendPacket(0x03, func(w *protocol.PacketWriter) {})
			c.conn.Flush()

		case 0x03:
			if gotKnownPacks {
				return nil
			}

		default:
			// Ignore Client Information, Plugin Message, etc.
		}
	}
}

func readKnownPacks(reader *protocol.PacketReader) ([]knownPack, error) {
	count, err := reader.ReadVarInt()
	if err != nil {
		return nil, err
	}
	packs := make([]knownPack, 0, count)
	for i := int32(0); i < count; i++ {
		namespace, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		id, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		version, err := reader.ReadString()
		if err != nil {
			return nil, err
		}
		packs = append(packs, knownPack{namespace: namespace, id: id, version: version})
	}
	return packs, nil
}

func hasCorePack(packs []knownPack, version string) bool {
	for _, pack := range packs {
		if pack.namespace == "minecraft" && pack.id == "core" && pack.version == version {
			return true
		}
	}
	return false
}

func formatKnownPacks(packs []knownPack) string {
	if len(packs) == 0 {
		return "(none)"
	}
	out := ""
	for i, pack := range packs {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%s:%s@%s", pack.namespace, pack.id, pack.version)
		if len(out) > 240 {
			out += "..."
			break
		}
	}
	return out
}

func (c *Connection) sendRegistryData(fullData bool) {
	if fullData {
		for _, reg := range world.FullRegistryData {
			c.conn.SendPacket(0x07, func(w *protocol.PacketWriter) {
				w.WriteString(reg.ID)
				w.WriteVarInt(int32(len(reg.Entries)))
				for _, entry := range reg.Entries {
					w.WriteString(entry.Name)
					w.WriteBool(true)
					w.WriteBytes(entry.Data)
				}
			})
		}
	} else {
		for _, reg := range world.SyncedRegistries {
			c.conn.SendPacket(0x07, func(w *protocol.PacketWriter) {
				w.WriteString(reg.ID)
				w.WriteVarInt(int32(len(reg.Entries)))
				for _, entry := range reg.Entries {
					w.WriteString(entry)
					w.WriteBool(false)
				}
			})
		}
	}
}

func (c *Connection) sendUpdateTags() {
	c.conn.SendPacket(0x0d, func(w *protocol.PacketWriter) {
		w.WriteVarInt(int32(len(world.ConfigurationTags)))
		for _, reg := range world.ConfigurationTags {
			w.WriteString(reg.Registry)
			w.WriteVarInt(int32(len(reg.Tags)))
			for _, tag := range reg.Tags {
				w.WriteString(tag.Name)
				w.WriteVarInt(int32(len(tag.Entries)))
				for _, id := range tag.Entries {
					w.WriteVarInt(id)
				}
			}
		}
	})
}

// --- Play ---

func (c *Connection) handlePlay() {
	defer c.server.playerCount.Add(-1)

	cfg := c.server.Config
	w := c.server.World
	dim := w.Active

	// Join Game (0x2c)
	c.conn.SendPacket(0x2c, func(pw *protocol.PacketWriter) {
		pw.WriteInt32(1)    // entity ID
		pw.WriteBool(false) // is hardcore
		// Dimension names
		pw.WriteVarInt(3)
		pw.WriteString("minecraft:overworld")
		pw.WriteString("minecraft:the_nether")
		pw.WriteString("minecraft:the_end")
		pw.WriteVarInt(int32(cfg.Server.MaxPlayers))
		pw.WriteVarInt(int32(cfg.Player.ViewDistance))
		pw.WriteVarInt(int32(cfg.Player.ViewDistance)) // simulation distance
		pw.WriteBool(false)                            // reduced debug info
		pw.WriteBool(true)                             // enable respawn screen
		pw.WriteBool(false)                            // do limited crafting
		// SpawnInfo (worldState)
		pw.WriteVarInt(int32(dim.TypeID)) // dimension type
		pw.WriteString(dim.Name)          // dimension name
		pw.WriteInt64(0)                  // hashed seed
		pw.WriteInt8(int8(cfg.Player.GameMode))
		pw.WriteUint8(255)  // previous game mode (-1)
		pw.WriteBool(false) // is debug
		pw.WriteBool(false) // is flat
		pw.WriteBool(false) // has death location
		pw.WriteVarInt(0)   // portal cooldown
		pw.WriteVarInt(63)  // sea level
		pw.WriteBool(true)  // enforces secure chat (suppresses "can't verify" toast)
	})

	// Player Abilities (0x3a)
	abilityFlags := byte(0)
	if cfg.Player.GameMode == 1 {
		abilityFlags = 0x01 | 0x04 | 0x08 // invulnerable, allow flying, creative
	} else if cfg.Player.GameMode == 3 {
		abilityFlags = 0x01 | 0x02 | 0x04 // invulnerable, flying, allow flying
	}
	c.conn.SendPacket(0x3a, func(pw *protocol.PacketWriter) {
		pw.WriteByte(abilityFlags)
		pw.WriteFloat32(0.05) // flying speed
		pw.WriteFloat32(0.1)  // walking speed
	})

	// Set Default Spawn Position (0x5b)
	spawnX := int(cfg.Player.SpawnX)
	spawnY := int(cfg.Player.SpawnY)
	spawnZ := int(cfg.Player.SpawnZ)
	c.conn.SendPacket(0x5b, func(pw *protocol.PacketWriter) {
		pw.WritePosition(spawnX, spawnY, spawnZ)
		pw.WriteFloat32(0)
	})

	// Synchronize Player Position (0x42)
	c.conn.SendPacket(0x42, func(pw *protocol.PacketWriter) {
		pw.WriteVarInt(0) // teleport ID
		pw.WriteFloat64(cfg.Player.SpawnX)
		pw.WriteFloat64(cfg.Player.SpawnY)
		pw.WriteFloat64(cfg.Player.SpawnZ)
		pw.WriteFloat64(0) // delta X
		pw.WriteFloat64(0) // delta Y
		pw.WriteFloat64(0) // delta Z
		pw.WriteFloat32(cfg.Player.SpawnYaw)
		pw.WriteFloat32(cfg.Player.SpawnPitch)
		pw.WriteInt32(0) // flags (all absolute)
	})

	// Set Center Chunk (0x58)
	centerChunkX := int(cfg.Player.SpawnX) >> 4
	centerChunkZ := int(cfg.Player.SpawnZ) >> 4
	c.conn.SendPacket(0x58, func(pw *protocol.PacketWriter) {
		pw.WriteVarInt(int32(centerChunkX))
		pw.WriteVarInt(int32(centerChunkZ))
	})

	// Game Event: Start waiting for level chunks (0x23)
	c.conn.SendPacket(0x23, func(pw *protocol.PacketWriter) {
		pw.WriteUint8(13)  // reason: start waiting for chunks
		pw.WriteFloat32(0) // value
	})

	c.conn.Flush()

	// Send chunks in a batch
	c.sendChunks(w, cfg.Player.ViewDistance, centerChunkX, centerChunkZ)

	// Player Info Update (0x40) — add self to tab list
	c.conn.SendPacket(0x40, func(pw *protocol.PacketWriter) {
		pw.WriteUint8(0x01 | 0x04 | 0x08 | 0x10) // actions: add_player, update_game_mode, update_listed, update_latency
		pw.WriteVarInt(1)                        // 1 entry
		pw.WriteUUID(c.uuid)
		pw.WriteString(c.username)
		pw.WriteVarInt(int32(len(c.properties)))
		for _, prop := range c.properties {
			pw.WriteString(prop.Name)
			pw.WriteString(prop.Value)
			pw.WriteBool(prop.HasSig)
			if prop.HasSig {
				pw.WriteString(prop.Signature)
			}
		}
		pw.WriteVarInt(int32(cfg.Player.GameMode))
		pw.WriteBool(true)
		pw.WriteVarInt(0)
	})

	// Tab List Header/Footer (0x74)
	c.conn.SendPacket(0x74, func(pw *protocol.PacketWriter) {
		writeTextComponentNBT(pw, cfg.Limbo.TabHeader)
		writeTextComponentNBT(pw, cfg.Limbo.TabFooter)
	})

	// System Chat — join message (0x73)
	if cfg.Limbo.JoinMessage != "" {
		msg := strings.ReplaceAll(cfg.Limbo.JoinMessage, "{player}", c.username)
		c.conn.SendPacket(0x73, func(pw *protocol.PacketWriter) {
			writeTextComponentNBT(pw, msg)
			pw.WriteBool(false) // not action bar
		})
	}

	c.conn.Flush()

	log.Printf("[%s] %s entered play state (%d chunks sent)", c.conn.RemoteAddr(), c.username, len(w.Chunks))

	// Keep alive loop
	c.keepAliveLoop()

	log.Printf("[%s] %s disconnected", c.conn.RemoteAddr(), c.username)
}

func (c *Connection) sendChunks(w *world.World, viewDistance int, centerX, centerZ int) {
	// Chunk Batch Start (0x0d)
	c.conn.SendPacket(0x0d, func(pw *protocol.PacketWriter) {})

	count := 0
	var blockEntities []world.BlockEntityUpdate
	for z := centerZ - viewDistance; z <= centerZ+viewDistance; z++ {
		for x := centerX - viewDistance; x <= centerX+viewDistance; x++ {
			if chunk, ok := w.Chunks[[2]int{x, z}]; ok {
				c.conn.WritePacket(0x28, chunk.PacketData)
				blockEntities = append(blockEntities, chunk.BlockEntities...)
			} else {
				emptyChunk := world.BuildChunkPacketData(x, z, map[string]interface{}{}, w.Active.Sections, w.Active.DefaultBiome, w.Active.HasSkyLight)
				c.conn.WritePacket(0x28, emptyChunk)
			}
			count++
		}
	}

	// Chunk Batch Finished (0x0c)
	c.conn.SendPacket(0x0c, func(pw *protocol.PacketWriter) {
		pw.WriteVarInt(int32(count))
	})
	for _, blockEntity := range blockEntities {
		c.conn.SendPacket(0x07, func(pw *protocol.PacketWriter) {
			pw.WritePosition(blockEntity.X, blockEntity.Y, blockEntity.Z)
			pw.WriteVarInt(blockEntity.TypeID)
			if len(blockEntity.NBT) == 0 {
				pw.WriteUint8(world.TagEnd)
				return
			}
			pw.WriteBytes(blockEntity.NBT)
		})
	}
	c.conn.Flush()
}

func (c *Connection) keepAliveLoop() {
	cfg := c.server.Config
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	needTeleport := make(chan struct{}, 1)
	done := make(chan struct{})
	var teleportID int32

	go func() {
		defer close(done)
		for {
			id, reader, err := c.conn.ReadPacket()
			if err != nil {
				return
			}
			if cfg.Limbo.VoidTPY != 0 {
				switch id {
				case 0x1c, 0x1d: // Set Player Position / Set Player Position and Rotation
					reader.ReadFloat64() // x
					y, _ := reader.ReadFloat64()
					if y < cfg.Limbo.VoidTPY {
						select {
						case needTeleport <- struct{}{}:
						default:
						}
					}
				}
			}
		}
	}()

	var keepAliveID int64
	for {
		select {
		case <-ticker.C:
			keepAliveID++
			err := c.conn.SendPacket(0x27, func(pw *protocol.PacketWriter) {
				pw.WriteInt64(keepAliveID)
			})
			if err != nil {
				return
			}
			c.conn.Flush()
		case <-needTeleport:
			teleportID++
			c.conn.SendPacket(0x42, func(pw *protocol.PacketWriter) {
				pw.WriteVarInt(teleportID)
				pw.WriteFloat64(cfg.Player.SpawnX)
				pw.WriteFloat64(cfg.Player.SpawnY)
				pw.WriteFloat64(cfg.Player.SpawnZ)
				pw.WriteFloat64(0) // delta X
				pw.WriteFloat64(0) // delta Y
				pw.WriteFloat64(0) // delta Z
				pw.WriteFloat32(cfg.Player.SpawnYaw)
				pw.WriteFloat32(cfg.Player.SpawnPitch)
				pw.WriteInt32(0) // flags (all absolute)
			})
			if cfg.Limbo.VoidMessage != "" {
				c.conn.SendPacket(0x73, func(pw *protocol.PacketWriter) {
					writeTextComponentNBT(pw, cfg.Limbo.VoidMessage)
					pw.WriteBool(false)
				})
			}
			c.conn.Flush()
		case <-done:
			return
		}
	}
}

func uuidToString(uuid [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// writeTextComponentNBT writes a text component as anonymous NBT to the packet.
// Empty string writes TAG_End (null). Otherwise writes {"text":"..."}.
func writeTextComponentNBT(pw *protocol.PacketWriter, text string) {
	if text == "" {
		pw.WriteUint8(world.TagEnd)
		return
	}
	var buf bytes.Buffer
	world.WriteAnonymousNBT(&buf, map[string]interface{}{"text": text})
	pw.WriteBytes(buf.Bytes())
}

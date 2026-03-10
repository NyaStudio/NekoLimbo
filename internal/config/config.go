package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Velocity VelocityConfig `yaml:"velocity"`
	World    WorldConfig    `yaml:"world"`
	Player   PlayerConfig   `yaml:"player"`
	Limbo    LimboConfig    `yaml:"limbo"`
}

type ServerConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	MaxPlayers int    `yaml:"max_players"`
	MOTD       string `yaml:"motd"`
}

type VelocityConfig struct {
	Enabled bool   `yaml:"enabled"`
	Secret  string `yaml:"secret"`
}

type WorldConfig struct {
	Path      string `yaml:"path"`
	Dimension string `yaml:"dimension"`
}

type PlayerConfig struct {
	GameMode     int     `yaml:"game_mode"`
	ViewDistance int     `yaml:"view_distance"`
	SpawnX       float64 `yaml:"spawn_x"`
	SpawnY       float64 `yaml:"spawn_y"`
	SpawnZ       float64 `yaml:"spawn_z"`
	SpawnYaw     float32 `yaml:"spawn_yaw"`
	SpawnPitch   float32 `yaml:"spawn_pitch"`
}

type LimboConfig struct {
	TabHeader   string  `yaml:"tab_header"`
	TabFooter   string  `yaml:"tab_footer"`
	JoinMessage string  `yaml:"join_message"`
	VoidMessage string  `yaml:"void_message"`
	VoidTPY     float64 `yaml:"void_tp_y"`
}

func (c *Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

func Load(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 25565
	}
	if cfg.Server.MaxPlayers == 0 {
		cfg.Server.MaxPlayers = 20
	}
	if cfg.Server.MOTD == "" {
		cfg.Server.MOTD = "A NekoLimbo Server"
	}
	if cfg.World.Path == "" {
		cfg.World.Path = "map"
	}
	if cfg.World.Dimension == "" {
		cfg.World.Dimension = "overworld"
	}
	if cfg.Player.ViewDistance == 0 {
		cfg.Player.ViewDistance = 10
	}
	if cfg.Player.SpawnY == 0 {
		cfg.Player.SpawnY = 100
	}
	if cfg.Limbo.VoidTPY == 0 {
		cfg.Limbo.VoidTPY = -100
	}
	cfg.Limbo.TabHeader = translateColorCodes(cfg.Limbo.TabHeader)
	cfg.Limbo.TabFooter = translateColorCodes(cfg.Limbo.TabFooter)
	cfg.Limbo.JoinMessage = translateColorCodes(cfg.Limbo.JoinMessage)
	cfg.Limbo.VoidMessage = translateColorCodes(cfg.Limbo.VoidMessage)
	return &cfg
}

// translateColorCodes replaces &X color codes with §X (Minecraft formatting).
func translateColorCodes(s string) string {
	const codes = "0123456789abcdefklmnorABCDEFKLMNOR"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '&' && i+1 < len(s) && strings.ContainsRune(codes, rune(s[i+1])) {
			b.WriteString("§")
			i++
			b.WriteByte(s[i])
		} else {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

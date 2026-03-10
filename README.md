# NekoLimbo

A lightweight Minecraft limbo server written in Go, for Java Edition 1.21.4.

Loads a vanilla world save and serves it as a read-only lobby. Players can look around, see the world, and get teleported back when falling into the void.

## Features

- Vanilla Anvil world loading (`.mca` region files)
- Overworld / Nether / End dimension support
- Velocity modern forwarding
- Tab player list with customizable header & footer
- Configurable join message & void teleport message
- Void fall detection with auto-teleport to spawn
- `&` color code support in all messages

## Requirements

- Go 1.24+
- A Minecraft Java Edition world save (1.18+ format)

## Build

```bash
go build -o nekolimbo ./cmd/nekolimbo
```

## Usage

1. Place your world save in the `map/` directory (or configure the path)
2. Edit `config.yml`
3. Run the server:

```bash
./nekolimbo
```

## Configuration

```yaml
server:
  host: "0.0.0.0"
  port: 25565
  max_players: 20
  motd: "A NekoLimbo Server"

velocity:
  enabled: false
  secret: ""

world:
  path: "map"
  dimension: "overworld"  # overworld, the_nether, the_end

player:
  game_mode: 2  # 0=survival, 1=creative, 2=adventure, 3=spectator
  view_distance: 4
  spawn_x: 0.0
  spawn_y: 100.0
  spawn_z: 0.0
  spawn_yaw: 0.0
  spawn_pitch: 0.0

limbo:
  tab_header: "&bNekoLimbo"
  tab_footer: "&7You are in limbo"
  join_message: "&eWelcome to limbo, &f{player}&e!"
  void_message: "&aWhoops, teleported you back!"
  void_tp_y: -100.0  # set to 0 to disable
```

`{player}` in `join_message` is replaced with the player's username.

## License
AGPLv3
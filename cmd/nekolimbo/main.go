package main

import (
	"log"

	"nekolimbo/internal/config"
	"nekolimbo/internal/server"
	"nekolimbo/internal/world"
)

func main() {
	log.SetFlags(log.Ltime)

	cfg := config.Load("config.yml")
	log.Printf("Config loaded: %s", cfg.Address())

	w := world.LoadWorld(cfg.World.Path, cfg.World.Dimension)

	s := server.New(cfg, w)
	s.Start()
}

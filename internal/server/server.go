package server

import (
	"log"
	"net"
	"sync/atomic"

	"nekolimbo/internal/config"
	"nekolimbo/internal/protocol"
	"nekolimbo/internal/world"
)

type Server struct {
	Config      *config.Config
	World       *world.World
	playerCount atomic.Int32
}

func New(cfg *config.Config, w *world.World) *Server {
	return &Server{
		Config: cfg,
		World:  w,
	}
}

func (s *Server) Start() {
	addr := s.Config.Address()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}
	log.Printf("Listening on %s", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(c net.Conn) {
	conn := &Connection{
		conn:   protocol.NewConn(c),
		server: s,
	}
	conn.Handle()
}

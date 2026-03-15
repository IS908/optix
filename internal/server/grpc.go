package server

import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// GRPCServer wraps the gRPC server and registered services.
type GRPCServer struct {
	srv      *grpc.Server
	listener net.Listener
	addr     string
}

// NewGRPCServer creates a new gRPC server listening on the given address.
func NewGRPCServer(addr string) (*GRPCServer, error) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := grpc.NewServer()
	reflection.Register(srv) // enable reflection for grpcurl/debugging

	return &GRPCServer{
		srv:      srv,
		listener: lis,
		addr:     addr,
	}, nil
}

// Server returns the underlying grpc.Server for service registration.
func (s *GRPCServer) Server() *grpc.Server {
	return s.srv
}

// Serve starts serving gRPC requests (blocking).
func (s *GRPCServer) Serve() error {
	fmt.Printf("gRPC server listening on %s\n", s.addr)
	return s.srv.Serve(s.listener)
}

// GracefulStop gracefully stops the server.
func (s *GRPCServer) GracefulStop() {
	s.srv.GracefulStop()
}

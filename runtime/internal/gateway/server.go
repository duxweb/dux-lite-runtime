package gateway

import (
	"context"
	"net"
	"net/rpc"
	"time"

	"github.com/duxweb/dux-runtime/runtime/internal/transport"
	goridgeRpc "github.com/roadrunner-server/goridge/v3/pkg/rpc"
)

func (g *Service) RunAdmin(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		<-ctx.Done()
		return nil
	}

	network, address := transport.ParseEndpoint(socketPath)
	if err := transport.PrepareEndpoint(network, address); err != nil {
		return err
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		transport.CleanupEndpoint(network, address)
	}()

	server := rpc.NewServer()
	if err = server.RegisterName("Gateway", NewControl(g)); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		go server.ServeCodec(goridgeRpc.NewCodec(conn))
	}
}

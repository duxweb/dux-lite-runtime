package gateway

import (
	"context"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"time"

	goridgeRpc "github.com/roadrunner-server/goridge/v3/pkg/rpc"
)

func (g *Service) RunAdmin(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		<-ctx.Done()
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o777); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
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

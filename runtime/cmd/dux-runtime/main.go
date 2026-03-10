package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/duxweb/dux-runtime/runtime/internal/app"
	"github.com/duxweb/dux-runtime/runtime/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	runtimeApp, err := app.New(cfg)
	if err != nil {
		log.Fatalf("create runtime: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runtimeApp.Run(ctx); err != nil {
		log.Fatalf("run runtime: %v", err)
	}
}

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/abyssbugg/ant-router/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lifecycle := app.NewLifecycle()

	os.Exit(run(ctx, lifecycle))
}

func run(ctx context.Context, lifecycle *app.Lifecycle) int {
	<-ctx.Done()

	lifecycle.BeginDrain()
	lifecycle.Stop()

	return 0
}

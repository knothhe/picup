package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/knothhe/picup/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(cli.Main(ctx, os.Args[1:]))
}

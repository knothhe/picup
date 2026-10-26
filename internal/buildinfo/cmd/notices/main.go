package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/knothhe/picup/internal/buildinfo/notices"
	"github.com/knothhe/picup/internal/config"
)

func run() error {
	output := flag.String("output", "build/THIRD_PARTY_NOTICES.txt", "generated attribution text path")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	modules, err := notices.Discover(ctx, root)
	if err != nil {
		return err
	}
	moduleCount := len(modules)
	// These SVGs are embedded by Picup rather than provided by a Go module.
	modules = append(modules, notices.Module{Path: "Picup bundled gallery icons", Version: "bundled assets", Dir: filepath.Join(root, "internal", "gui", "gallery-assets")})
	b, err := notices.Render(modules, runtime.GOROOT())
	if err != nil {
		return err
	}
	if err := config.AtomicWrite(*output, b, 0644); err != nil {
		return err
	}
	fmt.Printf("Collected notices for %d compiled modules and bundled gallery icons into %s\n", moduleCount, filepath.Clean(*output))
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

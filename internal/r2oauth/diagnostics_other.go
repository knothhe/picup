//go:build !darwin && !linux

package r2oauth

import (
	"errors"
	"os"
)

func lockDiagnosticDirectory(string) (*os.File, func(), error) {
	return nil, nil, errors.New("safe Cloudflare diagnostics are unavailable on this platform")
}
func openDiagnosticFile(*os.Root, string, int) (*os.File, error) {
	return nil, errors.New("safe Cloudflare diagnostics are unavailable on this platform")
}

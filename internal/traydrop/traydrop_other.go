//go:build !darwin

package traydrop

import "github.com/egoist/mygo"

// Register is a no-op on platforms without an AppKit menu bar.
func Register(_ *mygo.Tray, _ string, _ func() bool, _ func([]string)) (func(), error) {
	return func() {}, nil
}

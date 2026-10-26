//go:build !darwin

package devmaintenance

import "errors"

func verifyOwnBundle(string) error {
	return errors.New("Dev maintenance currently supports macOS only")
}
func loginStatus() (int, error) { return 0, errors.New("macOS login items are unavailable") }
func unregisterLogin() error    { return errors.New("macOS login items are unavailable") }

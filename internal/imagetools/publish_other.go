//go:build !darwin

package imagetools

import "os"

// A hard link publishes the complete file and atomically refuses existing paths.
func publishNewFile(temporary, destination string) error {
	return os.Link(temporary, destination)
}

package imagetools

import "golang.org/x/sys/unix"

// RENAME_EXCL preserves atomic publication on macOS, including destinations on
// filesystems without hard-link support, such as removable exFAT volumes.
func publishNewFile(temporary, destination string) error {
	return unix.RenamexNp(temporary, destination, unix.RENAME_EXCL)
}

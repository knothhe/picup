//go:build darwin || linux

package r2oauth

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockDiagnosticDirectory(path string) (*os.File, func(), error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	var metadata unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &metadata); err != nil {
		file.Close()
		return nil, nil, err
	}
	if metadata.Uid != uint32(os.Getuid()) {
		file.Close()
		return nil, nil, errors.New("diagnostic directory belongs to another user")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
}

func openDiagnosticFile(root *os.Root, name string, flags int) (*os.File, error) {
	file, err := root.OpenFile(name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	var metadata unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &metadata); err != nil {
		file.Close()
		return nil, err
	}
	if metadata.Mode&unix.S_IFMT != unix.S_IFREG || metadata.Mode&0777 != 0600 || metadata.Uid != uint32(os.Getuid()) || metadata.Nlink != 1 {
		file.Close()
		return nil, errors.New("unsafe diagnostic file ownership, links, type or permissions")
	}
	return file, nil
}

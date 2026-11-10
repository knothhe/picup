package updatedmg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Inspect authenticates image against m, mounts a private snapshot of it
// read-only, verifies the Picup.app inside and, when destination is not empty,
// copies only that bundle to destination/Picup.app and verifies the copy. The
// DMG's Applications link and window layout stay behind. The volume is always
// detached, even when ctx is canceled.
func Inspect(ctx context.Context, image string, m Manifest, public, arch, destination string) (result error) {
	if runtime.GOOS != "darwin" {
		return errors.New("update: DMG updates require macOS")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "picup-dmg-*")
	if err != nil {
		return err
	}
	mounted := false
	defer func() {
		if !mounted {
			result = errors.Join(result, os.RemoveAll(work))
		}
	}()
	// Mount bytes that were authenticated here, not a path someone else can swap.
	snapshot := filepath.Join(work, "update.dmg")
	if err := copySnapshot(image, snapshot, m.Size); err != nil {
		return err
	}
	file, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	err = VerifySignature(file, m.Size, m.Signature, public)
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	mount := filepath.Join(work, "mount")
	if err := os.Mkdir(mount, 0o700); err != nil {
		return err
	}
	before, err := os.Stat(mount)
	if err != nil {
		return err
	}
	defer func() {
		if !mounted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(cleanup, "/usr/bin/hdiutil", "detach", mount).CombinedOutput(); err != nil {
			if out2, err2 := exec.CommandContext(cleanup, "/usr/bin/hdiutil", "detach", "-force", mount).CombinedOutput(); err2 != nil {
				result = errors.Join(result, fmt.Errorf("update: cannot detach %s: %v %s %s", mount, err, out, out2))
				return
			}
		}
		mounted = false
		result = errors.Join(result, os.RemoveAll(work))
	}()
	out, attachErr := exec.CommandContext(ctx, "/usr/bin/hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen",
		"-noverify", "-owners", "off", "-mountpoint", mount, snapshot).CombinedOutput()
	// An interrupted attach may still have mounted the volume.
	after, statErr := os.Stat(mount)
	mounted = statErr == nil && !os.SameFile(before, after)
	if attachErr != nil {
		return fmt.Errorf("update: cannot mount the DMG: %v %s", attachErr, out)
	}
	mounted = true
	bundle := filepath.Join(mount, BundleName)
	if err := verifyRelease(ctx, bundle, m.Version, arch); err != nil {
		return err
	}
	if destination == "" {
		return nil
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	staged := filepath.Join(destination, BundleName)
	if out, err := exec.CommandContext(ctx, "/usr/bin/ditto", bundle, staged).CombinedOutput(); err != nil {
		return fmt.Errorf("update: cannot copy Picup.app: %v %s", err, out)
	}
	return verifyRelease(ctx, staged, m.Version, arch)
}

func verifyRelease(ctx context.Context, bundle, version, arch string) error {
	if err := VerifyBundle(bundle, version, arch); err != nil {
		return err
	}
	// The code signature seals the whole bundle; strict mode rejects
	// unsealed or modified resources.
	if out, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput(); err != nil {
		return fmt.Errorf("update: Picup.app code signature is invalid: %v %s", err, out)
	}
	return nil
}

func copySnapshot(source, destination string, size int64) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, size+1))
	if err = errors.Join(err, out.Close()); err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("update: DMG is %d bytes, manifest says %d", n, size)
	}
	return nil
}

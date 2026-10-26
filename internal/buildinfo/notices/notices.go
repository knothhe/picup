// Package notices collects attribution texts from the modules compiled into
// Picup. The output includes source texts; it does not infer license obligations.
package notices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Module struct {
	Path, Version, Dir string
	LocalReplacement   bool
}

// Discover asks Go which modules supply packages to the two shipped binaries.
// It does not include unrelated modules solely because they appear in go.sum.
func Discover(ctx context.Context, root string) ([]Module, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-json", "./cmd/picup", "./cmd/picup-gui")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.New("could not resolve compiled modules; run go list -deps ./cmd/picup ./cmd/picup-gui to inspect the build error")
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	byPath := map[string]Module{}
	for {
		var pkg struct {
			Standard bool
			Module   *struct {
				Path, Version, Dir string
				Main               bool
				Replace            *struct{ Version, Dir string }
			}
		}
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("could not decode compiled module metadata")
		}
		if pkg.Standard || pkg.Module == nil || pkg.Module.Main {
			continue
		}
		m := Module{Path: pkg.Module.Path, Version: pkg.Module.Version, Dir: pkg.Module.Dir}
		if pkg.Module.Replace != nil {
			m.Version = pkg.Module.Replace.Version
			m.Dir = pkg.Module.Replace.Dir
			m.LocalReplacement = m.Version == ""
		}
		if m.Dir == "" || m.Path == "" {
			return nil, errors.New("compiled module has no source directory for attribution collection")
		}
		byPath[m.Path] = m
	}
	modules := make([]Module, 0, len(byPath))
	for _, m := range byPath {
		modules = append(modules, m)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	return modules, nil
}

func isNotice(name string) bool {
	name = strings.ToUpper(name)
	for _, base := range []string{"LICENSE", "LICENCE", "NOTICE", "COPYING", "COPYRIGHT", "AUTHORS", "PATENTS", "OFL"} {
		if name == base || strings.HasPrefix(name, base+".") || strings.HasPrefix(name, base+"-") {
			return true
		}
	}
	return false
}

func sourceTexts(module Module) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(module.Dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if isNotice(entry.Name()) {
			rel, e := filepath.Rel(module.Dir, path)
			if e != nil {
				return e
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not collect attribution texts for %s", module.Path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no license or notice text found for compiled module %s", module.Path)
	}
	return paths, nil
}

func appendText(w *bytes.Buffer, title, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("could not read attribution text %s", title)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(b) > 1024*1024 || !utf8.Valid(b) || len(bytes.TrimSpace(b)) == 0 {
		return fmt.Errorf("invalid or oversized attribution text %s", title)
	}
	fmt.Fprintf(w, "\n--- %s ---\n\n", title)
	w.Write(b)
	if b[len(b)-1] != '\n' {
		w.WriteByte('\n')
	}
	return nil
}

// Render sorts modules and nested source filenames to make generated releases
// reproducible. Absolute cache, checkout and toolchain paths never enter output.
func Render(modules []Module, goRoot string) ([]byte, error) {
	modules = append([]Module(nil), modules...)
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	var b bytes.Buffer
	b.WriteString("Picup — third-party licenses and notices\n\nThis file reproduces license, notice and attribution texts found in the Go modules compiled into the shipped Picup binaries, including nested bundled libraries. Each module is identified by its resolved build version. Additional source texts may cover optional components.\n")
	licenseDir := goRoot
	if _, err := os.Stat(filepath.Join(licenseDir, "LICENSE")); errors.Is(err, os.ErrNotExist) && filepath.Base(goRoot) == "libexec" {
		// Homebrew keeps the unmodified Go license next to libexec.
		licenseDir = filepath.Dir(goRoot)
	}
	if err := appendText(&b, "Go runtime / LICENSE", filepath.Join(licenseDir, "LICENSE")); err != nil {
		return nil, err
	}
	for _, name := range []string{"PATENTS"} {
		path := filepath.Join(goRoot, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			path = filepath.Join(licenseDir, name)
		}
		if _, err := os.Stat(path); err == nil {
			if err := appendText(&b, "Go runtime / "+name, path); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("could not read Go runtime attribution metadata")
		}
	}
	for _, m := range modules {
		paths, err := sourceTexts(m)
		if err != nil {
			return nil, err
		}
		version := m.Version
		if m.LocalReplacement {
			version = "local replacement"
		} else if version == "" {
			version = "version unavailable"
		}
		fmt.Fprintf(&b, "\n\n=== %s %s ===\n", m.Path, version)
		for _, rel := range paths {
			if err := appendText(&b, m.Path+" / "+rel, filepath.Join(m.Dir, filepath.FromSlash(rel))); err != nil {
				return nil, err
			}
		}
	}
	return b.Bytes(), nil
}

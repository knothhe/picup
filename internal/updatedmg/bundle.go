package updatedmg

import (
	"bytes"
	"debug/macho"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	BundleName       = "Picup.app"
	BundleIdentifier = "com.knothhe.picup"
	maxBundleEntries = 20000
)

// BundleReport describes a verified release bundle.
type BundleReport struct {
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	Identifier   string `json:"identifier"`
	Stage        string `json:"stage"`
}

// VerifyBundle checks a Picup.app directory without executing it: only
// directories, regular files and links that stay inside the bundle, the
// release identity and version, and native executables of arch.
func VerifyBundle(bundle, version, arch string) error {
	if !validArch(arch) {
		return errors.New("update: unsupported architecture")
	}
	root, err := filepath.Abs(bundle)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		return errors.New("update: Picup.app is missing")
	}
	var count int
	var total int64
	err = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if count++; count > maxBundleEntries {
			return errors.New("update: application bundle has too many files")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, name)
		switch mode := info.Mode(); {
		case mode.IsDir():
		case mode.IsRegular():
			if total += info.Size(); total > 2*maxDMGSize {
				return errors.New("update: application bundle exceeds its size limit")
			}
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(name), target))
			if filepath.IsAbs(target) || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
				return fmt.Errorf("update: link escapes the application bundle: %s", rel)
			}
		default:
			return fmt.Errorf("update: unsupported special file: %s", rel)
		}
		return nil
	})
	if err != nil {
		return err
	}
	contents := filepath.Join(root, "Contents")
	plist, err := readRegular(filepath.Join(contents, "Info.plist"), 1<<20)
	if err != nil {
		return err
	}
	values, err := plistStrings(plist)
	if err != nil {
		return fmt.Errorf("update: invalid Info.plist: %w", err)
	}
	if values["CFBundleIdentifier"] != BundleIdentifier || values["CFBundleExecutable"] != "Picup" ||
		values["CFBundleVersion"] != version || values["CFBundleShortVersionString"] != version {
		return errors.New("update: application identity or version does not match the release")
	}
	expected := macho.CpuArm64
	if arch == "amd64" {
		expected = macho.CpuAmd64
	}
	for _, rel := range []string{"MacOS/Picup", "Resources/picup"} {
		name := filepath.Join(contents, rel)
		info, err := os.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("update: %s is missing or not executable", rel)
		}
		binary, err := macho.Open(name)
		if err != nil {
			return fmt.Errorf("update: %s is not a Mach-O executable", rel)
		}
		ok := binary.Cpu == expected && binary.Type == macho.TypeExec
		binary.Close()
		if !ok {
			return fmt.Errorf("update: %s does not match the %s architecture", rel, arch)
		}
	}
	if notices, err := readRegular(filepath.Join(contents, "Resources", "THIRD_PARTY_NOTICES.txt"), 64<<20); err != nil || len(notices) == 0 {
		return errors.New("update: third-party notices are missing")
	}
	return nil
}

func readRegular(name string, limit int64) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("update: %s is missing, linked or too large", filepath.Base(name))
	}
	return os.ReadFile(name)
}

func plistStrings(content []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "dict" {
			break
		}
	}
	values := map[string]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if end, ok := token.(xml.EndElement); ok && end.Name.Local == "dict" {
			return values, nil
		}
		keyStart, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if keyStart.Name.Local != "key" {
			return nil, errors.New("invalid top-level property list dictionary")
		}
		var key string
		if err := decoder.DecodeElement(&key, &keyStart); err != nil {
			return nil, err
		}
		for {
			token, err = decoder.Token()
			if err != nil {
				return nil, err
			}
			if _, ok := token.(xml.StartElement); ok {
				break
			}
		}
		start := token.(xml.StartElement)
		if start.Name.Local == "string" {
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return nil, err
			}
			if _, exists := values[key]; exists {
				return nil, errors.New("duplicate property list key")
			}
			values[key] = value
		} else if err := decoder.Skip(); err != nil {
			return nil, err
		}
	}
}

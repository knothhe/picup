package updatefeed

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const maxExpandedSize = 2 << 30
const maxEntrySize = 256 << 20

type ReleaseMetadata struct {
	Schema     int    `json:"schema"`
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Version    string `json:"version"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Channel    string `json:"channel"`
}

func metadata(version, goos, arch string) ReleaseMetadata {
	return ReleaseMetadata{1, "Picup", "com.knothhe.picup", version, goos, arch, "release"}
}

func supportedTarget(target string) bool {
	return target == "darwin-arm64" || target == "darwin-amd64" || target == "linux-amd64" || target == "linux-arm64"
}

func archiveRoot(target string) string {
	if strings.HasPrefix(target, "darwin-") {
		return "Picup.app"
	}
	return "Picup"
}

// VerifyArchive validates final ZIP bytes with the pinned Ed25519 public key,
// then checks bounded paths, link containment, application identity and CPU.
// It does not extract or execute the package.
func VerifyArchive(filename string, asset Asset, publicKey, version, target string) error {
	if !supportedTarget(target) || !validVersion(version) || asset.Size <= 0 || asset.Size > MaxArchiveSize || !digestPattern.MatchString(asset.SHA256) {
		return errors.New("invalid ZIP update identity or size")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != asset.Size {
		return errors.New("ZIP size or file type does not match the feed")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid update public key")
	}
	signature, err := base64.StdEncoding.DecodeString(asset.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("invalid update signature")
	}
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, asset.Size+1)); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != asset.SHA256 || !ed25519.Verify(key, digest.Sum(nil), signature) {
		return errors.New("ZIP digest or Ed25519 signature does not match")
	}
	archive, err := zip.NewReader(file, asset.Size)
	if err != nil {
		return err
	}
	_, err = inspectArchive(archive, version, target)
	return err
}

func archiveName(name string) string { return strings.ToLower(norm.NFC.String(name)) }

func inspectArchive(archive *zip.Reader, version, target string) (map[string]*zip.File, error) {
	root := archiveRoot(target)
	entries := make(map[string]*zip.File)
	aliases := make(map[string]bool)
	spellings := make(map[string]string)
	var expanded uint64
	if len(archive.File) == 0 || len(archive.File) > 10000 {
		return nil, errors.New("empty or excessive ZIP entries")
	}
	for _, entry := range archive.File {
		name := strings.TrimSuffix(entry.Name, "/")
		mode := entry.Mode()
		if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\\x00:") || !(name == root || strings.HasPrefix(name, root+"/")) || aliases[archiveName(name)] || entry.Flags&1 != 0 {
			return nil, errors.New("invalid, duplicate or aliased ZIP path")
		}
		if !mode.IsRegular() && !mode.IsDir() && mode&os.ModeSymlink == 0 {
			return nil, errors.New("ZIP contains an unsupported file type")
		}
		if name == root && !mode.IsDir() {
			return nil, errors.New("ZIP root must be an application directory")
		}
		if mode.IsDir() != strings.HasSuffix(entry.Name, "/") || mode.IsDir() && entry.UncompressedSize64 != 0 {
			return nil, errors.New("invalid ZIP directory")
		}
		for component := name; component != "."; component = path.Dir(component) {
			alias := archiveName(component)
			if previous, ok := spellings[alias]; ok && previous != component {
				return nil, errors.New("ZIP contains aliased parent directories")
			}
			spellings[alias] = component
		}
		if entry.UncompressedSize64 > maxEntrySize || expanded > maxExpandedSize-entry.UncompressedSize64 {
			return nil, errors.New("uncompressed ZIP exceeds size limit")
		}
		expanded += entry.UncompressedSize64
		entries[name], aliases[archiveName(name)] = entry, true
	}
	for name, entry := range entries {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if prior := entries[parent]; prior != nil && !prior.Mode().IsDir() {
				return nil, errors.New("ZIP path is nested beneath a file or symbolic link")
			}
			// A case or Unicode alias must not hide a parent that has a
			// different spelling on the destination filesystem.
			if aliases[archiveName(parent)] && entries[parent] == nil {
				return nil, errors.New("ZIP has an aliased parent directory")
			}
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			if !strings.HasPrefix(target, "darwin-") {
				return nil, errors.New("Linux release ZIP must not contain links")
			}
			if _, err := resolveArchiveLink(name, entries, root, 0); err != nil {
				return nil, err
			}
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, err
		}
		n, err := io.Copy(io.Discard, io.LimitReader(stream, int64(entry.UncompressedSize64)+1))
		stream.Close()
		if err != nil || n != int64(entry.UncompressedSize64) {
			return nil, errors.New("ZIP file size or checksum does not match")
		}
	}
	goos, arch, _ := strings.Cut(target, "-")
	if goos == "darwin" {
		content, err := readEntry(entries[root+"/Contents/Info.plist"], 1<<20)
		if err != nil {
			return nil, err
		}
		if err := validatePlist(content, version); err != nil {
			return nil, err
		}
		for _, name := range []string{"Contents/MacOS/Picup", "Contents/Resources/picup"} {
			if err := verifyExecutable(entries[root+"/"+name], goos, arch); err != nil {
				return nil, err
			}
		}
		content, err = readEntry(entries[root+"/Contents/Resources/THIRD_PARTY_NOTICES.txt"], maxEntrySize)
		if err != nil || len(content) == 0 {
			return nil, errors.New("missing application license notices")
		}
	} else {
		allowed := map[string]bool{"picup-gui": true, "picup": true, "icon.png": true, "THIRD_PARTY_NOTICES.txt": true, "picup-release.json": true}
		for name := range entries {
			if name != root && !allowed[strings.TrimPrefix(name, root+"/")] {
				return nil, errors.New("unexpected file in Linux release ZIP")
			}
		}
		content, err := readEntry(entries[root+"/picup-release.json"], 64<<10)
		if err != nil {
			return nil, err
		}
		var got ReleaseMetadata
		if json.Unmarshal(content, &got) != nil || got != metadata(version, goos, arch) {
			return nil, errors.New("Linux release identity or version does not match")
		}
		for _, name := range []string{"picup-gui", "picup"} {
			if err := verifyExecutable(entries[root+"/"+name], goos, arch); err != nil {
				return nil, err
			}
		}
		for _, name := range []string{"icon.png", "THIRD_PARTY_NOTICES.txt"} {
			content, err := readEntry(entries[root+"/"+name], maxEntrySize)
			if err != nil || len(content) == 0 {
				return nil, fmt.Errorf("missing or empty Linux resource: %s", name)
			}
		}
	}
	return entries, nil
}

func readEntry(entry *zip.File, maximum uint64) ([]byte, error) {
	if entry == nil || !entry.Mode().IsRegular() || entry.UncompressedSize64 > maximum {
		return nil, errors.New("missing, linked or excessive required ZIP file")
	}
	stream, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	content, err := io.ReadAll(io.LimitReader(stream, int64(maximum)+1))
	if err != nil || uint64(len(content)) != entry.UncompressedSize64 {
		return nil, errors.New("ZIP content size or checksum does not match")
	}
	return content, nil
}

func resolveArchiveLink(name string, entries map[string]*zip.File, root string, depth int) (string, error) {
	parts := strings.Split(name, "/")
	var resolved []string
	for len(parts) > 0 {
		part := parts[0]
		parts = parts[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) <= 1 {
				return "", errors.New("ZIP symbolic link escapes the application")
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		resolved = append(resolved, part)
		if resolved[0] != root {
			return "", errors.New("ZIP symbolic link escapes the application")
		}
		entry := entries[strings.Join(resolved, "/")]
		if entry == nil || entry.Mode()&os.ModeSymlink == 0 {
			continue
		}
		depth++
		if depth > 40 {
			return "", errors.New("cyclic ZIP symbolic link")
		}
		stream, err := entry.Open()
		if err != nil {
			return "", err
		}
		content, err := io.ReadAll(io.LimitReader(stream, 4097))
		stream.Close()
		link := string(content)
		if err != nil || len(content) == 0 || len(content) > 4096 || path.IsAbs(link) || strings.ContainsAny(link, "\\\x00:") {
			return "", errors.New("invalid ZIP symbolic link")
		}
		resolved = resolved[:len(resolved)-1]
		parts = append(strings.Split(link, "/"), parts...)
	}
	name = strings.Join(resolved, "/")
	if entries[name] == nil {
		for child := range entries {
			if strings.HasPrefix(child, name+"/") {
				return name, nil
			}
		}
		return "", errors.New("ZIP symbolic link has no target")
	}
	return name, nil
}

func verifyExecutable(entry *zip.File, goos, arch string) error {
	content, err := readEntry(entry, maxEntrySize)
	if err != nil || entry.Mode().Perm()&0111 == 0 {
		return errors.New("missing or non-executable application program")
	}
	if goos == "darwin" {
		binary, err := macho.NewFile(bytes.NewReader(content))
		if err != nil {
			return errors.New("application program is not a Mach-O executable")
		}
		defer binary.Close()
		cpu := macho.CpuArm64
		if arch == "amd64" {
			cpu = macho.CpuAmd64
		}
		if binary.Cpu != cpu || binary.Type != macho.TypeExec {
			return errors.New("application Mach-O architecture does not match")
		}
	} else {
		binary, err := elf.NewFile(bytes.NewReader(content))
		if err != nil {
			return errors.New("application program is not an ELF executable")
		}
		defer binary.Close()
		cpu := elf.EM_X86_64
		if arch == "arm64" {
			cpu = elf.EM_AARCH64
		}
		if binary.Machine != cpu || binary.Class != elf.ELFCLASS64 || (binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN) {
			return errors.New("application ELF architecture does not match")
		}
	}
	return nil
}

func validatePlist(content []byte, version string) error {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	values := make(map[string]string)
	depth, dictionary := 0, 0
	var key string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if element.Name.Local == "dict" && dictionary == 0 {
				dictionary = depth
			}
			if depth == dictionary+1 && (element.Name.Local == "key" || element.Name.Local == "string") {
				var value string
				if err := decoder.DecodeElement(&value, &element); err != nil {
					return err
				}
				depth--
				if element.Name.Local == "key" {
					key = value
				} else if key != "" {
					if _, exists := values[key]; exists {
						return errors.New("duplicate application property")
					}
					values[key], key = value, ""
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	for key, expected := range map[string]string{"CFBundleIdentifier": "com.knothhe.picup", "CFBundleExecutable": "Picup", "CFBundleVersion": version, "CFBundleShortVersionString": version} {
		if values[key] != expected {
			return fmt.Errorf("application %s does not match release identity", key)
		}
	}
	return nil
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/knothhe/picup/internal/install"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIStatusWorksWithBrokenStorageConfiguration(t *testing.T) {
	root := t.TempDir()
	configuration := filepath.Join(root, "config.json")
	os.WriteFile(configuration, []byte("invalid json"), 0600)
	var out, log bytes.Buffer
	r := Runner{strings.NewReader(""), &out, &log}
	if status := r.Run(context.Background(), []string{"--config", configuration, "cli", "status", "--dir", root, "--output", "json"}); status != 0 {
		t.Fatal(status, log.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report["state"] != "missing" {
		t.Fatal(out.String(), err)
	}
}

func TestCLIUninstallRefusesForeignCommand(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, install.Filename())
	body := []byte("#!/bin/sh\necho other-program\n")
	os.WriteFile(path, body, 0700)
	var out, log bytes.Buffer
	r := Runner{strings.NewReader(""), &out, &log}
	if status := r.Run(context.Background(), []string{"cli", "uninstall", "--dir", root}); status == 0 {
		t.Fatal("foreign executable was uninstalled")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, body) {
		t.Fatal("foreign command changed", err)
	}
}

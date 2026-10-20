// Package clipboard keeps the CLI independent of a GUI event loop.
package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/knothhe/picup/internal/imageproc"
)

func ReadImage(ctx context.Context) ([]byte, error) {
	switch runtime.GOOS {
	case "darwin", "windows":
		dir, err := os.MkdirTemp("", "picup-clipboard-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "clipboard.png")
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			cmd = exec.CommandContext(ctx, "osascript", "-e", `on run argv
set targetFile to POSIX file (item 1 of argv)
try
set imageData to (the clipboard as «class PNGf»)
on error
set imageData to (the clipboard as TIFF picture)
end try
set handle to open for access targetFile with write permission
try
set eof handle to 0
write imageData to handle
close access handle
on error errorMessage
close access handle
error errorMessage
end try
end run`, path)
		} else {
			cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command", `Add-Type -AssemblyName System.Windows.Forms; $im=[Windows.Forms.Clipboard]::GetImage(); if ($null -eq $im) { exit 1 }; $im.Save($env:PICUP_CLIPBOARD_FILE,[Drawing.Imaging.ImageFormat]::Png); $im.Dispose()`)
			cmd.Env = append(os.Environ(), "PICUP_CLIPBOARD_FILE="+path)
		}
		if err = cmd.Run(); err != nil {
			return nil, fmt.Errorf("no readable image in clipboard: %w", err)
		}
		return imageproc.ReadFile(path)
	default:
		for _, args := range [][]string{{"wl-paste", "--no-newline", "--type", "image/png"}, {"xclip", "-selection", "clipboard", "-t", "image/png", "-o"}} {
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			var out limitedBuffer
			cmd.Stdout = &out
			if err := cmd.Run(); err == nil && out.Len() > 0 {
				return out.Bytes(), nil
			}
		}
		return nil, fmt.Errorf("no PNG in clipboard; Linux requires wl-clipboard or xclip")
	}
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > imageproc.MaxBytes {
		return 0, fmt.Errorf("clipboard image exceeds 100 MiB")
	}
	return b.Buffer.Write(p)
}

func WriteText(ctx context.Context, text string) error {
	var choices [][]string
	switch runtime.GOOS {
	case "darwin":
		choices = [][]string{{"pbcopy"}}
	case "windows":
		choices = [][]string{{"powershell", "-NoProfile", "-STA", "-Command", `$input | Set-Clipboard`}}
	default:
		choices = [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}}
	}
	for _, args := range choices {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("cannot copy result to clipboard")
}

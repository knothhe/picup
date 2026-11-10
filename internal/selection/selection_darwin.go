package selection

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ebitengine/purego/objc"
)

// FinderFrontmost must be called on the UI thread before showing a Picup window.
// NSWorkspace does not require Accessibility or Automation permission.
func FinderFrontmost() bool {
	workspace := objc.ID(objc.GetClass("NSWorkspace")).Send(objc.RegisterName("sharedWorkspace"))
	app := workspace.Send(objc.RegisterName("frontmostApplication"))
	identifier := app.Send(objc.RegisterName("bundleIdentifier"))
	return identifier != 0 && objc.Send[string](identifier, objc.RegisterName("UTF8String")) == "com.apple.finder"
}

// Read requests Finder permission only in response to an explicit upload.
// Read the selection before focusing the quick panel. The frontmost guard also
// rejects a context switch while the worker is starting, instead of uploading a
// stale selection from a background Finder window.
func Read(ctx context.Context) ([]string, error) {
	return readFinderOutput(ctx, func(ctx context.Context) ([]byte, error) {
		return exec.CommandContext(ctx, "/usr/bin/osascript", "-l", "JavaScript", "-e", finderSelectionScript).Output()
	})
}

func readFinderOutput(ctx context.Context, output func(context.Context) ([]byte, error)) ([]string, error) {
	data, err := output(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			detail := strings.TrimSpace(string(exit.Stderr))
			if strings.Contains(detail, "-1743") {
				return nil, ErrPermission
			}
			if detail != "" {
				return nil, fmt.Errorf("read Finder selection: %w: %s", err, detail)
			}
		}
		return nil, fmt.Errorf("read Finder selection: %w", err)
	}
	return decodeImages(data)
}

const finderSelectionScript = `
ObjC.import('AppKit');
function run() {
  const front = $.NSWorkspace.sharedWorkspace.frontmostApplication;
  if (!front || ObjC.unwrap(front.bundleIdentifier) !== 'com.apple.finder') return '[]';
  const finder = Application('com.apple.finder');
  return JSON.stringify(finder.selection().map(item => {
    const url = $.NSURL.URLWithString(item.url());
    return url.isFileURL ? ObjC.unwrap(url.path) : '';
  }));
}`

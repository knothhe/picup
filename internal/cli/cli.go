package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/knothhe/picup"
	"github.com/knothhe/picup/internal/clipboard"
	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/service"
	"github.com/knothhe/picup/internal/storage"
	"golang.org/x/term"
)

var Version = picup.Version()

const help = `picup: compress an image, upload to S3/R2, copy its link.

Usage:
  picup [files...] [options]      No files: upload clipboard image
  picup config                   Interactive setup
  picup config show              Show settings with credentials redacted
  picup config set KEY VALUE      Save a setting (use - to read stdin)
  picup config test               Write and delete a temporary test object
  picup config path              Print the shared settings path
  picup config export FILE       Export profiles without credentials
  picup config import FILE       Merge profiles; reconnect credentials
  picup history [--output json]   Show the last 200 uploads
  picup cli status [--dir DIR]    Inspect this channel's CLI installation
  picup cli uninstall [--dir DIR] Remove this channel's installed CLI

Options:
  --no-compress       Upload original bytes unless converting or resizing
  --format FORMAT    original, webp, avif, jpeg, png
  --quality N        Compression quality, 1-100 (default 85)
  --max-width N      Resize proportionally, never enlarge (0: unlimited)
  --output FORMAT    url, markdown, json
  --concurrency N    Simultaneous image jobs, 1-6 (default 3)
  --collision POLICY reuse, overwrite, rename, skip
  --progress         Print per-image byte progress to stderr
  --copy             Copy even when stdout is redirected
  --no-copy          Do not copy results
  --config PATH      Override shared configuration location
  --profile NAME     Use a profile by name or ID for this command
  --version          Print version
  --help             Show help

Precedence: command flags > PICUP_* environment > config file > defaults.
Credentials: PICUP_ACCESS_KEY / PICUP_SECRET_KEY or AWS_ACCESS_KEY_ID /
AWS_SECRET_ACCESS_KEY. Settings are shared with the Picup desktop app.
`

type Runner struct {
	In       io.Reader
	Out, Err io.Writer
}

func Main(ctx context.Context, args []string) int {
	return (Runner{os.Stdin, os.Stdout, os.Stderr}).Run(ctx, args)
}
func (r Runner) Run(ctx context.Context, args []string) int {
	if err := r.run(ctx, args); err != nil {
		fmt.Fprintln(r.Err, "picup:", err)
		return 1
	}
	return 0
}
func (r Runner) run(ctx context.Context, args []string) error {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(r.Out, help)
			return nil
		}
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintln(r.Out, "picup "+Version)
		return nil
	}
	path, err := config.Path()
	if err != nil {
		return err
	}
	var rest []string
	var profileSelection string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if a == "--config" {
			if i+1 == len(args) {
				return errors.New("--config needs a path")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(a, "--config=") {
			path = strings.TrimPrefix(a, "--config=")
		} else if a == "--profile" {
			if i+1 == len(args) {
				return errors.New("--profile needs a name or ID")
			}
			i++
			profileSelection = args[i]
			if profileSelection == "" {
				return errors.New("--profile needs a name or ID")
			}
		} else if strings.HasPrefix(a, "--profile=") {
			profileSelection = strings.TrimPrefix(a, "--profile=")
			if profileSelection == "" {
				return errors.New("--profile needs a name or ID")
			}
		} else {
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) > 0 && args[0] == "cli" {
		return r.manageCLI(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "config" && len(args) > 1 && args[1] == "path" {
		fmt.Fprintln(r.Out, path)
		return nil
	}
	c, err := config.Load(path)
	if err != nil {
		return err
	}
	persistedProfile := c.ActiveProfile
	if profileSelection != "" {
		id := ""
		for _, profile := range c.Profiles {
			if profile.ID == profileSelection {
				id = profile.ID
				break
			}
		}
		if id == "" {
			for _, profile := range c.Profiles {
				if profile.Name == profileSelection {
					id = profile.ID
					break
				}
			}
		}
		if id == "" {
			return fmt.Errorf("profile %q was not found", profileSelection)
		}
		if err := c.SwitchProfile(id); err != nil {
			return err
		}
	}
	if len(args) > 0 && args[0] == "config" {
		return r.configure(ctx, path, c, args[1:], persistedProfile)
	}
	if err = c.ApplyEnv(); err != nil {
		return err
	}
	if len(args) > 0 && args[0] == "history" {
		output := c.Output
		if len(args) > 1 {
			if len(args) != 3 || args[1] != "--output" {
				return errors.New("usage: picup history [--output url|markdown|json]")
			}
			output = args[2]
		}
		check := c
		check.Output = output
		if err = check.ValidateOptions(); err != nil {
			return err
		}
		rows, e := service.History(path)
		if e != nil {
			return e
		}
		fmt.Fprintln(r.Out, service.Format(rows, output))
		return nil
	}
	if len(args) > 0 && args[0] == "upload" {
		args = args[1:]
	}
	flags := flag.NewFlagSet("picup", flag.ContinueOnError)
	flags.SetOutput(r.Err)
	var noCompress, noCopy, forceCopy, progress bool
	flags.BoolVar(&noCompress, "no-compress", false, "")
	flags.BoolVar(&noCopy, "no-copy", false, "")
	flags.BoolVar(&forceCopy, "copy", false, "")
	flags.BoolVar(&progress, "progress", false, "")
	flags.IntVar(&c.UploadConcurrency, "concurrency", c.UploadConcurrency, "")
	flags.StringVar(&c.CollisionPolicy, "collision", c.CollisionPolicy, "")
	flags.StringVar(&c.Format, "format", c.Format, "")
	flags.IntVar(&c.Quality, "quality", c.Quality, "")
	flags.IntVar(&c.MaxWidth, "max-width", c.MaxWidth, "")
	flags.StringVar(&c.Output, "output", c.Output, "")
	// Go's standard flag parser stops at the first file. Accept options anywhere.
	var opts, files []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			files = append(files, a)
			continue
		}
		opts = append(opts, a)
		key := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-")
		if !strings.Contains(a, "=") && (key == "format" || key == "quality" || key == "max-width" || key == "output" || key == "concurrency" || key == "collision") {
			if i+1 == len(args) {
				return fmt.Errorf("--%s needs a value", key)
			}
			i++
			opts = append(opts, args[i])
		}
	}
	if err = flags.Parse(opts); err != nil {
		return err
	}
	if noCompress {
		c.Compress = false
	}
	if err = c.ValidateStorage(); err != nil {
		return err
	}
	if len(files) == 0 {
		files = []string{""}
	}
	inputs := make([]service.Input, len(files))
	for i, file := range files {
		inputs[i] = service.Input{Path: file, Name: file}
		if file == "" {
			data, e := clipboard.ReadImage(ctx)
			if e != nil {
				return e
			}
			inputs[i] = service.Input{Name: "clipboard.png", Data: data}
		}
	}
	outcomes := (service.Service{Config: c, ConfigPath: path}).Batch(ctx, inputs, c.UploadConcurrency, func(event service.Event) {
		if progress && event.Total > 0 {
			fmt.Fprintf(r.Err, "%s: %d/%d bytes\n", inputs[event.Index].Name, event.Sent, event.Total)
		}
	})
	var results []service.Result
	failed := false
	for i, outcome := range outcomes {
		name := inputs[i].Name
		if outcome.Skipped {
			fmt.Fprintf(r.Err, "%s: skipped existing object\n", name)
			continue
		}
		if outcome.Err != nil {
			failed = true
			fmt.Fprintf(r.Err, "%s: %v\n", name, outcome.Err)
			continue
		}
		result := outcome.Result
		results = append(results, result)
		if result.Reused {
			fmt.Fprintf(r.Err, "%s: reused existing object\n", name)
		} else {
			fmt.Fprintf(r.Err, "%s: %d → %d bytes\n", name, result.OriginalBytes, result.Bytes)
		}
		if err := service.Record(path, result); err != nil {
			fmt.Fprintln(r.Err, "history:", err)
		}
		if c.Output != "json" {
			fmt.Fprintln(r.Out, service.Format([]service.Result{result}, c.Output))
		}
	}
	if c.Output == "json" {
		fmt.Fprintln(r.Out, service.Format(results, "json"))
	}
	interactive := false
	if f, ok := r.Out.(*os.File); ok {
		interactive = term.IsTerminal(int(f.Fd()))
	}
	if len(results) > 0 && !noCopy && (forceCopy || c.Copy && interactive) {
		if err = clipboard.WriteText(ctx, service.Format(results, c.Output)); err != nil {
			fmt.Fprintln(r.Err, "clipboard:", err)
		}
	}
	if failed {
		return errors.New("one or more images failed to upload")
	}
	return nil
}

func (r Runner) configure(ctx context.Context, path string, c config.Config, args []string, persistedProfile string) error {
	if len(args) == 0 {
		return r.setup(path, c, persistedProfile)
	}
	switch args[0] {
	case "export":
		if len(args) != 2 {
			return errors.New("usage: picup config export FILE (or - for stdout)")
		}
		data, err := config.ExportProfiles(c, nil)
		if err != nil {
			return err
		}
		if args[1] == "-" {
			_, err = r.Out.Write(data)
			return err
		}
		absolute, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		current, _ := filepath.Abs(path)
		if absolute == current {
			return errors.New("export destination must not replace your configuration")
		}
		file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		return errors.Join(writeErr, file.Close())
	case "import":
		if len(args) != 2 {
			return errors.New("usage: picup config import FILE (or - for stdin)")
		}
		var reader io.Reader = r.In
		if args[1] != "-" {
			file, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
		}
		data, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
		if err != nil {
			return err
		}
		merged, report, err := config.ImportProfiles(c, data)
		if err != nil {
			return err
		}
		if err := saveConfig(path, merged, persistedProfile); err != nil {
			return err
		}
		fmt.Fprintf(r.Out, "Imported %d profiles. Credentials are not included; reconnect before uploading.\n", report.Added)
		return nil
	case "show":
		if err := c.ApplyEnv(); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(c.Redacted(), "", "  ")
		fmt.Fprintln(r.Out, string(b))
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: picup config set KEY VALUE (or - to read stdin)")
		}
		v := args[2]
		if v == "-" {
			b, err := io.ReadAll(io.LimitReader(r.In, 65537))
			if err != nil {
				return err
			}
			if len(b) > 65536 {
				return errors.New("setting is too long")
			}
			v = strings.TrimRight(string(b), "\r\n")
		}
		if err := c.Set(args[1], v); err != nil {
			return err
		}
		return saveConfig(path, c, persistedProfile)
	case "test":
		if err := c.ApplyEnv(); err != nil {
			return err
		}
		client, err := storage.NewWithConfigPath(ctx, c, path)
		if err != nil {
			return err
		}
		fmt.Fprintln(r.Err, "Creating and deleting a temporary .picup-test object...")
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err = client.Test(testCtx); err != nil {
			return err
		}
		fmt.Fprintln(r.Out, "Connection OK. Test object deleted.")
		return nil
	default:
		return errors.New("unknown config command; use show, set, test, path, export or import")
	}
}

func (r Runner) setup(path string, c config.Config, persistedProfile string) error {
	reader := bufio.NewReader(r.In)
	ask := func(label string, dst *string, secret bool) error {
		current := *dst
		if secret && current != "" {
			current = "saved"
		}
		fmt.Fprintf(r.Err, "%s [%s]: ", label, current)
		var line string
		var err error
		if f, ok := r.In.(*os.File); ok && secret && term.IsTerminal(int(f.Fd())) {
			var b []byte
			b, err = term.ReadPassword(int(f.Fd()))
			line = string(b)
			fmt.Fprintln(r.Err)
		} else {
			line, err = reader.ReadString('\n')
		}
		if err != nil {
			return fmt.Errorf("setup interrupted; configuration was not saved: %w", err)
		}
		if v := strings.TrimSpace(line); v != "" {
			*dst = v
		}
		return nil
	}
	for _, item := range []struct {
		label  string
		dst    *string
		secret bool
	}{{"Provider (s3/r2)", &c.Provider, false}, {"Endpoint (R2: https://ACCOUNT.r2.cloudflarestorage.com)", &c.Endpoint, false}, {"Region (R2: auto; S3: e.g. us-east-1)", &c.Region, false}, {"Bucket", &c.Bucket, false}, {"Access key", &c.AccessKey, true}, {"Secret key", &c.SecretKey, true}, {"Public image URL", &c.PublicURL, false}, {"Object path template (e.g. {year}/{month}-{day}-{md5}.{extName})", &c.PathTemplate, false}} {
		if err := ask(item.label, item.dst, item.secret); err != nil {
			return err
		}
	}
	quality := strconv.Itoa(c.Quality)
	if err := ask("Compression quality (1-100)", &quality, false); err != nil {
		return err
	}
	if err := c.Set("quality", quality); err != nil {
		return err
	}
	if err := c.ValidateStorage(); err != nil {
		return err
	}
	if err := saveConfig(path, c, persistedProfile); err != nil {
		return err
	}
	fmt.Fprintln(r.Err, "Saved:", path)
	return nil
}

func saveConfig(path string, c config.Config, persistedProfile string) error {
	// A command may edit another profile without changing the app's selection.
	if c.ActiveProfile != persistedProfile {
		if err := c.SwitchProfile(persistedProfile); err != nil {
			return err
		}
	}
	return config.Save(path, c)
}

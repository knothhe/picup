// Package imagetools processes images locally without requiring cloud storage.
package imagetools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
)

// Options are captured when a batch starts. They are independent of the active
// upload profile, credentials, and environment overrides.
type Options struct {
	Compress bool   `json:"compress"`
	Format   string `json:"format"`
	Quality  int    `json:"quality"`
	MaxWidth int    `json:"max_width"`
}

func DefaultOptions() Options {
	c := config.Default()
	return Options{Compress: c.Compress, Format: c.Format, Quality: c.Quality, MaxWidth: c.MaxWidth}
}

func (o Options) processingConfig() config.Config {
	c := config.Default()
	c.Compress, c.Format, c.Quality, c.MaxWidth = o.Compress, o.Format, o.Quality, o.MaxWidth
	return c
}

func (o Options) Validate() error { return o.processingConfig().ValidateOptions() }

// Input accepts either a file or in-memory image, such as clipboard content.
// Non-nil Data takes precedence over Path. Name is optional for file inputs.
type Input struct {
	Path, Name string
	Data       []byte
}

type Result struct {
	Path, Name, Extension, ContentType  string
	OriginalBytes, Bytes, Width, Height int
}

type Outcome struct {
	Result Result
	Err    error
}

type Event struct {
	Index             int
	Started, Finished bool
	Outcome           Outcome
}

// Batch processes inputs serially to bound decoder memory. It reports one
// outcome for every input, including canceled inputs, and retains successful
// exports if another input fails. Callbacks run on the calling goroutine.
func Batch(ctx context.Context, inputs []Input, outputDir string, options Options, onEvent func(Event)) []Outcome {
	outcomes := make([]Outcome, len(inputs))
	for i, input := range inputs {
		if err := ctx.Err(); err != nil {
			outcomes[i].Err = err
		} else {
			if onEvent != nil {
				onEvent(Event{Index: i, Started: true})
			}
			outcomes[i].Result, outcomes[i].Err = Export(ctx, input, outputDir, options)
		}
		if onEvent != nil {
			onEvent(Event{Index: i, Finished: true, Outcome: outcomes[i]})
		}
	}
	return outcomes
}

// Export saves a separate image in outputDir. Existing files and originals are
// never replaced, including when two batches export the same filename at once.
// Animated images retain their format and frames, following imageproc behavior.
func Export(ctx context.Context, input Input, outputDir string, options Options) (Result, error) {
	var result Result
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := options.Validate(); err != nil {
		return result, err
	}
	if outputDir == "" {
		return result, errors.New("choose an output folder")
	}
	dir, err := filepath.Abs(outputDir)
	if err != nil {
		return result, fmt.Errorf("output folder: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return result, fmt.Errorf("output folder: %w", err)
	}
	if !info.IsDir() {
		return result, errors.New("output folder is not a directory")
	}
	name := input.Name
	if name == "" {
		name = filepath.Base(input.Path)
	}
	data := input.Data
	if data == nil {
		data, err = imageproc.ReadFile(input.Path)
		if err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	processed, err := imageproc.Process(ctx, data, name, options.processingConfig())
	if err != nil {
		return result, err
	}
	result = Result{Extension: processed.Extension, ContentType: processed.ContentType,
		OriginalBytes: processed.OriginalBytes, Bytes: len(processed.Data), Width: processed.Width, Height: processed.Height}
	path, err := saveNewFile(ctx, dir, exportStem(name), processed.Extension, processed.Data)
	if err != nil {
		return result, err
	}
	result.Path, result.Name = path, filepath.Base(path)
	return result, nil
}

func exportStem(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "image"
	}
	// Keep the output suffix and collision number within common filename limits,
	// without splitting a UTF-8 character.
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name + "-processed"
}

func saveNewFile(ctx context.Context, dir, stem, extension string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, ".picup-export-*")
	if err != nil {
		return "", fmt.Errorf("create output image: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		chunk := data[:min(len(data), 64<<10)]
		n, err := file.Write(chunk)
		if err != nil {
			return "", fmt.Errorf("write output image: %w", err)
		}
		data = data[n:]
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("save output image: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close output image: %w", err)
	}
	for number := 1; number <= 10000; number++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := stem
		if number > 1 {
			name += fmt.Sprintf(" (%d)", number)
		}
		path := filepath.Join(dir, name+"."+extension)
		// Publish only a completely written file, refusing existing paths.
		if err := publishNewFile(file.Name(), path); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("export image: %w", err)
		}
		return path, nil
	}
	return "", errors.New("too many images share this output filename")
}

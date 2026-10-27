package gui

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"os"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/knothhe/picup/internal/imageproc"
)

const (
	uploadPreviewSize     = 96
	uploadPreviewMaxBytes = 32 << 20
)

type uploadPreviewResult struct {
	image         image.Image
	bytes         int
	width, height int
	err           error
}

// This state belongs to the UI thread. Slots also bound decoding across
// canceled batches while an old decoder finishes its current operation.
type uploadPreviewState struct {
	jobs       []*job
	generation uint64
	ctx        context.Context
	slots      chan struct{}
	pending    map[*job]uploadPreviewSource
}

type uploadPreviewSource struct {
	job  *job
	path string
	data []byte
}

// Keep a pending clipboard source available across batch restarts if an
// upload releases job.data first. These snapshots disappear after decoding.
func (s *uploadPreviewState) source(j *job) uploadPreviewSource {
	if source, ok := s.pending[j]; ok {
		return source
	}
	if s.pending == nil {
		s.pending = make(map[*job]uploadPreviewSource)
	}
	source := uploadPreviewSource{job: j, path: j.path, data: j.data}
	s.pending[j] = source
	return source
}

func readUploadPreview(path string, data []byte) uploadPreviewResult {
	return readUploadPreviewContext(context.Background(), path, data)
}

// Only the small decoded image escapes this function. Neither file contents
// nor clipboard bytes are modified, and original encoded data is not cached.
func readUploadPreviewContext(ctx context.Context, path string, data []byte) uploadPreviewResult {
	var out uploadPreviewResult
	if err := ctx.Err(); err != nil {
		out.err = err
		return out
	}
	if data == nil {
		f, err := os.Open(path)
		if err != nil {
			out.err = err
			return out
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			out.err = err
			return out
		}
		if !info.Mode().IsRegular() {
			out.err = errors.New("preview source is not a regular file")
			return out
		}
		out.bytes = int(info.Size())
		if info.Size() > uploadPreviewMaxBytes {
			out.err = errors.New("image exceeds preview byte limit")
			return out
		}
		data, err = io.ReadAll(io.LimitReader(uploadPreviewReader{ctx: ctx, reader: f}, uploadPreviewMaxBytes+1))
		if err != nil {
			out.err = err
			return out
		}
	}
	out.bytes = len(data)
	if len(data) == 0 || len(data) > uploadPreviewMaxBytes {
		out.err = errors.New("image exceeds preview byte limit or is empty")
		return out
	}
	info, _, err := imageproc.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		out.err = err
		return out
	}
	out.width, out.height = info.Width, info.Height
	if !imageproc.ValidDimensions(info.Width, info.Height) {
		out.err = errors.New("image exceeds preview pixel limit")
		return out
	}
	img, err := imageproc.Decode(data)
	if err != nil {
		out.err = err
		return out
	}
	if err := ctx.Err(); err != nil {
		out.err = err
		return out
	}
	out.width, out.height = img.Bounds().Dx(), img.Bounds().Dy()
	out.image = imaging.Fit(img, uploadPreviewSize, uploadPreviewSize, imaging.Lanczos)
	if err := ctx.Err(); err != nil {
		out.image, out.err = nil, err
	}
	return out
}

type uploadPreviewReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r uploadPreviewReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(b)
}

func sameUploadPreviewJobs(a, b []*job) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The caller passes the rows it wants to show. All file IO and decoding run
// outside rendering, and a finished preview only attaches to a surviving job.
func (a *application) loadUploadPreviews(rows []*job) {
	if a.win == nil || a.quitting {
		return
	}
	live := make(map[*job]bool, len(a.jobs))
	for _, j := range a.jobs {
		live[j] = true
	}
	for _, j := range a.imageTools.jobs {
		live[j.source] = true
	}
	requested := make([]*job, 0, len(rows))
	seen := make(map[*job]bool, len(rows))
	for _, j := range rows {
		if j != nil && live[j] && !seen[j] {
			requested = append(requested, j)
			seen[j] = true
		}
	}
	s := &a.uploadPreviews
	for j := range s.pending {
		if !live[j] || j.previewDone {
			delete(s.pending, j)
		}
	}
	if sameUploadPreviewJobs(s.jobs, requested) && (s.ctx == nil || s.ctx.Err() == nil) {
		return
	}
	if a.uploadPreviewCancel != nil {
		a.uploadPreviewCancel()
		a.uploadPreviewCancel = nil
	}
	for _, j := range s.jobs {
		j.previewLoading = false
		if !live[j] {
			// A completed clipboard upload has released its encoded source.
			// Keep its thumbnail while it remains in either page's live queue.
			j.preview = nil
			j.previewDone = false
		}
		// A just-finished clipboard upload may release job.data before decoding
		// completes. Retain its only source until it is decoded or removed.
		needsClipboardSource := live[j] && j.data == nil && j.path == "" && !j.previewDone
		if !seen[j] && !needsClipboardSource {
			delete(s.pending, j)
		}
	}
	s.generation++
	s.jobs, s.ctx = requested, nil
	if s.slots == nil {
		s.slots = make(chan struct{}, 2)
	}
	var missing []uploadPreviewSource
	for _, j := range requested {
		if !j.previewDone {
			j.previewLoading = true
			// Clipboard data is immutable after a job is created. Workers only
			// read this snapshot; the decoder never writes into its input.
			missing = append(missing, s.source(j))
		}
	}
	if len(missing) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx, a.uploadPreviewCancel = ctx, cancel
	generation, slots := s.generation, s.slots
	go func() {
		queue := make(chan uploadPreviewSource)
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				for input := range queue {
					select {
					case slots <- struct{}{}:
					case <-ctx.Done():
						return
					}
					preview := readUploadPreviewContext(ctx, input.path, input.data)
					<-slots
					if ctx.Err() != nil {
						return
					}
					mygo.RunOnMain(func() {
						if ctx.Err() != nil || a.quitting || a.uploadPreviews.generation != generation {
							return
						}
						present := false
						for _, j := range a.jobs {
							if j == input.job {
								present = true
								break
							}
						}
						j := input.job
						for _, tool := range a.imageTools.jobs {
							if tool.source == j {
								present = true
								break
							}
						}
						delete(a.uploadPreviews.pending, j)
						j.previewLoading = false
						if !present {
							j.preview, j.previewDone = nil, false
							return
						}
						j.previewDone = true
						j.sourceBytes, j.sourceWidth, j.sourceHeight = preview.bytes, preview.width, preview.height
						if preview.image != nil {
							j.preview = ui.NewBitmap(preview.image)
						}
						if a.win != nil {
							a.win.Invalidate()
						}
					})
				}
			})
		}
		for _, input := range missing {
			select {
			case queue <- input:
			case <-ctx.Done():
				close(queue)
				workers.Wait()
				return
			}
		}
		close(queue)
		workers.Wait()
	}()
}

package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/knothhe/picup/internal/imageproc"
	"github.com/knothhe/picup/internal/storage"
)

type Input struct {
	Path, Name string
	Data       []byte
}
type Outcome struct {
	Result  Result
	Err     error
	Skipped bool
}
type Event struct {
	Index             int
	Sent, Total       int64
	Started, Finished bool
	Outcome           Outcome
}

// Batch keeps results in input order even when requests finish out of order.
// Event callbacks are serialized; UI consumers must marshal them to their UI
// thread. In-memory clipboard bytes and file reads obey the same concurrency cap.
func (s Service) Batch(ctx context.Context, inputs []Input, concurrency int, event func(Event)) []Outcome {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 6 {
		concurrency = 6
	}
	results := make([]Outcome, len(inputs))
	for i := range results {
		results[i].Err = context.Canceled
	}
	var mu sync.Mutex
	emit := func(e Event) {
		if event != nil {
			mu.Lock()
			defer mu.Unlock()
			event(e)
		}
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(concurrency, len(inputs)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				input := inputs[index]
				emit(Event{Index: index, Started: true})
				data, err := input.Data, error(nil)
				if data == nil {
					data, err = imageproc.ReadFile(input.Path)
				}
				var result Result
				if err == nil {
					one, cancel := context.WithTimeout(ctx, 2*time.Minute)
					result, err = s.UploadWithProgress(one, data, input.Name, func(sent, total int64) { emit(Event{Index: index, Sent: sent, Total: total}) })
					cancel()
				}
				results[index] = Outcome{Result: result, Err: err, Skipped: errors.Is(err, storage.ErrObjectSkipped)}
				emit(Event{Index: index, Finished: true, Outcome: results[index]})
			}
		}()
	}
send:
	for i := range inputs {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	workers.Wait()
	return results
}

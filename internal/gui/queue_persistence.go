package gui

import (
	"errors"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/knothhe/picup/internal/service"
)

type queueWrite struct {
	records []service.QueueRecord
	done    chan error
}
type queuePersistence struct {
	mu      sync.Mutex
	pending []queueWrite
	wake    chan struct{}
	blocked bool
	stopped bool
}

func (a *application) uploadQueueBlocked() bool {
	return a.queuePersistence != nil && a.queuePersistence.blocked
}

// Preserve an unreadable manifest until an explicit reset. Accepting new
// sources while writes are blocked would leave them only in this process.
func (a *application) rejectBlockedUploadInput() bool {
	if !a.uploadQueueBlocked() {
		return false
	}
	a.feedbackWithAction("无法添加图片", errors.New(tr("请先在上传页重置队列记录，再添加图片。")), "打开上传页", func() { a.showPage(0) })
	return true
}

func (p *queuePersistence) enqueue(request queueWrite) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return false
	}
	p.pending = append(p.pending, request)
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return true
}

func (a *application) stopUploadQueue() {
	if a.queuePersistence == nil {
		return
	}
	p := a.queuePersistence
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.stopped = true
		close(p.wake)
	}
}

func (a *application) initUploadQueue() error {
	store := service.NewQueueStore(a.path)
	records, err := store.Load()
	a.queuePersistence = &queuePersistence{wake: make(chan struct{}, 1), blocked: err != nil}
	for _, record := range records {
		a.jobs = append(a.jobs, &job{id: record.ID, path: record.Path, name: record.Name, status: record.Status, detail: record.Detail, data: record.Data, result: record.Result})
		a.nextID = max(a.nextID, record.ID)
	}
	writer := a.queuePersistence
	go func() {
		for range writer.wake {
			for {
				writer.mu.Lock()
				if len(writer.pending) == 0 {
					writer.mu.Unlock()
					break
				}
				request := writer.pending[0]
				writer.pending[0] = queueWrite{}
				writer.pending = writer.pending[1:]
				writer.mu.Unlock()
				request.done <- store.Save(request.records)
			}
		}
	}()
	return err
}

func (a *application) queueSnapshot() []service.QueueRecord {
	records := make([]service.QueueRecord, 0, len(a.jobs))
	for _, j := range a.jobs {
		record := service.QueueRecord{ID: j.id, Path: j.path, Name: j.name, Status: j.status, Detail: j.detail, Data: j.data}
		if j.result != nil {
			result := *j.result
			record.Result = &result
		}
		records = append(records, record)
	}
	return records
}

// The worker performs disk writes in the same order as UI mutations. A barrier
// on quit ensures the last accepted snapshot is durable before process exit.
func (a *application) persistUploadQueue() {
	if a.queuePersistence == nil || a.queuePersistence.blocked || a.quitting {
		return
	}
	request := queueWrite{records: a.queueSnapshot(), done: make(chan error, 1)}
	if !a.queuePersistence.enqueue(request) {
		return
	}
	go func() {
		if err := <-request.done; err != nil {
			mygo.RunOnMain(func() {
				if !a.quitting {
					a.feedback("保存上传队列失败，当前任务仍保留", err)
				}
			})
		}
	}()
}

func (a *application) flushUploadQueue() error {
	if a.queuePersistence == nil {
		return nil
	}
	if a.uploadQueueBlocked() {
		if len(a.jobs) > 0 {
			return errors.New(tr("队列记录无法读取，当前任务尚未保存。请先处理上传页的恢复提示。"))
		}
		return nil
	}
	request := queueWrite{records: a.queueSnapshot(), done: make(chan error, 1)}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	if !a.queuePersistence.enqueue(request) {
		return errors.New("upload queue writer has stopped")
	}
	select {
	case err := <-request.done:
		return err
	case <-timer.C:
		return errors.New("upload queue save timed out")
	}
}

func (a *application) clearUploadQueue() {
	if a.busy {
		return
	}
	a.jobs = nil
	if a.queuePersistence != nil {
		a.queuePersistence.blocked = false
	}
	a.persistUploadQueue()
}

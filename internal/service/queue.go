package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/knothhe/picup/internal/config"
	"github.com/knothhe/picup/internal/imageproc"
)

const MaxQueueClipboardBytes = 256 << 20

// QueueRecord contains no cloud credentials. Clipboard images are separate
// private files; file inputs keep their original absolute path without copying
// or modifying the source. Restoring a queue never starts an upload.
type QueueRecord struct {
	ID     int     `json:"id"`
	Path   string  `json:"path,omitempty"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Detail string  `json:"detail,omitempty"`
	Result *Result `json:"result,omitempty"`
	Blob   string  `json:"blob,omitempty"`
	Data   []byte  `json:"-"`
}
type queueDocument struct {
	Version int           `json:"version"`
	Jobs    []QueueRecord `json:"jobs"`
}
type QueueStore struct {
	directory string
	// A failed clipboard source remains tied to its row through later saves,
	// even when the GUI has no bytes to snapshot. Explicitly removing that row
	// releases the blob; a failure in another row must never garbage-collect it.
	damaged map[int]string
}

var queueBlobName = regexp.MustCompile(`^[a-f0-9]{64}\.bin$`)

func NewQueueStore(configPath string) *QueueStore {
	return &QueueStore{directory: filepath.Join(filepath.Dir(configPath), "upload-queue-v1")}
}

func (s *QueueStore) Load() ([]QueueRecord, error) { return s.load(MaxQueueClipboardBytes) }

func (s *QueueStore) load(maxBytes int64) ([]QueueRecord, error) {
	f, err := os.Open(filepath.Join(s.directory, "queue.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 4<<20 {
		return nil, errors.New("upload queue exceeds 4 MiB")
	}
	var doc queueDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Version != 1 || len(doc.Jobs) > 1000 {
		return nil, errors.New("unsupported upload queue")
	}
	seen := map[int]bool{}
	for _, j := range doc.Jobs {
		if j.ID <= 0 || seen[j.ID] || j.Name == "" || (j.Path != "" && !filepath.IsAbs(j.Path)) {
			return nil, errors.New("invalid upload queue entry")
		}
		seen[j.ID] = true
	}
	type blobResult struct {
		data []byte
		err  error
	}
	cache := map[string]blobResult{}
	damaged := map[int]string{}
	var used int64
	for i := range doc.Jobs {
		j := &doc.Jobs[i]
		if j.Result != nil {
			continue
		}
		var sourceErr error
		if j.Blob != "" {
			if !queueBlobName.MatchString(j.Blob) {
				sourceErr = errors.New("invalid clipboard image reference")
				j.Blob = "" // Never retain an untrusted filesystem path.
			} else {
				cached, ok := cache[j.Blob]
				if !ok {
					cached.data, cached.err = readQueueBlob(filepath.Join(s.directory, j.Blob), maxBytes-used)
					used += int64(len(cached.data))
					if cached.err == nil {
						hash := sha256.Sum256(cached.data)
						if hex.EncodeToString(hash[:])+".bin" != j.Blob {
							cached.err = errors.New("clipboard image checksum mismatch")
						}
					}
					if cached.err != nil {
						cached.data = nil
					}
					cache[j.Blob] = cached
				}
				j.Data, sourceErr = cached.data, cached.err
				if sourceErr != nil {
					damaged[j.ID] = j.Blob
				}
			}
		}
		if sourceErr == nil && j.Path == "" && j.Data == nil {
			sourceErr = errors.New("queue entry has no source")
		}
		if sourceErr != nil {
			j.Status = "上传失败"
			j.Detail = fmt.Sprintf("恢复图片失败，请重新添加或移除此项：%v", sourceErr)
		} else if j.Status != "上传失败" && j.Status != "已跳过" {
			j.Status = "等待上传"
			j.Detail = ""
		}
	}
	s.damaged = damaged
	return doc.Jobs, nil
}

func readQueueBlob(path string, budget int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("clipboard image is not a regular private file")
	}
	if info.Size() <= 0 || info.Size() > imageproc.MaxBytes {
		return nil, errors.New("clipboard image must be between 1 byte and 100 MiB")
	}
	if info.Size() > budget {
		return nil, errors.New("restored clipboard images exceed 256 MiB; remove other clipboard items or re-add this image")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Allocate exactly the admitted size: io.ReadAll's spare capacity could
	// otherwise retain substantially more memory than the configured budget.
	data := make([]byte, int(info.Size()))
	if _, err := io.ReadFull(f, data); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, err := f.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, errors.New("clipboard image changed while restoring")
	}
	return data, nil
}

func (s *QueueStore) Save(records []QueueRecord) error {
	return s.save(records, MaxQueueClipboardBytes)
}

func (s *QueueStore) save(records []QueueRecord, maxBytes int64) error {
	if len(records) > 1000 {
		return errors.New("upload queue supports at most 1000 images")
	}
	doc := queueDocument{Version: 1, Jobs: append([]QueueRecord(nil), records...)}
	keep := map[string]bool{}
	blobs := map[string][]byte{}
	damaged := map[int]string{}
	seen := map[int]bool{}
	var total int64
	for i := range doc.Jobs {
		j := &doc.Jobs[i]
		if j.ID <= 0 || seen[j.ID] || j.Name == "" || j.Path != "" && !filepath.IsAbs(j.Path) {
			return errors.New("invalid upload queue entry")
		}
		seen[j.ID] = true
		j.Blob = ""
		if j.Result != nil {
			continue
		}
		if j.Data != nil {
			if len(j.Data) == 0 || len(j.Data) > imageproc.MaxBytes {
				return errors.New("clipboard image must be between 1 byte and 100 MiB")
			}
			hash := sha256.Sum256(j.Data)
			j.Blob = hex.EncodeToString(hash[:]) + ".bin"
			if _, exists := blobs[j.Blob]; !exists {
				total += int64(len(j.Data))
				if total > maxBytes {
					return errors.New("queued clipboard images exceed 256 MiB")
				}
				blobs[j.Blob] = j.Data
			}
			keep[j.Blob] = true
		} else if previous := s.damaged[j.ID]; previous != "" {
			j.Blob = previous
			keep[previous], damaged[j.ID] = true, previous
		} else if j.Path == "" && j.Status != "上传失败" && j.Status != "已跳过" {
			return errors.New("queue entry has no source")
		}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(b) > 4<<20 {
		return errors.New("upload queue exceeds 4 MiB")
	}
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return err
	}
	badBlobs := map[string]bool{}
	for _, blob := range s.damaged {
		badBlobs[blob] = true
	}
	for name, data := range blobs {
		blob := filepath.Join(s.directory, name)
		info, err := os.Lstat(blob)
		if os.IsNotExist(err) || err == nil && (!info.Mode().IsRegular() || info.Size() != int64(len(data)) || badBlobs[name]) {
			if err := config.AtomicWrite(blob, data, 0600); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if err := config.AtomicWrite(filepath.Join(s.directory, "queue.json"), b, 0600); err != nil {
		return err
	}
	s.damaged = damaged
	entries, _ := os.ReadDir(s.directory)
	for _, entry := range entries {
		if queueBlobName.MatchString(entry.Name()) && !keep[entry.Name()] {
			_ = os.Remove(filepath.Join(s.directory, entry.Name()))
		}
	}
	return nil
}

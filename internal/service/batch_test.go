package service

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/knothhe/picup/internal/config"
)

func TestBatchLimitsConcurrencyKeepsInputOrderAndContinuesAfterFailure(t *testing.T) {
	var active, maximum atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(404)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		w.WriteHeader(200)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Endpoint = server.URL
	cfg.Bucket = "photos"
	cfg.PublicURL = "https://images.example.com"
	cfg.AccessKey = "key"
	cfg.SecretKey = "secret"
	cfg.Compress = false
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	inputs := []Input{{Name: "first.png", Data: b.Bytes()}, {Name: "bad.png", Data: []byte("bad")}, {Name: "third.png", Data: b.Bytes()}}
	done := make(chan []Outcome, 1)
	go func() { done <- (Service{Config: cfg}).Batch(context.Background(), inputs, 2, nil) }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("two requests did not overlap")
		}
	}
	close(release)
	results := <-done
	if maximum.Load() != 2 || results[0].Result.Name != "first.png" || results[1].Err == nil || results[2].Result.Name != "third.png" {
		t.Fatal(maximum.Load(), results)
	}
}

func TestCanceledBatchDoesNotReadOrUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := (Service{}).Batch(ctx, []Input{{Path: "missing", Name: "a.png"}, {Path: "missing", Name: "b.png"}}, 6, nil)
	for _, result := range results {
		if result.Err != context.Canceled {
			t.Fatal(result.Err)
		}
	}
}

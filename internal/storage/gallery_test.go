package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/knothhe/picup/internal/config"
)

func galleryTestClient(t *testing.T, provider string, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("unsigned cloud request")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	c := config.Default()
	c.Provider = provider
	c.Endpoint = server.URL
	c.Bucket = "photos"
	c.AccessKey = "test"
	c.SecretKey = "secret"
	c.PublicURL = "https://img.example.com/base"
	client, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func s3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>test failure</Message></Error>", code)
}
func TestGalleryListEveryPageAndImageURLs(t *testing.T) {
	var tokens []string
	client := galleryTestClient(t, "r2", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/photos" || r.URL.Query().Get("prefix") != "images/" || r.URL.Query().Get("list-type") != "2" {
			t.Error(r.URL)
		}
		token := r.URL.Query().Get("continuation-token")
		tokens = append(tokens, token)
		if token == "" {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>a+/=</NextContinuationToken><Contents><Key>images/海 a+.PNG</Key><Size>123</Size><ETag>&quot;one&quot;</ETag><LastModified>2026-10-20T03:00:00Z</LastModified></Contents><Contents><Key>images/readme.txt</Key></Contents><Contents><Key>images/folder/</Key></Contents></ListBucketResult>`)
		} else {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>images/b.webp</Key><Size>456</Size></Contents></ListBucketResult>`)
		}
	})
	rows, err := client.List(context.Background(), "images/")
	if err != nil || len(rows) != 2 || !reflect.DeepEqual(tokens, []string{"", "a+/="}) {
		t.Fatal(rows, tokens, err)
	}
	if rows[0].Size != 123 || rows[0].Modified.IsZero() || rows[0].ETag != `"one"` || rows[0].URL != "https://img.example.com/base/images/%E6%B5%B7%20a+.PNG" {
		t.Fatal(rows[0])
	}
}
func TestGalleryListFailureNeverReturnsPartialSuccess(t *testing.T) {
	for _, mode := range []string{"denied", "missing-token", "repeat-token"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := galleryTestClient(t, "s3", func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 2 && mode == "denied" {
					s3Error(w, 403, "AccessDenied")
					return
				}
				token := "next"
				if mode == "missing-token" {
					token = ""
				}
				fmt.Fprintf(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>%s</NextContinuationToken><Contents><Key>a.png</Key></Contents></ListBucketResult>`, token)
			})
			rows, err := client.List(context.Background(), "")
			if err == nil || rows != nil || calls > 2 {
				t.Fatal(rows, calls, err)
			}
		})
	}
}
func TestGallerySignedReadAndDeleteEscapedKey(t *testing.T) {
	var methods []string
	key := "images/海 a+#?.png"
	client := galleryTestClient(t, "r2", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.Path != "/photos/"+key {
			t.Error(r.URL.Path)
		}
		if r.Method == "GET" {
			fmt.Fprint(w, "original bytes")
		} else {
			w.WriteHeader(204)
		}
	})
	var b bytes.Buffer
	if err := client.Read(context.Background(), key, &b); err != nil {
		t.Fatal(err)
	}
	if b.String() != "original bytes" {
		t.Fatal(b.String())
	}
	if err := client.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, []string{"GET", "DELETE"}) {
		t.Fatal(methods)
	}
}
func TestRenamePreservesSourceOnEveryFailure(t *testing.T) {
	for _, provider := range []string{"s3", "r2"} {
		for _, scenario := range []string{"success", "exists", "head-denied", "copy-failed", "copy-conflict", "delete-failed"} {
			t.Run(provider+"/"+scenario, func(t *testing.T) {
				oldKey, newKey := "images/海 a+#?.png", "new/海 b+#?.png"
				var methods []string
				client := galleryTestClient(t, provider, func(w http.ResponseWriter, r *http.Request) {
					methods = append(methods, r.Method)
					switch r.Method {
					case "HEAD":
						if r.URL.Path != "/photos/"+newKey {
							t.Error(r.URL)
						}
						switch scenario {
						case "exists":
							w.WriteHeader(200)
						case "head-denied":
							w.WriteHeader(403)
						default:
							w.WriteHeader(404)
						}
					case "PUT":
						source, err := url.PathUnescape(r.Header.Get("X-Amz-Copy-Source"))
						if err != nil || source != "photos/"+oldKey {
							t.Error(source, err)
						}
						if r.Header.Get("X-Amz-Copy-Source-If-Match") != `"original"` {
							t.Error("missing source condition")
						}
						condition := "If-None-Match"
						if provider == "r2" {
							condition = "Cf-Copy-Destination-If-None-Match"
						}
						if r.Header.Get(condition) != "*" {
							t.Error("missing destination condition", r.Header)
						}
						if scenario == "copy-failed" {
							s3Error(w, 403, "AccessDenied")
							return
						}
						if scenario == "copy-conflict" {
							s3Error(w, 412, "PreconditionFailed")
							return
						}
						fmt.Fprint(w, `<CopyObjectResult><ETag>"copied"</ETag><LastModified>2026-10-20T03:00:00Z</LastModified></CopyObjectResult>`)
					case "DELETE":
						if r.URL.Path != "/photos/"+oldKey || r.Header.Get("If-Match") != `"original"` {
							t.Error("wrong source deletion", r.URL, r.Header)
						}
						if scenario == "delete-failed" {
							s3Error(w, 403, "AccessDenied")
							return
						}
						w.WriteHeader(204)
					}
				})
				renamed, err := client.Rename(context.Background(), Object{Key: oldKey, ETag: `"original"`}, newKey)
				want := []string{"HEAD", "PUT", "DELETE"}
				switch scenario {
				case "exists", "head-denied":
					want = []string{"HEAD"}
				case "copy-failed", "copy-conflict":
					want = []string{"HEAD", "PUT"}
				}
				if !reflect.DeepEqual(methods, want) {
					t.Fatal(methods, want)
				}
				if err == nil && (renamed.ETag != `"copied"` || renamed.Key != newKey || renamed.URL != client.URL(newKey)) {
					t.Fatal("copy metadata lost", renamed)
				}
				if (err == nil) != (scenario == "success") {
					t.Fatal(err)
				}
				var partial *RenamePartialError
				if errors.As(err, &partial) != (scenario == "delete-failed") {
					t.Fatal(err)
				}
				if partial != nil && (partial.OldKey != oldKey || partial.NewKey != newKey) {
					t.Fatal(partial)
				}
			})
		}
	}
}
func TestRenameValidationDoesNotSendRequests(t *testing.T) {
	client := galleryTestClient(t, "r2", func(w http.ResponseWriter, r *http.Request) { t.Fatal("validation sent request") })
	for _, key := range []string{"", "a.png", "/b.png", "../b.png", "a//b.png", "a/./b.png", "a\\b.png", "a\n.png", "b.txt", strings.Repeat("a", 1024) + ".png"} {
		if _, err := client.Rename(context.Background(), Object{Key: "a.png"}, key); err == nil {
			t.Error(key)
		}
	}
}
func TestReadReturnsWriterFailure(t *testing.T) {
	client := galleryTestClient(t, "s3", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "bytes") })
	err := client.Read(context.Background(), "a.png", failingWriter{})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

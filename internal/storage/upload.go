package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Progress measures bytes read by the HTTP transport. Retries restart at zero;
// callers may retain the high-water mark but success is only reported after S3
// acknowledges the request.
type Progress func(sent, total int64)

var ErrObjectSkipped = errors.New("object already exists; skipped")

type progressContextKey struct{}
type progressTransport struct{ base http.RoundTripper }

func (t progressTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if callback, ok := request.Context().Value(progressContextKey{}).(Progress); ok && request.Method == http.MethodPut && request.Body != nil {
		// Only wrap the final HTTP body: SDK checksum/signing passes must not
		// report bytes as already transmitted.
		request = request.Clone(request.Context())
		request.Body = &progressBody{ReadCloser: request.Body, progress: callback, total: request.ContentLength}
		callback(0, request.ContentLength)
	}
	return t.base.RoundTrip(request)
}

type progressBody struct {
	io.ReadCloser
	progress    Progress
	total, sent int64
	last        time.Time
}

func (r *progressBody) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.sent += int64(n)
	if r.progress != nil && (time.Since(r.last) >= 80*time.Millisecond || err == io.EOF || r.sent == r.total) {
		r.last = time.Now()
		r.progress(r.sent, r.total)
	}
	return n, err
}

func statusCode(err error, code int) bool {
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == code
}

func missingObject(err error) bool {
	var api smithy.APIError
	return statusCode(err, 404) || errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey")
}

func (c *Client) exists(ctx context.Context, key string) (bool, error) {
	_, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key)})
	if missingObject(err) {
		return false, nil
	}
	return err == nil, err
}

// PutWithPolicy uses conditional creation for every non-overwriting policy, so
// two concurrent uploads cannot silently replace each other's content. Reuse is
// by object key, not a claim that arbitrary existing content has the same hash.
func (c *Client) PutWithPolicy(ctx context.Context, key string, data []byte, contentType, policy string, progress Progress) (actualKey string, reused bool, err error) {
	if policy == "" {
		policy = "overwrite"
	}
	if policy != "reuse" && policy != "overwrite" && policy != "rename" && policy != "skip" {
		return "", false, fmt.Errorf("unknown collision policy %q", policy)
	}
	original := key
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		if policy != "overwrite" {
			exists, err := c.exists(ctx, key)
			if err != nil {
				return "", false, fmt.Errorf("check existing object: %w", err)
			}
			if exists {
				switch policy {
				case "reuse":
					return key, true, nil
				case "skip":
					return "", false, fmt.Errorf("%w: %s", ErrObjectSkipped, key)
				case "rename":
					key, err = uniqueObjectKey(original)
					if err != nil {
						return "", false, err
					}
					continue
				}
			}
		}
		input := &s3.PutObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String(contentType), CacheControl: aws.String("public, max-age=31536000, immutable")}
		if policy != "overwrite" {
			input.IfNoneMatch = aws.String("*")
		}
		if progress != nil {
			progress(0, int64(len(data)))
		}
		requestContext := ctx
		if progress != nil {
			requestContext = context.WithValue(ctx, progressContextKey{}, progress)
		}
		_, err := c.api.PutObject(requestContext, input)
		if err == nil {
			if progress != nil {
				progress(int64(len(data)), int64(len(data)))
			}
			return key, false, nil
		}
		if policy == "overwrite" || (!statusCode(err, 412) && !statusCode(err, 409)) {
			return "", false, err
		}
		// Recheck after a conditional conflict. A 409 does not prove existence.
	}
	return "", false, errors.New("object changed repeatedly during upload; retry")
}

func uniqueObjectKey(key string) (string, error) {
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	ext := path.Ext(key)
	suffix := "-" + hex.EncodeToString(nonce[:]) + ext
	directory, base := path.Split(strings.TrimSuffix(key, ext))
	budget := 1024 - len(directory) - len(suffix)
	if budget < 1 {
		return "", errors.New("object directory is too long to generate a unique name")
	}
	if len(base) > budget {
		base = base[:budget]
		for !utf8.ValidString(base) {
			base = base[:len(base)-1]
		}
	}
	if base == "" {
		return "", errors.New("object name is too long to generate a unique name")
	}
	return directory + base + suffix, nil
}

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Object describes a cloud image, independently of local upload history.
type Object struct {
	Key      string
	URL      string
	Size     int64
	Modified time.Time
	ETag     string
}

func IsImage(key string) bool {
	switch strings.ToLower(path.Ext(key)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif", ".heic", ".heif", ".tif", ".tiff", ".bmp", ".svg":
		return true
	}
	return false
}

// ListPage fetches one bounded service page. The limit counts cloud objects,
// including non-image files, so an empty image page can still have a next token.
// Prefix and continuation tokens are opaque S3 values and are never URL joined.
func (c *Client) ListPage(ctx context.Context, prefix, token string, limit int) ([]Object, string, error) {
	if limit < 1 || limit > 1000 {
		return nil, "", errors.New("list page limit must be between 1 and 1000")
	}
	page, err := c.api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(c.config.Bucket), Prefix: optional(prefix), ContinuationToken: optional(token), MaxKeys: aws.Int32(int32(limit))})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if aws.ToBool(page.IsTruncated) {
		next = aws.ToString(page.NextContinuationToken)
		if next == "" || next == token {
			return nil, "", errors.New("存储服务返回了无效的分页标记，请重试")
		}
	}
	images := make([]Object, 0, len(page.Contents))
	for _, item := range page.Contents {
		key := aws.ToString(item.Key)
		if IsImage(key) {
			images = append(images, Object{Key: key, URL: c.URL(key), Size: aws.ToInt64(item.Size), Modified: aws.ToTime(item.LastModified), ETag: aws.ToString(item.ETag)})
		}
	}
	return images, next, nil
}

// List retains the complete-enumeration API for CLI and existing callers. A
// failed or malformed page never becomes a successful partial gallery.
func (c *Client) List(ctx context.Context, prefix string) ([]Object, error) {
	var images []Object
	token := ""
	seen := map[string]bool{}
	for {
		page, next, err := c.ListPage(ctx, prefix, token, 1000)
		if err != nil {
			return nil, err
		}
		images = append(images, page...)
		if next == "" {
			return images, nil
		}
		if seen[next] {
			return nil, errors.New("存储服务返回了无效的分页标记，请重试")
		}
		seen[next] = true
		token = next
	}
}

func (c *Client) Read(ctx context.Context, key string, dst io.Writer) error {
	res, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, err = io.Copy(dst, res.Body)
	return err
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key)})
	return err
}

type RenamePartialError struct {
	OldKey, NewKey string
	Err            error
}

func (e *RenamePartialError) Error() string {
	return fmt.Sprintf("已复制到 %s，但未能删除 %s。两份图片均已保留，请刷新后检查：%v", e.NewKey, e.OldKey, e.Err)
}
func (e *RenamePartialError) Unwrap() error { return e.Err }

func ValidateImageKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 1024 || !utf8.ValidString(key) || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\\x00\r\n") || !IsImage(key) {
		return errors.New("请填写有效的图片路径，保留图片扩展名，最长 1024 字节")
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("图片路径不能包含空目录、. 或 ..")
		}
	}
	return nil
}

// Rename preserves object metadata, refuses to overwrite a destination, and only
// removes the source after a confirmed copy. R2 uses its destination condition
// extension; other S3 services use the standard conditional header.
func (c *Client) Rename(ctx context.Context, old Object, newKey string) (Object, error) {
	if err := ValidateImageKey(newKey); err != nil {
		return Object{}, err
	}
	if old.Key == newKey {
		return Object{}, errors.New("新路径与当前路径相同")
	}
	_, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(newKey)})
	if err == nil {
		return Object{}, errors.New("目标路径已存在，请更换名称后重试")
	}
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	if !(errors.As(err, &response) && response.HTTPStatusCode() == 404) && !(errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey")) {
		return Object{}, fmt.Errorf("检查目标路径失败：%w", err)
	}
	source := (&url.URL{Path: c.config.Bucket + "/" + old.Key}).EscapedPath()
	input := &s3.CopyObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(newKey), CopySource: aws.String(source), CopySourceIfMatch: optional(old.ETag)}
	var options []func(*s3.Options)
	if c.config.Provider == "r2" {
		options = append(options, func(o *s3.Options) {
			o.APIOptions = append(o.APIOptions, smithyhttp.AddHeaderValue("cf-copy-destination-if-none-match", "*"))
		})
	} else {
		input.IfNoneMatch = aws.String("*")
	}
	copied, err := c.api.CopyObject(ctx, input, options...)
	if err != nil {
		return Object{}, fmt.Errorf("复制到新路径失败，原图已保留：%w", err)
	}
	// A copy may have a different ETag, for example when the source was uploaded
	// in multiple parts. Never reuse its old ETag for a later rename.
	renamed := Object{Key: newKey, URL: c.URL(newKey), Size: old.Size, Modified: time.Now()}
	if copied.CopyObjectResult != nil {
		renamed.ETag = aws.ToString(copied.CopyObjectResult.ETag)
		if copied.CopyObjectResult.LastModified != nil {
			renamed.Modified = *copied.CopyObjectResult.LastModified
		}
	}
	// The condition prevents deleting an object that changed while it was copied.
	_, err = c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(old.Key), IfMatch: optional(old.ETag)})
	if err != nil {
		return renamed, &RenamePartialError{OldKey: old.Key, NewKey: newKey, Err: err}
	}
	return renamed, nil
}

package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/knothhe/picup/internal/config"
)

type Client struct {
	api    *s3.Client
	config config.Config
}

func New(c config.Config) (*Client, error) {
	if err := c.ValidateStorage(); err != nil {
		return nil, err
	}
	api := s3.New(s3.Options{Region: c.Region, Credentials: credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, c.SessionToken), UsePathStyle: c.PathStyle, HTTPClient: &http.Client{Timeout: 90 * time.Second}, BaseEndpoint: optional(c.Endpoint), RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
	return &Client{api, c}, nil
}
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func (c *Client) Put(ctx context.Context, key string, b []byte, contentType string) error {
	_, err := c.api.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key), Body: bytes.NewReader(b), ContentType: aws.String(contentType), CacheControl: aws.String("public, max-age=31536000, immutable")})
	return err
}
func (c *Client) URL(key string) string {
	u, _ := url.Parse(c.config.PublicURL)
	u.Path = strings.TrimRight(u.Path, "/") + "/" + key
	u.RawPath = ""
	return u.String()
}
func (c *Client) Test(ctx context.Context) error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	payload := []byte("picup connection test " + hex.EncodeToString(nonce[:]))
	example, err := c.config.ObjectKey(time.Now(), fmt.Sprintf("%x", md5.Sum(payload)), fmt.Sprintf("%x", sha256.Sum256(payload)), "txt")
	if err != nil {
		return err
	}
	// Probe the template's directory with a unique name, never its actual filename.
	directory := ""
	if slash := strings.LastIndexByte(example, '/'); slash >= 0 {
		directory = example[:slash+1]
	}
	key := directory + ".picup-test-" + hex.EncodeToString(nonce[:])
	if err := c.Put(ctx, key, payload, "text/plain"); err != nil {
		return fmt.Errorf("test upload failed: %w", err)
	}
	_, err = c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("upload succeeded but cleanup failed for %s: %w", key, err)
	}
	return nil
}

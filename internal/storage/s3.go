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
	"github.com/knothhe/picup/internal/r2oauth"
)

type Client struct {
	api    *s3.Client
	config config.Config
}

func New(c config.Config) (*Client, error) {
	path := ""
	if c.R2Connection != "" || c.CredentialRef != "" {
		var err error
		path, err = config.Path()
		if err != nil {
			return nil, err
		}
	}
	return NewWithConfigPath(context.Background(), c, path)
}

// NewWithConfigPath binds OAuth credentials to the same data directory as the
// selected profile. S3 retrieves credentials lazily on the operation's worker.
func NewWithConfigPath(_ context.Context, c config.Config, path string) (*Client, error) {
	if err := c.ValidateStorage(); err != nil {
		return nil, err
	}
	var provider aws.CredentialsProvider = credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, c.SessionToken)
	if c.R2Connection != "" {
		if path == "" {
			return nil, fmt.Errorf("configuration path is required for Cloudflare OAuth")
		}
		manager := r2oauth.New(path)

		provider = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			key, secret, err := manager.Resolve(ctx, c.R2Connection, c.Endpoint, c.Bucket)
			if err != nil {
				return aws.Credentials{}, err
			}
			return aws.Credentials{AccessKeyID: key, SecretAccessKey: secret, Source: "Picup Cloudflare OAuth"}, nil
		})
	} else if c.CredentialRef != "" {
		if path == "" {
			return nil, fmt.Errorf("configuration path is required for saved credentials")
		}
		manager := r2oauth.NewManual(path)
		provider = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			stored, err := manager.Resolve(ctx, c.CredentialRef, c.Endpoint, c.Bucket)
			if err != nil {
				return aws.Credentials{}, err
			}
			return aws.Credentials{AccessKeyID: stored.AccessKey, SecretAccessKey: stored.SecretKey, SessionToken: stored.SessionToken, Source: "Picup Keychain"}, nil
		})
	}
	api := s3.New(s3.Options{Region: c.Region, Credentials: provider, UsePathStyle: c.PathStyle, HTTPClient: &http.Client{Timeout: 90 * time.Second, Transport: progressTransport{base: http.DefaultTransport}}, BaseEndpoint: optional(c.Endpoint), RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired})
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

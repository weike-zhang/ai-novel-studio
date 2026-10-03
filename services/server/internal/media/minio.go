package media

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client uploads media to a MinIO bucket and returns a public-facing URL.
type Client struct {
	client   *minio.Client
	bucket   string
	publicURL string
}

type Config struct {
	Endpoint  string // host:port reachable by this process
	PublicURL string // base URL served to browsers (usually the same endpoint)
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

func Open(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("media configuration is incomplete")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	publicURL := strings.TrimRight(cfg.PublicURL, "/")
	if publicURL == "" {
		publicURL = strings.TrimRight(cfg.Endpoint, "/")
	}
	return &Client{client: client, bucket: cfg.Bucket, publicURL: publicURL}, nil
}

// EnsureBucket creates the bucket if it does not exist.
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.client.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}

// Upload stores content under key and returns the public URL path.
func (c *Client) Upload(ctx context.Context, key string, content []byte, contentType string) (string, error) {
	reader := bytes.NewReader(content)
	_, err := c.client.PutObject(ctx, c.bucket, key, reader, int64(len(content)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("upload media: %w", err)
	}
	return c.PublicPath(key), nil
}

// PublicPath returns the URL browsers can use to reach an object.
func (c *Client) PublicPath(key string) string {
	return c.publicURL + "/" + c.bucket + "/" + key
}

package blobstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Store is a minimal blobstore interface used by import jobs
type Store interface {
	EnsureBucket(ctx context.Context, bucket string) error
	Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error
	Download(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	GetPresignedURL(ctx context.Context, bucket, key string, expires time.Duration) (string, error)
}

// MinioStore is a simple MinIO/S3-compatible implementation
type MinioStore struct {
	Client *minio.Client
	UseSSL bool
	Region string
}

func NewMinioStore(endpoint, accessKey, secretKey string, useSSL bool, region string) (*MinioStore, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}
	return &MinioStore{Client: client, UseSSL: useSSL, Region: region}, nil
}

func (m *MinioStore) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := m.Client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		return m.Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: m.Region})
	}
	return nil
}

func (m *MinioStore) Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error {
	_, err := m.Client.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (m *MinioStore) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	obj, err := m.Client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// Ping by stat
	if _, err := obj.Stat(); err != nil {
		return nil, err
	}
	return obj, nil
}

func (m *MinioStore) GetPresignedURL(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	reqParams := make(url.Values)
	u, err := m.Client.PresignedGetObject(ctx, bucket, key, expires, reqParams)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

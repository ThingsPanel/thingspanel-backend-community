package fileassets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"encore.dev/storage/objects"
)

const maxSignedDownloadURLTTL = 7 * 24 * time.Hour

// ObjectStore is the small object-storage surface used by the file asset owner.
// Keys are generated internally and are never user-provided filenames.
type ObjectStore interface {
	Put(ctx context.Context, key string, contents []byte, contentType string) error
	SignedDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
}

// BucketObjectAPI is the least-privilege Encore bucket reference required by
// this adapter. The app declares the Bucket and creates this reference with
// objects.BucketRef before passing it to NewBucketStore.
type BucketObjectAPI interface {
	objects.Uploader
	objects.SignedDownloader
	objects.Remover
}

// BucketStore adapts Encore's standard object bucket to the file asset owner.
// The service resource declaration and bucket configuration belong to the app.
type BucketStore struct {
	bucket BucketObjectAPI
}

func NewBucketStore(bucket BucketObjectAPI) *BucketStore {
	return &BucketStore{bucket: bucket}
}

func (s *BucketStore) Put(ctx context.Context, key string, contents []byte, contentType string) error {
	if s == nil || s.bucket == nil {
		return errors.New("file asset object bucket is unavailable")
	}
	if key == "" {
		return errors.New("file asset object key is empty")
	}

	writer := s.bucket.Upload(ctx, key, objects.WithUploadAttrs(objects.UploadAttrs{ContentType: contentType}))
	if len(contents) > 0 {
		written, err := writer.Write(contents)
		if err != nil {
			writer.Abort(err)
			return err
		}
		if written != len(contents) {
			err = io.ErrShortWrite
			writer.Abort(err)
			return err
		}
	}
	return writer.Close()
}

func (s *BucketStore) SignedDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if s == nil || s.bucket == nil {
		return "", errors.New("file asset object bucket is unavailable")
	}
	if key == "" {
		return "", errors.New("file asset object key is empty")
	}
	if ttl < time.Second || ttl > maxSignedDownloadURLTTL {
		return "", fmt.Errorf("signed download URL TTL must be between 1s and %s", maxSignedDownloadURLTTL)
	}

	signed, err := s.bucket.SignedDownloadURL(ctx, key, objects.WithTTL(ttl))
	if err != nil {
		return "", err
	}
	if signed == nil || signed.URL == "" {
		return "", errors.New("file asset object store returned an empty signed URL")
	}
	return signed.URL, nil
}

func (s *BucketStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.bucket == nil {
		return errors.New("file asset object bucket is unavailable")
	}
	if key == "" {
		return errors.New("file asset object key is empty")
	}
	return s.bucket.Remove(ctx, key)
}

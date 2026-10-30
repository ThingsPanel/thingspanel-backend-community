package fileassets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"testing"
	"time"

	"encore.dev/storage/objects"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This uses Encore's actual local object provider, not the workflow fake.
// It proves SDK I/O and the signed-URL download path; production S3 and its
// signature enforcement remain a separate self-hosted acceptance gate.
func TestFileAssetEncoreObjectProviderRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store := NewBucketStore(imageBucketAPI)
	key := "sdk-acceptance/" + uuid.NewString()
	contents := []byte("\x89PNG\r\n\x1a\nencore-object-io")
	require.NoError(t, store.Put(ctx, key, contents, "image/png"))
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })
	signed, err := store.SignedDownloadURL(ctx, key, time.Minute)
	if err != nil {
		t.Fatal("Encore object provider could not sign a download capability")
	}
	if !validDownloadURL(signed) {
		t.Fatal("Encore object provider returned an invalid capability URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
	require.NoError(t, err)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("Encore object provider capability download failed")
	}
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	downloaded, err := io.ReadAll(io.LimitReader(response.Body, int64(len(contents)+1)))
	require.NoError(t, err)
	if !bytes.Equal(contents, downloaded) || sha256.Sum256(contents) != sha256.Sum256(downloaded) {
		t.Fatal("object provider round trip content checksum mismatch")
	}
	require.NoError(t, store.Delete(ctx, key))
	reader := imageBucket.Download(ctx, key)
	defer reader.Close()
	_, err = io.ReadAll(reader)
	require.ErrorIs(t, err, objects.ErrObjectNotFound)
}

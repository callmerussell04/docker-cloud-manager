package objectstorage_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/objectstorage"
)

func TestReaderAtReportsEndOfFileOnShortRead(t *testing.T) {
	const content = "0123456789"
	storage := newFakeS3Storage(t, "bucket", "object", content)
	reader, size, err := storage.NewReaderAt(t.Context(), "object")
	require.NoError(t, err)
	require.EqualValues(t, len(content), size)

	for _, tc := range []struct {
		name    string
		off     int64
		bufSize int
		want    string
		wantErr error
	}{
		{name: "inside the object", off: 2, bufSize: 4, want: "2345"},
		{name: "ending exactly at the end", off: 6, bufSize: 4, want: "6789"},
		{name: "crossing the end", off: 6, bufSize: 8, want: "6789", wantErr: io.EOF},
		{name: "whole object into a larger buffer", off: 0, bufSize: 16, want: content, wantErr: io.EOF},
		{name: "at the end", off: 10, bufSize: 4, want: "", wantErr: io.EOF},
		{name: "past the end", off: 11, bufSize: 4, want: "", wantErr: io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.bufSize)
			n, err := reader.ReadAt(buf, tc.off)

			require.Equal(t, tc.want, string(buf[:n]))
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// newFakeS3Storage serves one object from an in-process S3 endpoint that
// honours Range requests, so ReadAt is exercised through the real MinIO client.
func newFakeS3Storage(t *testing.T, bucket, key, content string) *objectstorage.MinIOStorage {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Has("location"):
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`) // test server; a failed write fails the client side
		case r.URL.Path == "/"+bucket || r.URL.Path == "/"+bucket+"/":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/"+bucket+"/"+key:
			serveObject(w, r, content)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	storage, err := objectstorage.NewMinIOStorage(t.Context(), objectstorage.Config{
		Endpoint:  strings.TrimPrefix(server.URL, "http://"),
		Bucket:    bucket,
		AccessKey: "access",
		SecretKey: "secret",
	})
	require.NoError(t, err)
	return storage
}

func serveObject(w http.ResponseWriter, r *http.Request, content string) {
	w.Header().Set("ETag", `"etag"`)
	w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	w.Header().Set("Content-Type", "application/octet-stream")
	body := content
	status := http.StatusOK
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil || start >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		end = min(end, len(content)-1)
		body = content[start : end+1]
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(content)))
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body) // test server; a failed write fails the client side
	}
}

// Callers log upload failures, and logs exclude object keys, so the error
// must not name the key.
func TestUploadErrorsOmitObjectKey(t *testing.T) {
	const key = "build-logs/owner/build-object-key.log"
	storage := newFakeS3Storage(t, "bucket", "object", "")
	path := filepath.Join(t.TempDir(), "upload.log")
	require.NoError(t, os.WriteFile(path, []byte("log"), 0o600))

	for name, upload := range map[string]func() error{
		"file": func() error { return storage.UploadFile(t.Context(), key, path, "text/plain") },
		"stream": func() error {
			return storage.UploadStream(t.Context(), key, strings.NewReader("log"), 3, "text/plain")
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := upload()
			require.Error(t, err)
			require.NotContains(t, err.Error(), key)
			require.NotContains(t, err.Error(), "build-object-key")
		})
	}
}

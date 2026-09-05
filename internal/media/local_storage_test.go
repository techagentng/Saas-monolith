package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalFilesystemStorageUploadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	storage := NewLocalFilesystemStorage(dir, "https://cdn.test.local/media")

	key := "tenants/t1/services/s1/abc123.webp"
	uploaded, err := storage.Upload(context.Background(), UploadInput{
		Key: key, ContentType: "image/webp", Size: 5, Body: bytes.NewReader([]byte("hello")),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if uploaded.Key != key {
		t.Fatalf("Key = %q, want %q", uploaded.Key, key)
	}
	if uploaded.PublicURL != "https://cdn.test.local/media/"+key {
		t.Fatalf("PublicURL = %q", uploaded.PublicURL)
	}

	written, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(key)))
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(written) != "hello" {
		t.Fatalf("file contents = %q, want %q", written, "hello")
	}
}

func TestLocalFilesystemStorageDeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	storage := NewLocalFilesystemStorage(dir, "https://cdn.test.local/media")
	key := "tenants/t1/services/s1/abc123.webp"
	if _, err := storage.Upload(context.Background(), UploadInput{Key: key, Body: bytes.NewReader([]byte("x"))}); err != nil {
		t.Fatal(err)
	}

	if err := storage.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(key))); !os.IsNotExist(err) {
		t.Fatalf("file still exists after Delete(), stat err = %v", err)
	}
}

// Deleting a key that was never uploaded (or was already deleted) is not an
// error — the caller treats "already gone" and "successfully removed" the
// same way.
func TestLocalFilesystemStorageDeleteOfMissingKeyIsNotAnError(t *testing.T) {
	storage := NewLocalFilesystemStorage(t.TempDir(), "https://cdn.test.local/media")
	if err := storage.Delete(context.Background(), "tenants/t1/services/s1/never-existed.webp"); err != nil {
		t.Fatalf("Delete() of a missing key error = %v, want nil", err)
	}
}

// Keys are always server-generated (BuildServiceImageKey), never
// caller-supplied — but resolvePath's containment check is a second,
// structural line of defense that must hold regardless.
func TestLocalFilesystemStorageRefusesToEscapeItsRoot(t *testing.T) {
	storage := NewLocalFilesystemStorage(t.TempDir(), "https://cdn.test.local/media")
	_, err := storage.Upload(context.Background(), UploadInput{
		Key: "../../etc/passwd", Body: bytes.NewReader([]byte("x")),
	})
	if err == nil {
		t.Fatal("Upload() accepted a key that escapes the storage root")
	}
}

func TestBuildServiceImageKeyIsUnpredictableAndScoped(t *testing.T) {
	a := BuildServiceImageKey("tenant-1", "service-1", ".webp")
	b := BuildServiceImageKey("tenant-1", "service-1", ".webp")
	if a == b {
		t.Fatal("BuildServiceImageKey produced the same key twice")
	}
	wantPrefix := "tenants/tenant-1/services/service-1/"
	if len(a) <= len(wantPrefix) || a[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("key = %q, want prefix %q", a, wantPrefix)
	}
}

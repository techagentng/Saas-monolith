package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalFilesystemStorage implements MediaStorage by writing to a directory on
// the same host the API runs on, and serving it back out through a plain
// static file route (wired in internal/app.New) rather than any object-store
// API.
//
// This is the "no provider currently exists in the repo" case: a clean,
// env-driven implementation of the same interface any future S3/Cloudinary/R2
// driver would implement, so adopting one later means writing that one file,
// not touching ServiceImageService or anything above it. It needs no cloud
// credentials, which is exactly why it is the right default for local
// development and for a first production deploy that hasn't yet stood up
// object storage — see internal/config's MediaStorageDriver/MediaLocalDir/
// MediaPublicBaseURL for how a deployment configures it.
type LocalFilesystemStorage struct {
	// rootDir is the absolute filesystem directory objects are written under.
	// Every key is joined beneath it; Upload refuses to escape it (see
	// resolvePath) even though keys are already server-generated and never
	// caller-supplied, as a second, structural line of defense.
	rootDir string
	// publicBaseURL is the externally-reachable origin+path prefix a key is
	// appended to, e.g. "https://api.example.com/media". No trailing slash.
	publicBaseURL string
}

func NewLocalFilesystemStorage(rootDir string, publicBaseURL string) *LocalFilesystemStorage {
	return &LocalFilesystemStorage{
		rootDir:       filepath.Clean(rootDir),
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// resolvePath turns a logical key into an absolute filesystem path, refusing
// one that would resolve outside rootDir. Keys are always server-generated
// (see BuildServiceImageKey) and never come from client input, so this should
// never trigger — it exists as a structural guarantee, not a response to a
// known attack surface.
func (s *LocalFilesystemStorage) resolvePath(key string) (string, error) {
	joined := filepath.Join(s.rootDir, filepath.FromSlash(key))
	if !strings.HasPrefix(joined, s.rootDir+string(filepath.Separator)) && joined != s.rootDir {
		return "", fmt.Errorf("media: key %q escapes the storage root", key)
	}
	return joined, nil
}

func (s *LocalFilesystemStorage) Upload(ctx context.Context, input UploadInput) (*UploadedFile, error) {
	path, err := s.resolvePath(input.Key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("media: creating directory: %w", err)
	}

	// Write to a temp file in the same directory, then rename, so a reader
	// hitting the public URL mid-upload never sees a partially-written file —
	// os.Rename is atomic on the same filesystem, a plain write is not.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("media: creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, input.Body); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("media: writing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("media: closing file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("media: finalizing file: %w", err)
	}

	return &UploadedFile{
		Key:       input.Key,
		PublicURL: s.publicBaseURL + "/" + strings.TrimLeft(filepath.ToSlash(input.Key), "/"),
	}, nil
}

func (s *LocalFilesystemStorage) Delete(ctx context.Context, key string) error {
	path, err := s.resolvePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("media: deleting file: %w", err)
	}
	return nil
}

// BuildServiceImageKey computes the server-controlled storage key for one
// service image upload. tenantID and serviceID are already-validated UUIDs by
// the time this is called (ServiceImageService parses them before ever
// reaching storage); ext is derived from the sniffed content type
// (model.ExtensionForMIMEType), never from a client-supplied filename — a
// filename is never consulted for anything, including display, which is
// exactly what keeps a hostile "../../etc/passwd.jpg" upload harmless: it
// never becomes part of a path.
func BuildServiceImageKey(tenantID, serviceID, ext string) string {
	return fmt.Sprintf("tenants/%s/services/%s/%s%s", tenantID, serviceID, randomFilename(), ext)
}

func randomFilename() string {
	buf := make([]byte, 16)
	// crypto/rand.Read on the standard library's global reader never returns
	// an error in practice (it would mean the OS entropy source is broken),
	// but a zero-value fallback keeps this function infallible rather than
	// propagating an error that has no meaningful caller-side recovery.
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

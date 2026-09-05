package media

import (
	"context"
	"io"
)

// FakeStorage is an in-memory MediaStorage for tests — no real cloud storage,
// and no real filesystem writes, is ever used by the test suite. It records
// every call so a test can assert on upload/delete behavior, including
// negative-path assertions like "a DB failure must trigger a compensating
// delete of the object just uploaded."
type FakeStorage struct {
	// Objects tracks every key currently "stored" — deleted keys are removed.
	Objects map[string][]byte

	UploadCalls int
	DeleteCalls int
	DeletedKeys []string

	// UploadErr, when set, makes every Upload fail with this error instead of
	// succeeding — for exercising "storage failure must not persist DB
	// metadata."
	UploadErr error
	// DeleteErr, when set, makes every Delete fail — for exercising "storage
	// cleanup can itself fail" without a real backend to break.
	DeleteErr error

	// PublicURLPrefix mimics LocalFilesystemStorage's publicBaseURL, so tests
	// can assert on the shape of a returned URL without depending on the real
	// implementation.
	PublicURLPrefix string
}

func NewFakeStorage() *FakeStorage {
	return &FakeStorage{Objects: map[string][]byte{}, PublicURLPrefix: "https://cdn.test.local/media"}
}

func (f *FakeStorage) Upload(_ context.Context, input UploadInput) (*UploadedFile, error) {
	f.UploadCalls++
	if f.UploadErr != nil {
		return nil, f.UploadErr
	}
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	f.Objects[input.Key] = body
	return &UploadedFile{Key: input.Key, PublicURL: f.PublicURLPrefix + "/" + input.Key}, nil
}

func (f *FakeStorage) Delete(_ context.Context, key string) error {
	f.DeleteCalls++
	f.DeletedKeys = append(f.DeletedKeys, key)
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	delete(f.Objects, key)
	return nil
}

// compile-time guard: the fake must keep satisfying the real interface.
var _ MediaStorage = (*FakeStorage)(nil)

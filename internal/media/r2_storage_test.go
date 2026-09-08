package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
)

// fakeS3 is an in-memory stand-in for the S3 operations R2Storage uses. No
// unit test in this package ever reaches real Cloudflare R2.
type fakeS3 struct {
	objects map[string][]byte

	putBucket string
	putKey    string
	putBody   string
	putType   string
	putLen    int64

	delBucket string
	delKey    string

	putErr error
	delErr error
}

func newFakeS3() *fakeS3 { return &fakeS3{objects: map[string][]byte{}} }

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	f.putBucket = aws.ToString(in.Bucket)
	f.putKey = aws.ToString(in.Key)
	f.putType = aws.ToString(in.ContentType)
	f.putLen = aws.ToInt64(in.ContentLength)
	body, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	f.putBody = string(body)
	f.objects[f.putKey] = body
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.delErr != nil {
		return nil, f.delErr
	}
	f.delBucket = aws.ToString(in.Bucket)
	f.delKey = aws.ToString(in.Key)
	delete(f.objects, f.delKey)
	return &s3.DeleteObjectOutput{}, nil
}

type apiError struct{ code, message string }

func (e apiError) Error() string                 { return e.code + ": " + e.message }
func (e apiError) ErrorCode() string             { return e.code }
func (e apiError) ErrorMessage() string          { return e.message }
func (e apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

const (
	testBucket    = "bookflow"
	testPublicURL = "https://media.iweapps.com"
	logicalKey    = "tenants/t1/services/s1/abc123.webp"
)

func newTestR2(t *testing.T, prefix string) (*R2Storage, *fakeS3) {
	t.Helper()
	fake := newFakeS3()
	storage, err := NewR2Storage(fake, testBucket, prefix, testPublicURL)
	if err != nil {
		t.Fatalf("NewR2Storage() error = %v", err)
	}
	return storage, fake
}

func TestNewR2StorageValidatesInputs(t *testing.T) {
	if _, err := NewR2Storage(nil, testBucket, "serviceimg", testPublicURL); err == nil {
		t.Fatal("NewR2Storage accepted a nil client")
	}
	if _, err := NewR2Storage((*s3.Client)(nil), testBucket, "serviceimg", testPublicURL); err == nil {
		t.Fatal("NewR2Storage accepted a typed-nil client")
	}
	if _, err := NewR2Storage(newFakeS3(), "  ", "serviceimg", testPublicURL); err == nil {
		t.Fatal("NewR2Storage accepted an empty bucket")
	}
	if _, err := NewR2Storage(newFakeS3(), testBucket, "serviceimg", ""); err == nil {
		t.Fatal("NewR2Storage accepted an empty public base URL")
	}
}

func TestR2StorageNormalizesPrefixAndPublicBaseURL(t *testing.T) {
	fake := newFakeS3()
	storage, err := NewR2Storage(fake, testBucket, "/serviceimg/", testPublicURL+"/")
	if err != nil {
		t.Fatalf("NewR2Storage() error = %v", err)
	}
	if storage.prefix != "serviceimg" {
		t.Fatalf("prefix = %q, want %q", storage.prefix, "serviceimg")
	}
	if storage.publicBaseURL != testPublicURL {
		t.Fatalf("publicBaseURL = %q, want %q", storage.publicBaseURL, testPublicURL)
	}
}

func TestR2StorageUploadBuildsBucketKeyAndPublicURL(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")

	uploaded, err := storage.Upload(context.Background(), UploadInput{
		Key:         logicalKey,
		ContentType: "image/webp",
		Size:        5,
		Body:        bytes.NewReader([]byte("hello")),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if fake.putBucket != testBucket {
		t.Fatalf("PutObject bucket = %q, want %q", fake.putBucket, testBucket)
	}
	wantObjectKey := "serviceimg/" + logicalKey
	if fake.putKey != wantObjectKey {
		t.Fatalf("PutObject key = %q, want %q", fake.putKey, wantObjectKey)
	}
	if fake.putBody != "hello" || fake.putType != "image/webp" || fake.putLen != 5 {
		t.Fatalf("PutObject body/type/len = %q/%q/%d", fake.putBody, fake.putType, fake.putLen)
	}
	// The DB persists the LOGICAL key (prefix-free), exactly like the local
	// driver, so rows stay portable across a driver swap.
	if uploaded.Key != logicalKey {
		t.Fatalf("UploadedFile.Key = %q, want the logical key %q", uploaded.Key, logicalKey)
	}
	wantURL := testPublicURL + "/serviceimg/" + logicalKey
	if uploaded.PublicURL != wantURL {
		t.Fatalf("PublicURL = %q, want %q", uploaded.PublicURL, wantURL)
	}
	if strings.Contains(uploaded.PublicURL, "?") || strings.Contains(uploaded.PublicURL, "X-Amz") {
		t.Fatalf("PublicURL %q looks signed — it must be a plain, browser-safe URL", uploaded.PublicURL)
	}
}

func TestR2StorageAppliesPrefixExactlyOnce(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")
	if _, err := storage.Upload(context.Background(), UploadInput{Key: logicalKey, Body: bytes.NewReader([]byte("x"))}); err != nil {
		t.Fatal(err)
	}
	if got := fake.putKey; strings.Count(got, "serviceimg/") != 1 {
		t.Fatalf("object key %q does not contain the prefix exactly once", got)
	}
	if strings.Contains(fake.putKey, "//") {
		t.Fatalf("object key %q contains a doubled slash", fake.putKey)
	}
}

func TestR2StorageEmptyPrefixWritesAtBucketRoot(t *testing.T) {
	storage, fake := newTestR2(t, "")
	uploaded, err := storage.Upload(context.Background(), UploadInput{Key: logicalKey, Body: bytes.NewReader([]byte("x"))})
	if err != nil {
		t.Fatal(err)
	}
	if fake.putKey != logicalKey {
		t.Fatalf("object key = %q, want %q", fake.putKey, logicalKey)
	}
	if uploaded.PublicURL != testPublicURL+"/"+logicalKey {
		t.Fatalf("PublicURL = %q", uploaded.PublicURL)
	}
}

func TestR2StorageUploadRejectsEmptyKey(t *testing.T) {
	storage, _ := newTestR2(t, "serviceimg")
	if _, err := storage.Upload(context.Background(), UploadInput{Key: "  ", Body: bytes.NewReader(nil)}); err == nil {
		t.Fatal("Upload accepted an empty key")
	}
}

func TestR2StorageUploadPropagatesError(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")
	fake.putErr = apiError{code: "AccessDenied", message: "no"}
	_, err := storage.Upload(context.Background(), UploadInput{Key: logicalKey, Body: bytes.NewReader([]byte("x"))})
	if err == nil {
		t.Fatal("Upload did not propagate the storage error")
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("error %q should name the API failure", err)
	}
}

func TestR2StorageDeleteUsesPrefixedBucketKey(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")
	if err := storage.Delete(context.Background(), logicalKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if fake.delBucket != testBucket || fake.delKey != "serviceimg/"+logicalKey {
		t.Fatalf("DeleteObject bucket/key = %q/%q", fake.delBucket, fake.delKey)
	}
}

func TestR2StorageDeleteTreatsMissingObjectAsSuccess(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")
	fake.delErr = apiError{code: "NoSuchKey", message: "gone"}
	if err := storage.Delete(context.Background(), logicalKey); err != nil {
		t.Fatalf("Delete() of a missing object error = %v, want nil", err)
	}
}

func TestR2StorageDeletePropagatesRealError(t *testing.T) {
	storage, fake := newTestR2(t, "serviceimg")
	fake.delErr = apiError{code: "InternalError", message: "boom"}
	if err := storage.Delete(context.Background(), logicalKey); err == nil {
		t.Fatal("Delete did not propagate a non-not-found error")
	}
}

func TestR2StorageDeleteRejectsEmptyKey(t *testing.T) {
	storage, _ := newTestR2(t, "serviceimg")
	if err := storage.Delete(context.Background(), "   "); err == nil {
		t.Fatal("Delete accepted an empty key")
	}
}

// The access key and secret live only in the S3 client's signer, never in
// R2Storage — so no failure path can put them into an error.
func TestR2StorageErrorsNeverContainCredentials(t *testing.T) {
	const secret = "super-secret-r2-key"
	storage, fake := newTestR2(t, "serviceimg")
	fake.putErr = errors.New("connection reset")
	fake.delErr = errors.New("connection reset")

	_, uploadErr := storage.Upload(context.Background(), UploadInput{Key: logicalKey, Body: bytes.NewReader([]byte("x"))})
	deleteErr := storage.Delete(context.Background(), logicalKey)
	for _, err := range []error{uploadErr, deleteErr} {
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaked the secret", err)
		}
	}
}

func TestNewR2ClientRequiresEndpointAndCredentials(t *testing.T) {
	if _, err := NewR2Client(context.Background(), R2ClientConfig{AccessKeyID: "a", SecretAccessKey: "b"}); err == nil {
		t.Fatal("NewR2Client accepted a missing endpoint")
	}
	if _, err := NewR2Client(context.Background(), R2ClientConfig{Endpoint: "https://x.r2.cloudflarestorage.com"}); err == nil {
		t.Fatal("NewR2Client accepted missing credentials")
	}
}

func TestNewR2ClientBuildsAPathStyleClientForTheGivenEndpoint(t *testing.T) {
	client, err := NewR2Client(context.Background(), R2ClientConfig{
		Endpoint:        "https://7ffa12f1c0499f3341c35fc21f9347f5.r2.cloudflarestorage.com",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("NewR2Client() error = %v", err)
	}
	opts := client.Options()
	if aws.ToString(opts.BaseEndpoint) != "https://7ffa12f1c0499f3341c35fc21f9347f5.r2.cloudflarestorage.com" {
		t.Fatalf("BaseEndpoint = %q", aws.ToString(opts.BaseEndpoint))
	}
	if !opts.UsePathStyle {
		t.Fatal("UsePathStyle = false, want true for R2 compatibility")
	}
	if opts.Region != "auto" {
		t.Fatalf("Region = %q, want \"auto\"", opts.Region)
	}
}

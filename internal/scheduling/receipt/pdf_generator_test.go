package receipt

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func sampleData() Data {
	email := "ada@example.test"
	phone := "+2348012345678"
	return Data{
		BusinessName:    "Acme Nails",
		Reference:       "NB-1A2B3C4D",
		Status:          "CONFIRMED",
		ServiceName:     "Gel Manicure",
		DurationMinutes: 45,
		TechnicianName:  "Ada",
		Date:            "2026-09-26",
		Start:           "14:00",
		End:             "14:45",
		Timezone:        "Africa/Lagos",
		FormattedPrice:  "₦15,000.00",
		CustomerName:    "Jane Doe",
		CustomerEmail:   &email,
		CustomerPhone:   &phone,
	}
}

// encodeOnePixelPNG builds a minimal, valid 1x1 PNG. Encoding this fixed,
// trivial image cannot realistically fail, so this has no error return —
// shared by this file's tests and logo_fetcher_test.go's httptest server,
// which needs real bytes on the wire rather than a fake in-memory Fetch.
func encodeOnePixelPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	return encodeOnePixelPNG()
}

type fakeLogoFetcher struct {
	data        []byte
	contentType string
	err         error
	calls       int
}

func (f *fakeLogoFetcher) Fetch(context.Context, string) ([]byte, string, error) {
	f.calls++
	if f.err != nil {
		return nil, "", f.err
	}
	return f.data, f.contentType, nil
}

func isPDF(b []byte) bool {
	return bytes.HasPrefix(b, []byte("%PDF"))
}

func TestGenerateWithoutALogoProducesAValidPDF(t *testing.T) {
	gen := NewPDFGenerator(nil)
	logoURL := "https://media.example.test/logo.png"
	data := sampleData()
	data.LogoURL = &logoURL // set, but no fetcher wired — same as "no logo"

	out, err := gen.Generate(context.Background(), data)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !isPDF(out) {
		t.Fatalf("output does not start with the PDF magic bytes: %q", out[:min(20, len(out))])
	}
	if len(out) == 0 {
		t.Fatal("Generate() returned an empty PDF")
	}
}

func TestGenerateWithAValidLogoSucceeds(t *testing.T) {
	fetcher := &fakeLogoFetcher{data: onePixelPNG(t), contentType: "image/png"}
	gen := NewPDFGenerator(fetcher)
	logoURL := "https://media.example.test/logo.png"
	data := sampleData()
	data.LogoURL = &logoURL

	out, err := gen.Generate(context.Background(), data)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !isPDF(out) {
		t.Fatal("output is not a PDF")
	}
	if fetcher.calls != 1 {
		t.Fatalf("logo fetcher calls = %d, want 1", fetcher.calls)
	}
}

func TestGenerateToleratesALogoFetchFailure(t *testing.T) {
	fetcher := &fakeLogoFetcher{err: errors.New("network down")}
	gen := NewPDFGenerator(fetcher)
	logoURL := "https://media.example.test/logo.png"
	data := sampleData()
	data.LogoURL = &logoURL

	out, err := gen.Generate(context.Background(), data)
	if err != nil {
		t.Fatalf("a logo fetch failure must not fail the receipt: Generate() error = %v", err)
	}
	if !isPDF(out) {
		t.Fatal("output is not a PDF")
	}
}

func TestGenerateToleratesAnUnsupportedLogoContentType(t *testing.T) {
	fetcher := &fakeLogoFetcher{data: []byte("not an image"), contentType: "text/plain"}
	gen := NewPDFGenerator(fetcher)
	logoURL := "https://media.example.test/logo.txt"
	data := sampleData()
	data.LogoURL = &logoURL

	out, err := gen.Generate(context.Background(), data)
	if err != nil {
		t.Fatalf("an unsupported logo content type must not fail the receipt: Generate() error = %v", err)
	}
	if !isPDF(out) {
		t.Fatal("output is not a PDF")
	}
}

func TestGenerateToleratesCorruptLogoBytes(t *testing.T) {
	// Correct content type, but the bytes are not a real PNG — tryFetchLogo's
	// stdlib image.Decode step must catch this before fpdf ever sees it.
	fetcher := &fakeLogoFetcher{data: []byte("PNG-shaped garbage, not actually a PNG"), contentType: "image/png"}
	gen := NewPDFGenerator(fetcher)
	logoURL := "https://media.example.test/logo.png"
	data := sampleData()
	data.LogoURL = &logoURL

	out, err := gen.Generate(context.Background(), data)
	if err != nil {
		t.Fatalf("corrupt logo bytes must not fail the receipt: Generate() error = %v", err)
	}
	if !isPDF(out) {
		t.Fatal("output is not a PDF")
	}
}

func TestGenerateWithNoLogoURLNeverCallsTheFetcher(t *testing.T) {
	fetcher := &fakeLogoFetcher{data: onePixelPNG(t), contentType: "image/png"}
	gen := NewPDFGenerator(fetcher)
	data := sampleData() // LogoURL left nil

	if _, err := gen.Generate(context.Background(), data); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("fetcher calls = %d, want 0 when Data.LogoURL is nil", fetcher.calls)
	}
}

// The receipt must never render as an accounting/invoice document — S12-BE
// is explicit that this is a "Booking Confirmation / Receipt" and no more.
// This is a light content smoke check: the raw PDF bytes are compressed by
// default, so this only confirms the literal header text elsewhere in the
// stream is absent from an obviously wrong alternate title, not a full
// text-extraction assertion.
func TestGenerateNeverLabelsItselfAnInvoice(t *testing.T) {
	gen := NewPDFGenerator(nil)
	out, err := gen.Generate(context.Background(), sampleData())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if strings.Contains(strings.ToLower(string(out)), "invoice") {
		t.Fatal("receipt PDF bytes contain the word 'invoice'")
	}
}

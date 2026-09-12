package receipt

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // decoders registered for side effect only — see decodeLogo
	_ "image/png"

	"github.com/go-pdf/fpdf"
)

// LogoFetcher retrieves a logo image's raw bytes and its sniffed content
// type, given a URL. PDFGenerator never fetches over the network itself —
// production wires HTTPLogoFetcher (SSRF-restricted to the app's own media
// host); tests inject a fake, so PDFGenerator's own tests never touch the
// network.
type LogoFetcher interface {
	Fetch(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// PDFGenerator renders Data as a simple, single-page PDF using go-pdf/fpdf —
// a pure-Go library with no cgo and no headless-browser dependency, chosen
// because the project had no PDF or HTML-to-PDF tooling at all before S12-BE
// (confirmed by audit: nothing in go.mod).
//
// A missing, failing, or unsupported logo NEVER fails the receipt: Generate
// always produces a PDF for valid Data, falling back to the business name
// rendered prominently when the logo cannot be included.
type PDFGenerator struct {
	logos LogoFetcher
}

// NewPDFGenerator wires a PDFGenerator over the given LogoFetcher. Pass
// NewHTTPLogoFetcher(...) in production.
func NewPDFGenerator(logos LogoFetcher) *PDFGenerator {
	return &PDFGenerator{logos: logos}
}

func (g *PDFGenerator) Generate(ctx context.Context, data Data) ([]byte, error) {
	logo := g.tryFetchLogo(ctx, data.LogoURL)
	return render(data, logo)
}

// decodedLogo is a logo already fetched AND successfully decoded as a real
// image — the point where PDFGenerator commits to using it. Decoding via the
// standard library first (rather than handing raw, unvalidated bytes
// straight to fpdf) is what lets a corrupt-but-correctly-MIME-typed file be
// caught and discarded before fpdf's own, less forgiving decoder ever sees
// it and enters its internal error state.
type decodedLogo struct {
	bytes []byte
	kind  string // "PNG" or "JPG", fpdf's ImageOptions.ImageType vocabulary
}

// tryFetchLogo fetches and fully validates data.LogoURL, returning nil for
// every failure mode: no URL configured, no fetcher wired, a network/host
// error, an unsupported content type, or bytes that fail to decode as a real
// image. Every one of these is silently treated as "render without a logo" —
// never an error Generate propagates.
func (g *PDFGenerator) tryFetchLogo(ctx context.Context, logoURL *string) *decodedLogo {
	if logoURL == nil || g.logos == nil {
		return nil
	}
	data, contentType, err := g.logos.Fetch(ctx, *logoURL)
	if err != nil {
		return nil
	}
	kind := imageKindForContentType(contentType)
	if kind == "" {
		return nil
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return nil
	}
	return &decodedLogo{bytes: data, kind: kind}
}

func imageKindForContentType(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return "JPG"
	case "image/png":
		return "PNG"
	default:
		return ""
	}
}

// render builds the entire receipt in one pass over a fresh *fpdf.Fpdf.
// Called at most twice by Generate — once optimistically with a logo, and
// again with logo=nil ONLY in the vanishingly rare case fpdf's own decoder
// rejects an image the standard library already validated (see
// tryFetchLogo) — and never partially: renderHeader reports whether placing
// the logo left the Fpdf instance in fpdf's own internal error state, in
// which case that whole instance is discarded (per fpdf's documented
// behavior, every method becomes a no-op once an error is set, so
// "continuing" to build the rest of the content on it would silently
// produce a truncated, broken receipt) and a completely fresh instance
// renders the logo-free fallback, which has no image operation and so
// cannot fail the same way.
func render(data Data, logo *decodedLogo) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	if !renderHeader(pdf, data, logo) {
		return render(data, nil)
	}

	pdf.SetFont("Arial", "", 12)
	pdf.CellFormat(0, 6, "Booking Confirmation / Receipt", "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont("Arial", "B", 11)
	pdf.CellFormat(0, 6, fmt.Sprintf("Booking Reference: %s", data.Reference), "", 1, "L", false, 0, "")
	pdf.CellFormat(0, 6, fmt.Sprintf("Status: %s", data.Status), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	pdf.SetFont("Arial", "", 11)
	line := func(label, value string) {
		pdf.CellFormat(0, 6, fmt.Sprintf("%s: %s", label, value), "", 1, "L", false, 0, "")
	}
	line("Service", data.ServiceName)
	line("Technician", data.TechnicianName)
	line("Date", data.Date)
	line("Time", fmt.Sprintf("%s - %s", data.Start, data.End))
	line("Duration", fmt.Sprintf("%d minutes", data.DurationMinutes))
	line("Price", data.FormattedPrice)
	line("Timezone", data.Timezone)
	pdf.Ln(4)

	pdf.SetFont("Arial", "B", 11)
	pdf.CellFormat(0, 6, "Customer", "", 1, "L", false, 0, "")
	pdf.SetFont("Arial", "", 11)
	line("Name", data.CustomerName)
	if data.CustomerEmail != nil {
		line("Email", *data.CustomerEmail)
	}
	if data.CustomerPhone != nil {
		line("Phone", *data.CustomerPhone)
	}
	pdf.Ln(6)

	pdf.SetFont("Arial", "I", 10)
	pdf.MultiCell(0, 5, fmt.Sprintf("Thank you for booking with %s.", data.BusinessName), "", "L", false)

	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("building receipt PDF: %w", err)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("rendering receipt PDF: %w", err)
	}
	return buf.Bytes(), nil
}

// renderHeader places the logo, if one was successfully validated, or falls
// back to the business name set prominently — the one mandatory fallback
// S12-BE requires. logo is nil for every failure mode tryFetchLogo handles.
//
// Returns false ONLY when a logo was supplied and fpdf itself rejected it
// (RegisterImageOptionsReader or ImageOptions left the instance in an error
// state) — the caller must then discard pdf entirely; render() does exactly
// that. It is never false for logo == nil: the plain business-name path
// uses no fpdf feature that can fail this way.
func renderHeader(pdf *fpdf.Fpdf, data Data, logo *decodedLogo) bool {
	if logo != nil {
		options := fpdf.ImageOptions{ImageType: logo.kind, ReadDpi: true}
		info := pdf.RegisterImageOptionsReader("logo", options, bytes.NewReader(logo.bytes))
		if !pdf.Ok() || info == nil {
			return false
		}
		pdf.ImageOptions("logo", 20, 15, 0, 20, false, options, 0, "")
		if !pdf.Ok() {
			return false
		}
		pdf.Ln(24)
		pdf.SetFont("Arial", "B", 16)
		pdf.CellFormat(0, 8, data.BusinessName, "", 1, "L", false, 0, "")
		pdf.Ln(2)
		return true
	}
	pdf.SetFont("Arial", "B", 18)
	pdf.CellFormat(0, 10, data.BusinessName, "", 1, "L", false, 0, "")
	pdf.Ln(4)
	return true
}

// compile-time guard.
var _ Generator = (*PDFGenerator)(nil)

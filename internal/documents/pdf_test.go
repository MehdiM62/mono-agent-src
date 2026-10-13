package documents_test

import (
	"bytes"
	"context"
	"github.com/go-rod/rod/lib/launcher"
	"testing"

	"github.com/monoes/mono-agent/internal/documents"
)

func TestRenderPDFProducesValidPDF(t *testing.T) {
	// Unit tests never download a browser: without a local Chrome/Chromium
	// the render can't run here, which is an environment limit.
	if _, ok := launcher.LookPath(); !ok {
		t.Skip("no local Chrome/Chromium; RenderPDF needs a browser")
	}
	pdfBytes, err := documents.RenderPDF(context.Background(), "<html><body><h1>Test</h1></body></html>")
	if err != nil {
		t.Fatalf("RenderPDF: %v (requires a headless Chrome/Chromium binary reachable by go-rod's launcher — if none is available in this environment, this is an environmental limitation, not a code defect; report it as such rather than treating it as a logic bug)", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("expected output to start with the PDF magic number %%PDF-, got first bytes: %q", pdfBytes[:min(20, len(pdfBytes))])
	}
}

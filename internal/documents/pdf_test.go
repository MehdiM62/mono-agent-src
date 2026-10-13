package documents_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/go-rod/rod/lib/launcher"

	"github.com/monoes/mono-agent/internal/documents"
)

func TestRenderPDFProducesValidPDF(t *testing.T) {
	// RenderPDF needs a Chrome/Chromium. Without one on this machine go-rod
	// downloads one, which unit tests may not do (testhome blocks the
	// network): skip unless a browser is installed or the network is allowed.
	if _, found := launcher.LookPath(); !found && os.Getenv("MONOAGENT_TEST_ALLOW_NETWORK") != "1" {
		t.Skip("no Chrome/Chromium on this machine (set MONOAGENT_TEST_ALLOW_NETWORK=1 to let go-rod download one)")
	}
	pdfBytes, err := documents.RenderPDF(context.Background(), "<html><body><h1>Test</h1></body></html>")
	if err != nil {
		t.Fatalf("RenderPDF: %v (requires a headless Chrome/Chromium binary reachable by go-rod's launcher — if none is available in this environment, this is an environmental limitation, not a code defect; report it as such rather than treating it as a logic bug)", err)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF-")) {
		t.Fatalf("expected output to start with the PDF magic number %%PDF-, got first bytes: %q", pdfBytes[:min(20, len(pdfBytes))])
	}
}

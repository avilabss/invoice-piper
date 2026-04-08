package email

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

var chromedpRun = chromedp.Run

var htmlRenderBlockedURLPatterns = []string{
	"*",
}

var (
	enableNetworkForHTMLRender = func(ctx context.Context) error {
		return network.Enable().Do(ctx)
	}
	blockURLsForHTMLRender = func(ctx context.Context, patterns []string) error {
		return network.SetBlockedURLs(patterns).Do(ctx)
	}
	setScriptExecutionDisabledForHTMLRender = func(ctx context.Context, disabled bool) error {
		return emulation.SetScriptExecutionDisabled(disabled).Do(ctx)
	}
)

// NewChromeAllocator creates a headless Chrome allocator context that can be
// shared across multiple HTMLToPDF calls to avoid spawning a new Chrome
// process for each conversion. The caller must call the returned cancel func.
func NewChromeAllocator(ctx context.Context) (context.Context, context.CancelFunc) {
	return chromedp.NewExecAllocator(ctx, chromedp.DefaultExecAllocatorOptions[:]...)
}

// HTMLToPDF converts an HTML string to a PDF using headless Chrome.
// The ctx should come from NewChromeAllocator for efficient Chrome reuse.
// If ctx has no allocator, chromedp will create one automatically.
func HTMLToPDF(ctx context.Context, html string) ([]byte, error) {
	tabCtx, cancel := chromedp.NewContext(ctx)
	defer cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(tabCtx, 30*time.Second)
	defer timeoutCancel()

	var pdfData []byte

	if err := chromedpRun(timeoutCtx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(applyHTMLRenderIsolation),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frameTree.Frame.ID, html).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfData, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				Do(ctx)
			return err
		}),
	); err != nil {
		return nil, wrapHTMLToPDFError(err)
	}

	return pdfData, nil
}

func applyHTMLRenderIsolation(ctx context.Context) error {
	if err := enableNetworkForHTMLRender(ctx); err != nil {
		return err
	}

	if err := blockURLsForHTMLRender(ctx, htmlRenderBlockedURLPatterns); err != nil {
		return err
	}

	if err := setScriptExecutionDisabledForHTMLRender(ctx, true); err != nil {
		return err
	}

	return nil
}

func wrapHTMLToPDFError(err error) error {
	if browserUnavailable(err) {
		return fmt.Errorf("converting HTML to PDF: Chrome/Chromium browser is required for HTML email conversion; install Chrome or Chromium and ensure it is available in PATH: %w", err)
	}

	return fmt.Errorf("converting HTML to PDF: %w", err)
}

func browserUnavailable(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, exec.ErrNotFound) {
		return true
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "could not find a valid chrome browser") {
		return true
	}

	return strings.Contains(message, "executable file not found") &&
		(strings.Contains(message, "chrome") || strings.Contains(message, "chromium"))
}

package email

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestHTMLToPDF_MissingBrowserErrorIncludesActionableGuidance(t *testing.T) {
	originalRun := chromedpRun
	t.Cleanup(func() {
		chromedpRun = originalRun
	})

	chromedpRun = func(ctx context.Context, actions ...chromedp.Action) error {
		return &exec.Error{Name: "google-chrome", Err: exec.ErrNotFound}
	}

	_, err := HTMLToPDF(context.Background(), "<html><body>Invoice</body></html>")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	for _, want := range []string{
		"converting HTML to PDF",
		"Chrome/Chromium browser is required",
		"install Chrome or Chromium",
		"PATH",
	} {
		if !strings.Contains(errMsg, want) {
			t.Errorf("error message %q should contain %q", errMsg, want)
		}
	}
}

func TestHTMLToPDF_NonBrowserErrorsRemainUnchanged(t *testing.T) {
	originalRun := chromedpRun
	t.Cleanup(func() {
		chromedpRun = originalRun
	})

	chromedpRun = func(ctx context.Context, actions ...chromedp.Action) error {
		return errors.New("print failed")
	}

	_, err := HTMLToPDF(context.Background(), "<html><body>Invoice</body></html>")
	if err == nil {
		t.Fatal("expected error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "converting HTML to PDF: print failed") {
		t.Fatalf("unexpected error message: %q", errMsg)
	}
	if strings.Contains(errMsg, "install Chrome or Chromium") {
		t.Fatalf("error message should not include browser install guidance: %q", errMsg)
	}
}

func TestHTMLToPDF_AppliesIsolationBeforeRendering(t *testing.T) {
	originalRun := chromedpRun
	originalEnable := enableNetworkForHTMLRender
	originalBlock := blockURLsForHTMLRender
	originalDisable := setScriptExecutionDisabledForHTMLRender
	t.Cleanup(func() {
		chromedpRun = originalRun
		enableNetworkForHTMLRender = originalEnable
		blockURLsForHTMLRender = originalBlock
		setScriptExecutionDisabledForHTMLRender = originalDisable
	})

	var callOrder []string
	enableNetworkForHTMLRender = func(context.Context) error {
		callOrder = append(callOrder, "enable")
		return nil
	}
	blockURLsForHTMLRender = func(context.Context, []string) error {
		callOrder = append(callOrder, "block")
		return nil
	}
	setScriptExecutionDisabledForHTMLRender = func(context.Context, bool) error {
		callOrder = append(callOrder, "disable")
		return nil
	}

	stopErr := errors.New("stop after isolation")
	chromedpRun = func(ctx context.Context, actions ...chromedp.Action) error {
		if len(actions) < 2 {
			t.Fatalf("expected at least 2 actions, got %d", len(actions))
		}
		if err := actions[1].Do(ctx); err != nil {
			return err
		}
		return stopErr
	}

	_, err := HTMLToPDF(context.Background(), "<html><body>Invoice</body></html>")
	if !errors.Is(err, stopErr) {
		t.Fatalf("error = %v, want wrapped %v", err, stopErr)
	}

	wantOrder := []string{"enable", "block", "disable"}
	if !reflect.DeepEqual(callOrder, wantOrder) {
		t.Fatalf("isolation call order = %v, want %v", callOrder, wantOrder)
	}
}

func TestApplyHTMLRenderIsolation_DefaultPolicy(t *testing.T) {
	originalEnable := enableNetworkForHTMLRender
	originalBlock := blockURLsForHTMLRender
	originalDisable := setScriptExecutionDisabledForHTMLRender
	t.Cleanup(func() {
		enableNetworkForHTMLRender = originalEnable
		blockURLsForHTMLRender = originalBlock
		setScriptExecutionDisabledForHTMLRender = originalDisable
	})

	var callOrder []string
	var gotPatterns []string
	var gotDisabled bool

	enableNetworkForHTMLRender = func(context.Context) error {
		callOrder = append(callOrder, "enable")
		return nil
	}
	blockURLsForHTMLRender = func(_ context.Context, patterns []string) error {
		callOrder = append(callOrder, "block")
		gotPatterns = append([]string(nil), patterns...)
		return nil
	}
	setScriptExecutionDisabledForHTMLRender = func(_ context.Context, disabled bool) error {
		callOrder = append(callOrder, "disable")
		gotDisabled = disabled
		return nil
	}

	if err := applyHTMLRenderIsolation(context.Background()); err != nil {
		t.Fatalf("applyHTMLRenderIsolation returned error: %v", err)
	}

	wantOrder := []string{"enable", "block", "disable"}
	if !reflect.DeepEqual(callOrder, wantOrder) {
		t.Fatalf("call order = %v, want %v", callOrder, wantOrder)
	}

	if !reflect.DeepEqual(gotPatterns, htmlRenderBlockedURLPatterns) {
		t.Fatalf("blocked patterns = %v, want %v", gotPatterns, htmlRenderBlockedURLPatterns)
	}

	if !gotDisabled {
		t.Fatal("script execution should be disabled")
	}
}

func TestApplyHTMLRenderIsolation_ErrorPropagation(t *testing.T) {
	tests := []struct {
		name      string
		failAt    string
		wantCalls []string
	}{
		{name: "network enable fails", failAt: "enable", wantCalls: []string{"enable"}},
		{name: "URL blocking fails", failAt: "block", wantCalls: []string{"enable", "block"}},
		{name: "script disable fails", failAt: "disable", wantCalls: []string{"enable", "block", "disable"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalEnable := enableNetworkForHTMLRender
			originalBlock := blockURLsForHTMLRender
			originalDisable := setScriptExecutionDisabledForHTMLRender
			t.Cleanup(func() {
				enableNetworkForHTMLRender = originalEnable
				blockURLsForHTMLRender = originalBlock
				setScriptExecutionDisabledForHTMLRender = originalDisable
			})

			var callOrder []string
			wantErr := errors.New("isolation setup failed")

			enableNetworkForHTMLRender = func(context.Context) error {
				callOrder = append(callOrder, "enable")
				if tt.failAt == "enable" {
					return wantErr
				}
				return nil
			}
			blockURLsForHTMLRender = func(context.Context, []string) error {
				callOrder = append(callOrder, "block")
				if tt.failAt == "block" {
					return wantErr
				}
				return nil
			}
			setScriptExecutionDisabledForHTMLRender = func(context.Context, bool) error {
				callOrder = append(callOrder, "disable")
				if tt.failAt == "disable" {
					return wantErr
				}
				return nil
			}

			err := applyHTMLRenderIsolation(context.Background())
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want wrapped %v", err, wantErr)
			}

			if !reflect.DeepEqual(callOrder, tt.wantCalls) {
				t.Fatalf("call order = %v, want %v", callOrder, tt.wantCalls)
			}
		})
	}
}

package cmd

import (
	"io"
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing write pipe: %v", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("closing read pipe: %v", err)
	}

	return string(data)
}

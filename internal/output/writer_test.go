package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteAttachment(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)

	path, err := w.WriteAttachment(2025, 1, "amazon", date, "invoice.pdf", []byte("pdf-content"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := filepath.Join(dir, "2025", "01", "amazon", "20250115_143022.pdf")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != "pdf-content" {
		t.Errorf("content = %q, want %q", string(data), "pdf-content")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat written file: %v", err)
	}
	if got := info.Mode().Perm(); got != outputFilePerm {
		t.Errorf("file mode = %#o, want %#o", got, outputFilePerm)
	}
}

func TestWriteAttachment_CollisionHandling(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)

	// Write first file
	path1, err := w.WriteAttachment(2025, 3, "zomato", date, "receipt.pdf", []byte("first"))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Write second file with same timestamp
	path2, err := w.WriteAttachment(2025, 3, "zomato", date, "receipt.pdf", []byte("second"))
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	if path1 == path2 {
		t.Error("collision: both paths are identical")
	}

	expectedBase := filepath.Join(dir, "2025", "03", "zomato")
	if filepath.Base(path1) != "20250310_090000.pdf" {
		t.Errorf("first file = %q, want 20250310_090000.pdf", filepath.Base(path1))
	}
	if filepath.Base(path2) != "20250310_090000_2.pdf" {
		t.Errorf("second file = %q, want 20250310_090000_2.pdf", filepath.Base(path2))
	}

	_ = expectedBase
}

func TestWriteAttachment_NoExtension(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	path, err := w.WriteAttachment(2025, 6, "unknown", date, "noext", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filepath.Ext(path) != ".bin" {
		t.Errorf("ext = %q, want .bin", filepath.Ext(path))
	}
}

func TestWriteAttachment_DirectoryCreation(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2024, 12, 25, 8, 0, 0, 0, time.UTC)

	_, err := w.WriteAttachment(2024, 12, "openai", date, "invoice.pdf", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	providerDir := filepath.Join(dir, "2024", "12", "openai")
	info, err := os.Stat(providerDir)
	if err != nil {
		t.Fatalf("provider dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("provider path is not a directory")
	}
	if got := info.Mode().Perm(); got != outputDirPerm {
		t.Errorf("provider dir mode = %#o, want %#o", got, outputDirPerm)
	}
}

func TestWriteAttachment_TightensReusedDirectoryPermissions(t *testing.T) {
	dir := t.TempDir()
	yearDir := filepath.Join(dir, "2024")
	monthDir := filepath.Join(yearDir, "12")
	providerDir := filepath.Join(monthDir, "openai")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("creating provider dir: %v", err)
	}

	for _, path := range []string{yearDir, monthDir, providerDir} {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod %s: %v", path, err)
		}
	}

	w := NewWriter(dir)
	date := time.Date(2024, 12, 25, 8, 0, 0, 0, time.UTC)

	path, err := w.WriteAttachment(2024, 12, "openai", date, "invoice.pdf", []byte("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := filepath.Dir(path); got != providerDir {
		t.Fatalf("file written to %q, want %q", got, providerDir)
	}

	for _, path := range []string{yearDir, monthDir, providerDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != outputDirPerm {
			t.Fatalf("dir mode for %s = %#o, want %#o", path, got, outputDirPerm)
		}
	}
}

func TestWriteAttachment_ConcurrentWritesUniquePaths(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 3, 10, 9, 0, 0, 0, time.UTC)

	const writes = 25
	paths := make([]string, writes)
	errs := make([]error, writes)

	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < writes; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			paths[i], errs[i] = w.WriteAttachment(2025, 3, "zomato", date, "receipt.pdf", []byte(fmt.Sprintf("data-%d", i)))
		}(i)
	}

	close(start)
	wg.Wait()

	seen := make(map[string]struct{}, writes)
	for i := 0; i < writes; i++ {
		if errs[i] != nil {
			t.Fatalf("write %d failed: %v", i, errs[i])
		}

		if _, exists := seen[paths[i]]; exists {
			t.Fatalf("duplicate output path generated: %s", paths[i])
		}
		seen[paths[i]] = struct{}{}
	}

	if len(seen) != writes {
		t.Fatalf("unique paths = %d, want %d", len(seen), writes)
	}
}

func TestWriteAttachment_ProviderPathSafety(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 5, 1, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		provider     string
		wantProvider string
	}{
		{name: "safe provider", provider: "amazon", wantProvider: "amazon"},
		{name: "path traversal", provider: "../../etc/passwd", wantProvider: "etcpasswd"},
		{name: "absolute path", provider: "/tmp/evil", wantProvider: "tmpevil"},
		{name: "empty after sanitization", provider: "../..", wantProvider: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, err := w.WriteAttachment(2025, 5, tc.provider, date, "invoice.pdf", []byte("data"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rel, err := filepath.Rel(dir, path)
			if err != nil {
				t.Fatalf("filepath.Rel failed: %v", err)
			}

			if strings.HasPrefix(rel, "..") {
				t.Fatalf("output escaped base dir: %q", rel)
			}

			parts := strings.Split(rel, string(filepath.Separator))
			if len(parts) != 4 {
				t.Fatalf("path parts = %v, want [year month provider file]", parts)
			}

			if parts[0] != "2025" || parts[1] != "05" {
				t.Fatalf("unexpected year/month in path: %v", parts[:2])
			}

			if parts[2] != tc.wantProvider {
				t.Errorf("provider dir = %q, want %q", parts[2], tc.wantProvider)
			}
		})
	}
}

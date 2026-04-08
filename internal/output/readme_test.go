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

func TestWritePasswordHint_NewFile(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)

	err := w.WritePasswordHint(2025, 1, "hdfcbank", "20250115_143022.pdf", "Password is your PAN number", "HDFC Credit Card Statement", date)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	readmePath := filepath.Join(dir, "2025", "01", "hdfcbank", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Password-Protected PDFs") {
		t.Error("missing header")
	}
	if !strings.Contains(content, "## 20250115_143022.pdf") {
		t.Error("missing PDF section")
	}
	if !strings.Contains(content, "Password is your PAN number") {
		t.Error("missing hint")
	}
	if !strings.Contains(content, "HDFC Credit Card Statement") {
		t.Error("missing email subject")
	}

	info, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("stat README: %v", err)
	}
	if got := info.Mode().Perm(); got != outputFilePerm {
		t.Errorf("README mode = %#o, want %#o", got, outputFilePerm)
	}
}

func TestWritePasswordHint_AppendDifferentPDF(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	date1 := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)
	date2 := time.Date(2025, 1, 20, 10, 0, 0, 0, time.UTC)

	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "20250115_143022.pdf", "Password is your PAN number", "Jan Statement", date1); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "20250120_100000.pdf", "Password is DOB in DDMMYYYY", "Special Statement", date2); err != nil {
		t.Fatal(err)
	}

	readmePath := filepath.Join(dir, "2025", "01", "hdfcbank", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "## 20250115_143022.pdf") {
		t.Error("missing first PDF entry")
	}
	if !strings.Contains(content, "## 20250120_100000.pdf") {
		t.Error("missing second PDF entry")
	}
	if !strings.Contains(content, "PAN number") {
		t.Error("missing first hint")
	}
	if !strings.Contains(content, "DOB in DDMMYYYY") {
		t.Error("missing second hint")
	}
}

func TestWritePasswordHint_Deduplication(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)

	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "20250115_143022.pdf", "PAN number", "Statement", date); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "20250115_143022.pdf", "PAN number", "Statement", date); err != nil {
		t.Fatal(err)
	}

	readmePath := filepath.Join(dir, "2025", "01", "hdfcbank", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	count := strings.Count(content, "## 20250115_143022.pdf")
	if count != 1 {
		t.Errorf("PDF section appears %d times, want 1", count)
	}
}

func TestWritePasswordHint_DeduplicationWithFilenamePrefix(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)

	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "statement.pdf.encrypted", "PAN number", "Statement (encrypted)", date); err != nil {
		t.Fatal(err)
	}
	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "statement.pdf", "DOB", "Statement", date); err != nil {
		t.Fatal(err)
	}

	readmePath := filepath.Join(dir, "2025", "01", "hdfcbank", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if count := strings.Count(content, "## statement.pdf.encrypted\n"); count != 1 {
		t.Fatalf("encrypted section appears %d times, want 1", count)
	}
	if count := strings.Count(content, "## statement.pdf\n"); count != 1 {
		t.Fatalf("plain section appears %d times, want 1", count)
	}
}

func TestWritePasswordHint_DeduplicationTightensReusedPathPermissions(t *testing.T) {
	dir := t.TempDir()
	yearDir := filepath.Join(dir, "2025")
	monthDir := filepath.Join(yearDir, "01")
	providerDir := filepath.Join(monthDir, "hdfcbank")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatalf("creating provider dir: %v", err)
	}

	for _, path := range []string{yearDir, monthDir, providerDir} {
		if err := os.Chmod(path, 0o755); err != nil {
			t.Fatalf("chmod %s: %v", path, err)
		}
	}

	readmePath := filepath.Join(providerDir, "README.md")
	content := "# Password-Protected PDFs\n\n## statement.pdf\nPassword hint: \"PAN number\"\nSource email: \"Statement\" (2025-01-15)\n\n"
	if err := os.WriteFile(readmePath, []byte(content), 0o644); err != nil {
		t.Fatalf("creating readme: %v", err)
	}
	if err := os.Chmod(readmePath, 0o644); err != nil {
		t.Fatalf("chmod readme: %v", err)
	}

	w := NewWriter(dir)
	date := time.Date(2025, 1, 15, 14, 30, 22, 0, time.UTC)
	if err := w.WritePasswordHint(2025, 1, "hdfcbank", "statement.pdf", "PAN number", "Statement", date); err != nil {
		t.Fatalf("write password hint: %v", err)
	}

	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if count := strings.Count(string(data), "## statement.pdf\n"); count != 1 {
		t.Fatalf("section appears %d times, want 1", count)
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

	info, err := os.Stat(readmePath)
	if err != nil {
		t.Fatalf("stat readme: %v", err)
	}
	if got := info.Mode().Perm(); got != outputFilePerm {
		t.Fatalf("README mode = %#o, want %#o", got, outputFilePerm)
	}
}

func TestWritePasswordHint_ConcurrentUpdates(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	date := time.Date(2025, 2, 1, 9, 0, 0, 0, time.UTC)

	const entries = 20
	errs := make([]error, entries)

	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < entries; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = w.WritePasswordHint(
				2025,
				2,
				"hdfcbank",
				fmt.Sprintf("20250201_090000_%d.pdf", i),
				fmt.Sprintf("hint-%d", i),
				fmt.Sprintf("subject-%d", i),
				date,
			)
		}(i)
	}

	close(start)
	wg.Wait()

	for i := 0; i < entries; i++ {
		if errs[i] != nil {
			t.Fatalf("write %d failed: %v", i, errs[i])
		}
	}

	readmePath := filepath.Join(dir, "2025", "02", "hdfcbank", "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}

	content := string(data)
	for i := 0; i < entries; i++ {
		header := fmt.Sprintf("## 20250201_090000_%d.pdf", i)
		if count := strings.Count(content, header); count != 1 {
			t.Fatalf("header %q appears %d times, want 1", header, count)
		}
	}
}

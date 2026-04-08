package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	outputDirPerm  os.FileMode = 0o700
	outputFilePerm os.FileMode = 0o600
)

// FileWriter defines the operations for writing output files.
// Implemented by Writer; can be mocked for testing.
type FileWriter interface {
	WriteAttachment(year, month int, provider string, emailDate time.Time, originalFilename string, data []byte) (string, error)
	WritePasswordHint(year, month int, provider string, pdfFilename string, hint string, emailSubject string, emailDate time.Time) error
}

// Writer handles writing files to the output directory structure.
type Writer struct {
	BaseDir string
	mu      sync.Mutex
}

func NewWriter(baseDir string) *Writer {
	return &Writer{BaseDir: baseDir}
}

// WriteAttachment saves an attachment to: <base>/<year>/<month>/<provider>/<datetime.ext>
// Returns the full path of the written file.
func (w *Writer) WriteAttachment(year, month int, provider string, emailDate time.Time, originalFilename string, data []byte) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir, err := w.ensurePrivateProviderDir(year, month, provider)
	if err != nil {
		return "", err
	}

	filename := buildFilename(emailDate, originalFilename, dir)
	fullPath := filepath.Join(dir, filename)

	if err := os.WriteFile(fullPath, data, outputFilePerm); err != nil {
		return "", fmt.Errorf("writing file %s: %w", fullPath, err)
	}
	if err := ensurePrivateFilePerm(fullPath); err != nil {
		return "", err
	}

	return fullPath, nil
}

func (w *Writer) ensurePrivateProviderDir(year, month int, provider string) (string, error) {
	yearDir := filepath.Join(w.BaseDir, fmt.Sprintf("%d", year))
	monthDir := filepath.Join(yearDir, fmt.Sprintf("%02d", month))
	dir := w.providerDir(year, month, provider)

	if err := os.MkdirAll(dir, outputDirPerm); err != nil {
		return "", fmt.Errorf("creating directory %s: %w", dir, err)
	}

	for _, path := range []string{yearDir, monthDir, dir} {
		if err := os.Chmod(path, outputDirPerm); err != nil {
			return "", fmt.Errorf("setting directory permissions %s: %w", path, err)
		}
	}

	return dir, nil
}

func ensurePrivateFilePerm(path string) error {
	if err := os.Chmod(path, outputFilePerm); err != nil {
		return fmt.Errorf("setting file permissions %s: %w", path, err)
	}

	return nil
}

func (w *Writer) providerDir(year, month int, provider string) string {
	return filepath.Join(w.BaseDir, fmt.Sprintf("%d", year), fmt.Sprintf("%02d", month), sanitizeProvider(provider))
}

func sanitizeProvider(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))

	var out strings.Builder
	for _, r := range provider {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}

	clean := strings.Trim(out.String(), "-")
	if clean == "" {
		return "unknown"
	}

	return clean
}

func buildFilename(emailDate time.Time, originalFilename string, dir string) string {
	ext := filepath.Ext(originalFilename)
	if ext == "" {
		ext = ".bin"
	}
	ext = strings.ToLower(ext)

	base := emailDate.Format("20060102_150405")
	candidate := base + ext

	if !fileExists(filepath.Join(dir, candidate)) {
		return candidate
	}

	for i := 2; i <= 10000; i++ {
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
		if !fileExists(filepath.Join(dir, candidate)) {
			return candidate
		}
	}

	// Fallback: use unix timestamp to guarantee uniqueness
	return fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

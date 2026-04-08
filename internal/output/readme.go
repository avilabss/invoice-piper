package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WritePasswordHint appends a password hint entry to the provider's README.md.
// Skips if the same PDF filename already has an entry.
func (w *Writer) WritePasswordHint(year, month int, provider string, pdfFilename string, hint string, emailSubject string, emailDate time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	dir, err := w.ensurePrivateProviderDir(year, month, provider)
	if err != nil {
		return err
	}

	readmePath := filepath.Join(dir, "README.md")

	existing, err := os.ReadFile(readmePath)
	readmeExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", readmePath, err)
	}

	content := string(existing)

	// Skip if this PDF already has an entry
	if hasPDFSection(content, pdfFilename) {
		if readmeExists {
			if err := ensurePrivateFilePerm(readmePath); err != nil {
				return err
			}
		}
		return nil
	}

	// Add header if new file
	if len(existing) == 0 {
		content = "# Password-Protected PDFs\n\n"
	}

	entry := fmt.Sprintf("## %s\nPassword hint: %q\nSource email: %q (%s)\n\n",
		pdfFilename,
		hint,
		emailSubject,
		emailDate.Format("2006-01-02"),
	)
	content += entry

	if err := os.WriteFile(readmePath, []byte(content), outputFilePerm); err != nil {
		return err
	}

	return ensurePrivateFilePerm(readmePath)
}

func hasPDFSection(content string, pdfFilename string) bool {
	target := "## " + pdfFilename
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSuffix(line, "\r") == target {
			return true
		}
	}

	return false
}

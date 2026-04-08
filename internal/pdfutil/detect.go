package pdfutil

import (
	"bytes"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// IsPasswordProtected checks if the given PDF data is password-locked.
// Returns false for non-PDF data and corrupt PDFs that aren't encrypted.
func IsPasswordProtected(data []byte) bool {
	if !IsPDF(data) {
		return false
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	ctx, err := api.ReadContext(bytes.NewReader(data), conf)
	if err != nil {
		// pdfcpu returns a "password" error when the PDF requires
		// a user password to open. This means it's encrypted.
		return strings.Contains(err.Error(), "password")
	}

	// Owner-password-only PDFs can be read but have an encryption dict.
	return ctx.Encrypt != nil
}

// IsPDF checks if the data looks like a PDF by examining the magic bytes.
func IsPDF(data []byte) bool {
	return len(data) >= 5 && string(data[:5]) == "%PDF-"
}

package pdfutil

import (
	"bytes"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func TestIsPDF(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{"valid pdf header", []byte("%PDF-1.4 rest of content"), true},
		{"pdf 1.7", []byte("%PDF-1.7\n"), true},
		{"not a pdf", []byte("Hello World"), false},
		{"empty", []byte{}, false},
		{"short", []byte("%PD"), false},
		{"png header", []byte("\x89PNG\r\n\x1a\n"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPDF(tt.data)
			if result != tt.expected {
				t.Errorf("IsPDF() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsPasswordProtected_NotPDF(t *testing.T) {
	result := IsPasswordProtected([]byte("not a pdf"))
	if result {
		t.Error("expected false for non-PDF data")
	}
}

func TestIsPasswordProtected_CorruptPDF(t *testing.T) {
	result := IsPasswordProtected([]byte("%PDF-1.4 this is not a valid pdf"))
	if result {
		t.Error("expected false for corrupt PDF")
	}
}

func createMinimalPDF(t *testing.T) []byte {
	t.Helper()
	conf := model.NewDefaultConfiguration()
	ctx, err := pdfcpu.CreateContextWithXRefTable(conf, types.PaperSize["A4"])
	if err != nil {
		t.Fatalf("creating PDF context: %v", err)
	}
	var buf bytes.Buffer
	if err := api.WriteContext(ctx, &buf); err != nil {
		t.Fatalf("writing PDF: %v", err)
	}
	return buf.Bytes()
}

func TestIsPasswordProtected_UnencryptedPDF(t *testing.T) {
	pdf := createMinimalPDF(t)
	if !IsPDF(pdf) {
		t.Fatal("generated data should be valid PDF")
	}
	if IsPasswordProtected(pdf) {
		t.Error("unencrypted PDF should not be password-protected")
	}
}

func TestIsPasswordProtected_EncryptedPDF(t *testing.T) {
	plainPDF := createMinimalPDF(t)

	encConf := model.NewAESConfiguration("secret", "secret", 256)
	var out bytes.Buffer
	if err := api.Encrypt(bytes.NewReader(plainPDF), &out, encConf); err != nil {
		t.Fatalf("encrypting PDF: %v", err)
	}

	encPDF := out.Bytes()
	if !IsPDF(encPDF) {
		t.Fatal("encrypted data should still be valid PDF")
	}
	if !IsPasswordProtected(encPDF) {
		t.Error("encrypted PDF should be detected as password-protected")
	}
}

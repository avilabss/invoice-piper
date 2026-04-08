package email

import (
	"strings"
	"testing"
)

func TestExtractPasswordHint(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		contains string
		fallback bool
	}{
		{
			name:     "PAN number pattern",
			body:     "Dear Customer,\nYour statement is attached.\nPassword is your PAN number.\nRegards, HDFC Bank",
			contains: "Password is your PAN number",
		},
		{
			name:     "DOB pattern",
			body:     "Please find attached your statement.\nThe password is your DOB in DDMMYYYY format.\nThank you.",
			contains: "DOB in DDMMYYYY format",
		},
		{
			name:     "passcode pattern",
			body:     "Your invoice is attached.\nPasscode is: ABC123\nContact support for help.",
			contains: "Passcode is: ABC123",
		},
		{
			name:     "unlock pattern",
			body:     "Statement attached.\nTo open this document, use your registered mobile number.\nThank you.",
			contains: "To open this document",
		},
		{
			name:     "protected with pattern",
			body:     "File protected by your date of birth in DDMMYYYY format.\nPlease check.",
			contains: "protected by your date of birth",
		},
		{
			name:     "empty body",
			body:     "",
			fallback: true,
		},
		{
			name:     "no password info",
			body:     "Dear Customer,\nPlease find your monthly statement attached.\nRegards, Bank",
			fallback: true,
		},
	}

	fallbackMsg := "This PDF is password-protected. Check the original email for unlock instructions."

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractPasswordHint(tt.body)

			if tt.fallback {
				if result != fallbackMsg {
					t.Errorf("expected fallback message, got %q", result)
				}
				return
			}

			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("result %q does not contain %q", result, tt.contains)
			}
		})
	}
}

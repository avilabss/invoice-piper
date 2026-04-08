package resolver

import "testing"

func TestResolve(t *testing.T) {
	// Reset user aliases for base tests
	SetAliases(nil)

	tests := []struct {
		name     string
		email    string
		expected string
	}{
		// Standard domains
		{"simple domain", "noreply@amazon.in", "amazon"},
		{"com domain", "receipts@uber.com", "uber"},
		{"subdomain stripped", "no-reply@email.zomato.com", "zomato"},
		{"billing subdomain", "billing@openai.com", "openai"},
		{"deep subdomain", "noreply@mail.billing.swiggy.in", "swiggy"},

		// Compound TLDs (handled by publicsuffix)
		{"co.uk", "noreply@amazon.co.uk", "amazon"},
		{"co.in", "noreply@flipkart.co.in", "flipkart"},
		{"com.au", "billing@example.com.au", "example"},
		{"co.in with subdomain", "noreply@mail.amazon.co.in", "amazon"},

		// Overrides
		{"gmail override", "someone@gmail.com", "google"},
		{"outlook override", "someone@outlook.com", "microsoft"},
		{"hotmail override", "someone@hotmail.com", "microsoft"},
		{"googleusercontent", "noreply@googleusercontent.com", "google"},

		// RFC 5322 format
		{"display name", "Amazon <noreply@amazon.com>", "amazon"},
		{"quoted display", `"Uber Receipts" <receipts@uber.com>`, "uber"},

		// Edge cases
		{"empty string", "", "unknown"},
		{"no at sign", "notanemail", "unknown"},
		{"bank domain", "alerts@hdfcbank.com", "hdfcbank"},
		{"hyphenated", "noreply@my-company.com", "my-company"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(tt.email)
			if result != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.email, result, tt.expected)
			}
		})
	}
}

func TestResolve_UserAliases(t *testing.T) {
	defer SetAliases(nil) // cleanup

	SetAliases(map[string]string{
		"custom-billing.com":       "mycorp",
		"notifications.stripe.com": "stripe-payments",
	})

	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{"user alias exact domain", "noreply@custom-billing.com", "mycorp"},
		{"user alias full subdomain match", "noreply@notifications.stripe.com", "stripe-payments"},
		{"user alias takes priority over builtin", "noreply@custom-billing.com", "mycorp"},
		{"builtin still works", "someone@gmail.com", "google"},
		{"normal resolution unaffected", "noreply@uber.com", "uber"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(tt.email)
			if result != tt.expected {
				t.Errorf("Resolve(%q) = %q, want %q", tt.email, result, tt.expected)
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Amazon", "amazon"},
		{"my-company", "my-company"},
		{"weird!@#name", "weirdname"},
		{"-leading-dash-", "leading-dash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitize(tt.input)
			if result != tt.expected {
				t.Errorf("sanitize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

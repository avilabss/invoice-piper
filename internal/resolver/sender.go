package resolver

import (
	"net/mail"
	"strings"
	"sync"

	"golang.org/x/net/publicsuffix"
)

// builtinAliases maps specific domains to provider names when domain extraction produces poor results.
var builtinAliases = map[string]string{
	"googleusercontent.com": "google",
	"gstatic.com":           "google",
	"gmail.com":             "google",
	"outlook.com":           "microsoft",
	"hotmail.com":           "microsoft",
	"live.com":              "microsoft",
}

var (
	mu          sync.RWMutex
	userAliases map[string]string
)

// SetAliases configures user-provided domain→provider overrides from config.
// These take priority over built-in aliases. Safe for concurrent use.
func SetAliases(aliases map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	userAliases = aliases
}

// getUserAlias returns the user alias for a domain, if any.
func getUserAlias(domain string) (string, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if userAliases == nil {
		return "", false
	}
	name, ok := userAliases[domain]
	return name, ok
}

// Resolve takes a sender email address and returns a cleaned provider name.
// Example: "noreply@billing.zomato.com" → "zomato"
// Safe for concurrent use.
func Resolve(senderEmail string) string {
	domain := extractDomain(senderEmail)
	if domain == "" {
		return "unknown"
	}

	// User aliases have highest priority (checked against full domain)
	if name, ok := getUserAlias(domain); ok {
		return sanitize(name)
	}

	// Check built-in aliases for full domain
	if name, ok := builtinAliases[domain]; ok {
		return name
	}

	// Use publicsuffix to get the eTLD+1 (registrable domain)
	registrable, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		// Fallback: just return the domain sanitized
		return sanitize(domain)
	}

	// Check user aliases for registrable domain
	if name, ok := getUserAlias(registrable); ok {
		return sanitize(name)
	}

	// Check built-in aliases for registrable domain
	if name, ok := builtinAliases[registrable]; ok {
		return name
	}

	// Extract the name part (everything before the eTLD)
	name := strings.SplitN(registrable, ".", 2)[0]
	return sanitize(name)
}

func extractDomain(email string) string {
	// Try parsing as a full RFC 5322 address
	addr, err := mail.ParseAddress(email)
	if err == nil {
		email = addr.Address
	}

	// Extract domain part after @
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return strings.Trim(result.String(), "-")
}

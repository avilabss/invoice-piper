package email

import (
	"regexp"
	"strings"
)

// passwordPatterns matches lines that likely contain password/unlock information.
var passwordPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)password\s*(is|:)`),
	regexp.MustCompile(`(?i)passcode\s*(is|:)`),
	regexp.MustCompile(`(?i)unlock\s*(with|using|code|:)`),
	regexp.MustCompile(`(?i)protected\s*(with|by|:)`),
	regexp.MustCompile(`(?i)\bpin\b\s*(is|:)`),
	regexp.MustCompile(`(?i)to\s+open\s+(this|the|your)`),
	regexp.MustCompile(`(?i)dob\s*(in|format)`),
	regexp.MustCompile(`(?i)date\s+of\s+birth`),
	regexp.MustCompile(`(?i)\bpan\b.*(number|card)`),
}

// ExtractPasswordHint scans the email text body for password-related information.
// Returns the extracted hint lines, or a fallback message if no pattern matches.
func ExtractPasswordHint(textBody string) string {
	if textBody == "" {
		return "This PDF is password-protected. Check the original email for unlock instructions."
	}

	lines := strings.Split(textBody, "\n")
	var hints []string

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		for _, pattern := range passwordPatterns {
			if pattern.MatchString(line) {
				// Include the matching line and the next line for context
				hints = append(hints, line)
				if i+1 < len(lines) {
					next := strings.TrimSpace(lines[i+1])
					if next != "" && !isDuplicate(hints, next) {
						hints = append(hints, next)
					}
				}
				break
			}
		}
	}

	if len(hints) == 0 {
		return "This PDF is password-protected. Check the original email for unlock instructions."
	}

	return strings.Join(hints, "\n")
}

func isDuplicate(existing []string, candidate string) bool {
	for _, s := range existing {
		if s == candidate {
			return true
		}
	}
	return false
}

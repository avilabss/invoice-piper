package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	configFileName  = "config.json"
	localConfigPath = "./config.json"
	appConfigDir    = "invoice-piper"

	imapSecurityIMAPS    = "imaps"
	imapSecuritySTARTTLS = "starttls"
	imapSecurityPlain    = "plain"
)

var (
	userConfigDir = os.UserConfigDir
	lookupEnv     = os.LookupEnv
)

type Config struct {
	OutputDir       string            `json:"output_dir"`
	ProviderAliases map[string]string `json:"provider_aliases"`
	Email           EmailConfig       `json:"email"`
}

type EmailConfig struct {
	Accounts []IMAPAccount `json:"accounts"`
}

type IMAPAccount struct {
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Security      string   `json:"security"`
	Port          int      `json:"port"`
	TLSSkipVerify bool     `json:"tls_skip_verify"`
	Username      string   `json:"username"`
	PasswordEnv   string   `json:"password_env"`
	Password      string   `json:"password"`
	Mailboxes     []string `json:"mailboxes"`

	securitySet      bool
	portSet          bool
	tlsSkipVerifySet bool
}

func (a *IMAPAccount) UnmarshalJSON(data []byte) error {
	type rawIMAPAccount struct {
		Name          string   `json:"name"`
		Host          string   `json:"host"`
		Security      string   `json:"security"`
		Port          int      `json:"port"`
		TLSSkipVerify *bool    `json:"tls_skip_verify"`
		Username      string   `json:"username"`
		PasswordEnv   string   `json:"password_env"`
		Password      string   `json:"password"`
		Mailboxes     []string `json:"mailboxes"`
	}

	var raw rawIMAPAccount
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected extra JSON content")
		}
		return err
	}

	a.Name = raw.Name
	a.Host = raw.Host
	a.Security = raw.Security
	a.Port = raw.Port
	a.Username = raw.Username
	a.PasswordEnv = raw.PasswordEnv
	a.Password = raw.Password
	a.Mailboxes = raw.Mailboxes

	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return err
	}
	_, a.securitySet = rawFields["security"]
	_, a.portSet = rawFields["port"]

	a.tlsSkipVerifySet = raw.TLSSkipVerify != nil
	a.TLSSkipVerify = false
	if raw.TLSSkipVerify != nil {
		a.TLSSkipVerify = *raw.TLSSkipVerify
	}

	return nil
}

// Load reads the config from the given path, or searches default locations.
// Resolution order: explicit path → ./config.json → os.UserConfigDir()/invoice-piper/config.json
func Load(path string) (*Config, error) {
	if path != "" {
		return loadFrom(path)
	}

	candidates := defaultConfigCandidates()

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return loadFrom(candidate)
		}
	}

	return nil, fmt.Errorf("config file not found; create %s (see config.example.json)", DefaultConfigSearchPathHint())
}

func loadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parsing config %s: unexpected extra JSON content", path)
		}
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	cfg.applyDefaults()

	if err := cfg.resolveSecrets(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if len(c.Email.Accounts) == 0 {
		return fmt.Errorf("no email accounts configured")
	}

	for i, acc := range c.Email.Accounts {
		if acc.Host == "" {
			return fmt.Errorf("email.accounts[%d]: host is required", i)
		}
		switch acc.Security {
		case imapSecurityIMAPS, imapSecuritySTARTTLS, imapSecurityPlain:
			// Valid values.
		default:
			return fmt.Errorf("email.accounts[%d]: security must be one of %q, %q, or %q", i, imapSecurityIMAPS, imapSecuritySTARTTLS, imapSecurityPlain)
		}

		if acc.Port == 0 {
			return fmt.Errorf("email.accounts[%d]: port is required when security is %q", i, acc.Security)
		}

		if acc.Security == imapSecurityPlain && acc.tlsSkipVerifySet {
			return fmt.Errorf("email.accounts[%d]: tls_skip_verify is not allowed when security is %q", i, imapSecurityPlain)
		}

		if acc.Username == "" {
			return fmt.Errorf("email.accounts[%d]: username is required", i)
		}
		if acc.Password == "" {
			return fmt.Errorf("email.accounts[%d]: password is required (set password_env or password)", i)
		}
	}

	return nil
}

func (c *Config) resolveSecrets() error {
	for i := range c.Email.Accounts {
		acc := &c.Email.Accounts[i]

		if acc.PasswordEnv != "" && acc.Password != "" {
			return fmt.Errorf("email.accounts[%d]: set exactly one of password_env or password (prefer password_env); both were provided", i)
		}

		if acc.PasswordEnv == "" {
			if acc.Password == "" {
				return fmt.Errorf("email.accounts[%d]: password is required (set password_env or password)", i)
			}
			continue
		}

		password, ok := lookupEnv(acc.PasswordEnv)
		if !ok || password == "" {
			return fmt.Errorf("email.accounts[%d]: password_env %q is not set or empty", i, acc.PasswordEnv)
		}

		acc.Password = password
	}

	return nil
}

// DefaultConfigSearchPathHint returns the default discovery paths for help text.
func DefaultConfigSearchPathHint() string {
	return strings.Join(defaultConfigCandidates(), " or ")
}

func defaultConfigCandidates() []string {
	candidates := []string{localConfigPath}

	if dir, err := userConfigDir(); err == nil && dir != "" {
		candidates = append(candidates, filepath.Join(dir, appConfigDir, configFileName))
	}

	return candidates
}

func (c *Config) applyDefaults() {
	if c.OutputDir == "" {
		c.OutputDir = "./invoices"
	}

	for i := range c.Email.Accounts {
		acc := &c.Email.Accounts[i]
		if !acc.securitySet && acc.Security == "" {
			acc.Security = imapSecurityIMAPS
		}

		if !acc.portSet && acc.Port == 0 && acc.Security == imapSecurityIMAPS {
			acc.Port = 993
		}
		if len(acc.Mailboxes) == 0 {
			acc.Mailboxes = []string{"INBOX"}
		}
		if acc.Name == "" {
			acc.Name = acc.Username
		}
	}
}

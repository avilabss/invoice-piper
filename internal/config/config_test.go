package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
}

func stubUserConfigDir(t *testing.T, dir string) {
	t.Helper()
	old := userConfigDir
	userConfigDir = func() (string, error) {
		return dir, nil
	}
	t.Cleanup(func() {
		userConfigDir = old
	})
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"output_dir": "./my-invoices",
		"email": {
			"accounts": [
				{
					"name": "test",
					"host": "127.0.0.1",
					"security": "starttls",
					"port": 1143,
					"tls_skip_verify": true,
					"username": "user@gmail.com",
					"password": "secret",
					"mailboxes": ["INBOX", "[Gmail]/Trash"]
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OutputDir != "./my-invoices" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "./my-invoices")
	}
	if len(cfg.Email.Accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(cfg.Email.Accounts))
	}
	acc := cfg.Email.Accounts[0]
	if acc.Name != "test" {
		t.Errorf("Name = %q, want %q", acc.Name, "test")
	}
	if acc.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", acc.Host, "127.0.0.1")
	}
	if acc.Security != "starttls" {
		t.Errorf("Security = %q, want %q", acc.Security, "starttls")
	}
	if acc.Port != 1143 {
		t.Errorf("Port = %d, want 1143", acc.Port)
	}
	if !acc.TLSSkipVerify {
		t.Error("TLSSkipVerify = false, want true")
	}
	if len(acc.Mailboxes) != 2 {
		t.Errorf("Mailboxes = %v, want 2 entries", acc.Mailboxes)
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.example.com",
					"username": "user@example.com",
					"password": "pass"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OutputDir != "./invoices" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "./invoices")
	}

	acc := cfg.Email.Accounts[0]
	if acc.Security != "imaps" {
		t.Errorf("Security = %q, want %q", acc.Security, "imaps")
	}
	if acc.Port != 993 {
		t.Errorf("Port = %d, want 993", acc.Port)
	}
	if len(acc.Mailboxes) != 1 || acc.Mailboxes[0] != "INBOX" {
		t.Errorf("Mailboxes = %v, want [INBOX]", acc.Mailboxes)
	}
	if acc.Name != "user@example.com" {
		t.Errorf("Name = %q, want %q", acc.Name, "user@example.com")
	}
}

func TestLoad_ExplicitAllMailboxIsPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.example.com",
					"username": "user@example.com",
					"password": "pass",
					"mailboxes": ["ALL"]
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acc := cfg.Email.Accounts[0]
	if len(acc.Mailboxes) != 1 || acc.Mailboxes[0] != "ALL" {
		t.Errorf("Mailboxes = %v, want [ALL]", acc.Mailboxes)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeTestConfig(t, cfgPath, `{invalid`)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "no accounts",
			json: `{"email": {"accounts": []}}`,
		},
		{
			name: "missing host",
			json: `{"email": {"accounts": [{"username": "u", "password": "p"}]}}`,
		},
		{
			name: "missing username",
			json: `{"email": {"accounts": [{"host": "h", "password": "p"}]}}`,
		},
		{
			name: "missing password and password_env",
			json: `{"email": {"accounts": [{"host": "h", "username": "u"}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.json")
			writeTestConfig(t, cfgPath, tt.json)

			_, err := Load(cfgPath)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestLoad_TransportValidationErrors(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		wantErrContains string
	}{
		{
			name: "unknown security value",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "tls", "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: "security must be one of",
		},
		{
			name: "explicit empty security value is rejected",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "", "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: "security must be one of",
		},
		{
			name: "starttls requires explicit port",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "starttls", "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: `port is required when security is "starttls"`,
		},
		{
			name: "plain requires explicit port",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "plain", "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: `port is required when security is "plain"`,
		},
		{
			name: "imaps rejects explicit zero port",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "imaps", "port": 0, "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: `port is required when security is "imaps"`,
		},
		{
			name: "plain rejects tls_skip_verify true",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "plain", "port": 143, "tls_skip_verify": true, "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: "tls_skip_verify is not allowed",
		},
		{
			name: "plain rejects tls_skip_verify false",
			json: `{
				"email": {
					"accounts": [
						{"host": "imap.example.com", "security": "plain", "port": 143, "tls_skip_verify": false, "username": "u", "password": "p"}
					]
				}
			}`,
			wantErrContains: "tls_skip_verify is not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.json")
			writeTestConfig(t, cfgPath, tt.json)

			_, err := Load(cfgPath)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrContains, err)
			}
		})
	}
}

func TestLoad_ValidPlainSecurityConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "127.0.0.1",
					"security": "plain",
					"port": 143,
					"username": "bridge-user",
					"password": "bridge-pass"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	acc := cfg.Email.Accounts[0]
	if acc.Security != "plain" {
		t.Fatalf("Security = %q, want %q", acc.Security, "plain")
	}
	if acc.Port != 143 {
		t.Fatalf("Port = %d, want %d", acc.Port, 143)
	}
}

func TestLoad_StrictUnknownFields(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{
			name: "unknown top-level field",
			json: `{
				"email": {"accounts": [{"host": "h", "username": "u", "password": "p"}]},
				"unexpected": true
			}`,
		},
		{
			name: "unknown nested field",
			json: `{
				"email": {"accounts": [{"host": "h", "username": "u", "password": "p", "unexpected": true}]}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.json")
			writeTestConfig(t, cfgPath, tt.json)

			_, err := Load(cfgPath)
			if err == nil {
				t.Fatal("expected strict parsing error")
			}
			if !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("expected unknown field error, got %v", err)
			}
		})
	}
}

func TestLoad_RejectsLegacyTLSField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.gmail.com",
					"username": "user@gmail.com",
					"password": "secret",
					"tls": true
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for unsupported tls field")
	}
	if !strings.Contains(err.Error(), `unknown field "tls"`) {
		t.Fatalf("expected unknown tls field error, got %v", err)
	}
}

func TestLoad_ResolvesPasswordFromEnv(t *testing.T) {
	const envVar = "INVP_TEST_IMAP_PASSWORD"
	t.Setenv(envVar, "env-secret")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.gmail.com",
					"username": "user@gmail.com",
					"password_env": "INVP_TEST_IMAP_PASSWORD"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.Email.Accounts[0].Password; got != "env-secret" {
		t.Fatalf("resolved password = %q, want %q", got, "env-secret")
	}
}

func TestLoad_PlaintextPasswordFallback(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.gmail.com",
					"username": "user@gmail.com",
					"password": "legacy-secret"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.Email.Accounts[0].Password; got != "legacy-secret" {
		t.Fatalf("password = %q, want %q", got, "legacy-secret")
	}
}

func TestLoad_RejectsAmbiguousSecretConfig(t *testing.T) {
	const envVar = "INVP_TEST_AMBIGUOUS_SECRET"
	t.Setenv(envVar, "env-secret")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.gmail.com",
					"username": "user@gmail.com",
					"password_env": "INVP_TEST_AMBIGUOUS_SECRET",
					"password": "legacy-secret"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected ambiguous secret config error")
	}
	if !strings.Contains(err.Error(), "exactly one of password_env or password") {
		t.Fatalf("expected mutually exclusive secret source error, got %v", err)
	}
}

func TestLoad_PasswordEnvMissing(t *testing.T) {
	const envVar = "INVP_TEST_MISSING_IMAP_PASSWORD"
	t.Setenv(envVar, "")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{
		"email": {
			"accounts": [
				{
					"host": "imap.gmail.com",
					"username": "user@gmail.com",
					"password_env": "INVP_TEST_MISSING_IMAP_PASSWORD"
				}
			]
		}
	}`
	writeTestConfig(t, cfgPath, data)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected missing password_env error")
	}
	if !strings.Contains(err.Error(), "password_env") {
		t.Fatalf("expected password_env error, got %v", err)
	}
}

func TestLoad_AutoDiscovery_CurrentDirPreferred(t *testing.T) {
	workDir := t.TempDir()
	userConfigRoot := t.TempDir()

	stubUserConfigDir(t, userConfigRoot)
	chdirForTest(t, workDir)

	writeTestConfig(t, filepath.Join(workDir, configFileName), `{
		"email": {
			"accounts": [
				{"host": "imap.gmail.com", "username": "local@example.com", "password": "local-pass"}
			]
		}
	}`)

	userConfigPath := filepath.Join(userConfigRoot, appConfigDir, configFileName)
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, userConfigPath, `{
		"email": {
			"accounts": [
				{"host": "imap.gmail.com", "username": "usercfg@example.com", "password": "usercfg-pass"}
			]
		}
	}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.Email.Accounts[0].Username; got != "local@example.com" {
		t.Fatalf("loaded username = %q, want %q", got, "local@example.com")
	}
}

func TestLoad_AutoDiscovery_UserConfigFallback(t *testing.T) {
	workDir := t.TempDir()
	userConfigRoot := t.TempDir()

	stubUserConfigDir(t, userConfigRoot)
	chdirForTest(t, workDir)

	userConfigPath := filepath.Join(userConfigRoot, appConfigDir, configFileName)
	if err := os.MkdirAll(filepath.Dir(userConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestConfig(t, userConfigPath, `{
		"email": {
			"accounts": [
				{"host": "imap.gmail.com", "username": "usercfg@example.com", "password": "usercfg-pass"}
			]
		}
	}`)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.Email.Accounts[0].Username; got != "usercfg@example.com" {
		t.Fatalf("loaded username = %q, want %q", got, "usercfg@example.com")
	}
}

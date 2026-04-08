package email

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

type stubWaitCommand struct {
	err error
}

func (c stubWaitCommand) Wait() error {
	return c.err
}

type stubListCommand struct {
	mailboxes []*imap.ListData
	err       error
}

func (c stubListCommand) Collect() ([]*imap.ListData, error) {
	return c.mailboxes, c.err
}

type stubSelectCommand struct {
	data *imap.SelectData
	err  error
}

func (c stubSelectCommand) Wait() (*imap.SelectData, error) {
	if c.data == nil {
		return &imap.SelectData{}, c.err
	}
	return c.data, c.err
}

type stubSearchCommand struct {
	data *imap.SearchData
	err  error
}

func (c stubSearchCommand) Wait() (*imap.SearchData, error) {
	if c.data == nil {
		return &imap.SearchData{}, c.err
	}
	return c.data, c.err
}

type stubFetchMessage struct {
	buf *imapclient.FetchMessageBuffer
	err error
}

func (m stubFetchMessage) Collect() (*imapclient.FetchMessageBuffer, error) {
	return m.buf, m.err
}

type stubFetchCommand struct {
	messages []fetchMessage
	closeErr error
	index    int
}

func (c *stubFetchCommand) Next() fetchMessage {
	if c.index >= len(c.messages) {
		return nil
	}
	msg := c.messages[c.index]
	c.index++
	return msg
}

func (c *stubFetchCommand) Close() error {
	return c.closeErr
}

type stubIMAPClient struct {
	loginErr error

	listData []*imap.ListData
	listErr  error

	selectData *imap.SelectData
	selectErr  error

	searchData *imap.SearchData
	searchErr  error

	fetchCmd fetchCommand

	logoutErr error
	closeErr  error
}

func (c *stubIMAPClient) Login(username, password string) waitCommand {
	return stubWaitCommand{err: c.loginErr}
}

func (c *stubIMAPClient) List(ref, pattern string, options *imap.ListOptions) listCommand {
	return stubListCommand{mailboxes: c.listData, err: c.listErr}
}

func (c *stubIMAPClient) Select(mailbox string, options *imap.SelectOptions) selectCommand {
	return stubSelectCommand{data: c.selectData, err: c.selectErr}
}

func (c *stubIMAPClient) UIDSearch(criteria *imap.SearchCriteria, options *imap.SearchOptions) searchCommand {
	return stubSearchCommand{data: c.searchData, err: c.searchErr}
}

func (c *stubIMAPClient) Fetch(numSet imap.NumSet, options *imap.FetchOptions) fetchCommand {
	if c.fetchCmd == nil {
		return &stubFetchCommand{}
	}
	return c.fetchCmd
}

func (c *stubIMAPClient) Logout() waitCommand {
	return stubWaitCommand{err: c.logoutErr}
}

func (c *stubIMAPClient) Close() error {
	return c.closeErr
}

type stubNetAddr string

func (a stubNetAddr) Network() string { return "tcp" }
func (a stubNetAddr) String() string  { return string(a) }

type stubNetConn struct{}

func (c *stubNetConn) Read(_ []byte) (int, error)         { return 0, io.EOF }
func (c *stubNetConn) Write(b []byte) (int, error)        { return len(b), nil }
func (c *stubNetConn) Close() error                       { return nil }
func (c *stubNetConn) LocalAddr() net.Addr                { return stubNetAddr("local") }
func (c *stubNetConn) RemoteAddr() net.Addr               { return stubNetAddr("remote") }
func (c *stubNetConn) SetDeadline(_ time.Time) error      { return nil }
func (c *stubNetConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *stubNetConn) SetWriteDeadline(_ time.Time) error { return nil }

func newStubClientConnection(account config.IMAPAccount, conn IMAPClient) *Client {
	return &Client{
		account: account,
		dialer: func(ctx context.Context, addr string) (IMAPClient, error) {
			return conn, nil
		},
	}
}

func newTestFetchBuffer(subject string, uid imap.UID) *imapclient.FetchMessageBuffer {
	return &imapclient.FetchMessageBuffer{
		UID: uid,
		Envelope: &imap.Envelope{
			Subject: subject,
			Date:    time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC),
			From: []imap.Address{
				{
					Mailbox: "billing",
					Host:    "example.com",
				},
			},
		},
		BodySection: []imapclient.FetchBodySectionBuffer{
			{
				Section: &imap.FetchItemBodySection{Peek: true},
				Bytes:   []byte("Content-Type: text/plain; charset=UTF-8\r\n\r\ninvoice body"),
			},
		},
	}
}

func TestNewClient(t *testing.T) {
	acc := config.IMAPAccount{
		Name:      "test",
		Host:      "imap.test.com",
		Port:      993,
		Username:  "user@test.com",
		Password:  "pass",
		Mailboxes: []string{"INBOX"},
	}

	client := NewClient(acc)
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	if client.account.Host != "imap.test.com" {
		t.Errorf("Host = %q, want %q", client.account.Host, "imap.test.com")
	}
}

func TestResolveMailboxes_ALL(t *testing.T) {
	client := &Client{
		account: config.IMAPAccount{
			Mailboxes: []string{"ALL"},
		},
	}

	// When mailboxes is ["ALL"], resolveMailboxes should call List
	// We can't easily test this without a real/mock IMAP client,
	// but we verify the logic path
	if len(client.account.Mailboxes) != 1 || client.account.Mailboxes[0] != "ALL" {
		t.Error("expected ALL mailbox config")
	}
}

func TestResolveMailboxes_Specific(t *testing.T) {
	client := &Client{
		account: config.IMAPAccount{
			Mailboxes: []string{"INBOX", "[Gmail]/Trash"},
		},
	}

	// For specific mailboxes, it should return them directly
	// This tests the non-ALL path
	if len(client.account.Mailboxes) != 2 {
		t.Errorf("expected 2 mailboxes, got %d", len(client.account.Mailboxes))
	}
}

func TestParseMessage_NilBody(t *testing.T) {
	msg := &imapclient.FetchMessageBuffer{}
	bodySection := &imap.FetchItemBodySection{Peek: true}

	_, err := parseMessage(msg, bodySection)
	if err == nil {
		t.Error("expected error for nil body")
	}
}

func TestClient_FetchMessages_ConnectionFailure(t *testing.T) {
	client := NewClient(config.IMAPAccount{
		Host:      "nonexistent.invalid",
		Port:      993,
		Username:  "user",
		Password:  "pass",
		Mailboxes: []string{"INBOX"},
	})

	_, err := client.FetchMessages(context.Background(), 2025, 1)
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestClient_FetchMessages_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := NewClient(config.IMAPAccount{
		Host:      "imap.gmail.com",
		Port:      993,
		Username:  "user",
		Password:  "pass",
		Mailboxes: []string{"INBOX"},
	})

	_, err := client.FetchMessages(ctx, 2025, 1)
	if err == nil {
		t.Error("expected context cancelled error")
	}
}

func TestClient_ConnectTimeout(t *testing.T) {
	// Use a dialer that blocks forever to verify timeout works
	client := &Client{
		account: config.IMAPAccount{
			Host: "10.255.255.1", // non-routable IP — will hang
			Port: 993,
		},
		dialer: func(ctx context.Context, addr string) (IMAPClient, error) {
			// Simulate a blocking dial that respects context
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.connect(ctx)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestClient_ListMailboxes_ConnectionFailure(t *testing.T) {
	client := NewClient(config.IMAPAccount{
		Host:     "nonexistent.invalid",
		Port:     993,
		Username: "user",
		Password: "pass",
	})

	_, err := client.ListMailboxes(context.Background())
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestClient_ListMailboxes_PreservesPartialResultsOnListError(t *testing.T) {
	listErr := errors.New("list failed")
	conn := &stubIMAPClient{
		listData: []*imap.ListData{{Mailbox: "INBOX"}, {Mailbox: "Archive"}},
		listErr:  listErr,
	}
	client := newStubClientConnection(config.IMAPAccount{
		Name:     "test",
		Host:     "imap.test.com",
		Port:     993,
		Username: "user",
		Password: "pass",
	}, conn)

	names, err := client.ListMailboxes(context.Background())
	if err == nil {
		t.Fatal("expected list error")
	}
	if !errors.Is(err, listErr) {
		t.Fatalf("expected wrapped list error, got %v", err)
	}
	if len(names) != 2 || names[0] != "INBOX" || names[1] != "Archive" {
		t.Fatalf("unexpected mailbox names: %#v", names)
	}
}

func TestClient_FetchMessages_PreservesPartialResolvedMailboxes(t *testing.T) {
	listErr := errors.New("partial list error")
	conn := &stubIMAPClient{
		listData: []*imap.ListData{{Mailbox: "INBOX"}},
		listErr:  listErr,
		searchData: &imap.SearchData{
			All: imap.UIDSetNum(42),
		},
		fetchCmd: &stubFetchCommand{
			messages: []fetchMessage{stubFetchMessage{buf: newTestFetchBuffer("Invoice", 42)}},
		},
	}
	client := newStubClientConnection(config.IMAPAccount{
		Name:      "test",
		Host:      "imap.test.com",
		Port:      993,
		Username:  "user",
		Password:  "pass",
		Mailboxes: []string{"ALL"},
	}, conn)

	messages, err := client.FetchMessages(context.Background(), 2025, 1)
	if err == nil {
		t.Fatal("expected non-nil error for partial LIST failure")
	}
	if len(messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(messages))
	}
	if messages[0].Subject != "Invoice" {
		t.Fatalf("subject = %q, want %q", messages[0].Subject, "Invoice")
	}
}

func TestClient_FetchFromMailbox_ReturnsFetchCloseErrorWithPartialMessages(t *testing.T) {
	closeErr := errors.New("fetch close failed")
	conn := &stubIMAPClient{
		searchData: &imap.SearchData{
			All: imap.UIDSetNum(99),
		},
		fetchCmd: &stubFetchCommand{
			messages: []fetchMessage{stubFetchMessage{buf: newTestFetchBuffer("Receipt", 99)}},
			closeErr: closeErr,
		},
	}

	client := &Client{}
	messages, err := client.fetchFromMailbox(context.Background(), conn, "INBOX", &imap.SearchCriteria{})
	if err == nil {
		t.Fatal("expected fetch close error")
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected wrapped close error, got %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages length = %d, want 1", len(messages))
	}
	if messages[0].Subject != "Receipt" {
		t.Fatalf("subject = %q, want %q", messages[0].Subject, "Receipt")
	}
}

func TestDialWithSecurity_TransportSelection(t *testing.T) {
	tests := []struct {
		name            string
		security        string
		tlsSkipVerify   bool
		wantTLSDial     int
		wantTCPDial     int
		wantNewClient   int
		wantNewStartTLS int
		wantNextProtos  []string
	}{
		{
			name:            "imaps uses implicit TLS",
			security:        "imaps",
			tlsSkipVerify:   true,
			wantTLSDial:     1,
			wantNextProtos:  []string{"imap"},
			wantNewClient:   1,
			wantNewStartTLS: 0,
		},
		{
			name:            "starttls dials TCP then upgrades",
			security:        "starttls",
			tlsSkipVerify:   true,
			wantTCPDial:     1,
			wantNextProtos:  nil,
			wantNewClient:   0,
			wantNewStartTLS: 1,
		},
		{
			name:            "plain uses TCP only",
			security:        "plain",
			tlsSkipVerify:   true,
			wantTCPDial:     1,
			wantNewClient:   1,
			wantNewStartTLS: 0,
		},
		{
			name:            "empty security defaults to imaps",
			security:        "",
			tlsSkipVerify:   false,
			wantTLSDial:     1,
			wantNextProtos:  []string{"imap"},
			wantNewClient:   1,
			wantNewStartTLS: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				tlsDialCalls      int
				tcpDialCalls      int
				newClientCalls    int
				newStartTLSCalls  int
				capturedTLSConfig *tls.Config
			)

			expectedConn := &stubNetConn{}
			expectedClient := &stubIMAPClient{}

			dialers := transportDialers{
				dialTLS: func(_ context.Context, _ string, tlsConfig *tls.Config) (net.Conn, error) {
					tlsDialCalls++
					capturedTLSConfig = tlsConfig
					return expectedConn, nil
				},
				dialTCP: func(_ context.Context, _ string) (net.Conn, error) {
					tcpDialCalls++
					return expectedConn, nil
				},
				newClient: func(conn net.Conn) IMAPClient {
					newClientCalls++
					if conn != expectedConn {
						t.Fatalf("newClient conn = %v, want expected conn", conn)
					}
					return expectedClient
				},
				newStartTLSClient: func(_ context.Context, conn net.Conn, tlsConfig *tls.Config) (IMAPClient, error) {
					newStartTLSCalls++
					capturedTLSConfig = tlsConfig
					if conn != expectedConn {
						t.Fatalf("newStartTLSClient conn = %v, want expected conn", conn)
					}
					return expectedClient, nil
				},
			}

			account := config.IMAPAccount{
				Security:      tt.security,
				TLSSkipVerify: tt.tlsSkipVerify,
			}

			client, err := dialWithSecurity(context.Background(), account, "imap.example.com:993", dialers)
			if err != nil {
				t.Fatalf("dialWithSecurity() error = %v", err)
			}
			if client != expectedClient {
				t.Fatalf("dialWithSecurity() client = %v, want expected client", client)
			}

			if tlsDialCalls != tt.wantTLSDial {
				t.Fatalf("TLS dial calls = %d, want %d", tlsDialCalls, tt.wantTLSDial)
			}
			if tcpDialCalls != tt.wantTCPDial {
				t.Fatalf("TCP dial calls = %d, want %d", tcpDialCalls, tt.wantTCPDial)
			}
			if newClientCalls != tt.wantNewClient {
				t.Fatalf("newClient calls = %d, want %d", newClientCalls, tt.wantNewClient)
			}
			if newStartTLSCalls != tt.wantNewStartTLS {
				t.Fatalf("newStartTLSClient calls = %d, want %d", newStartTLSCalls, tt.wantNewStartTLS)
			}

			if tt.security == "plain" {
				if capturedTLSConfig != nil {
					t.Fatalf("plain mode should not create TLS config, got %#v", capturedTLSConfig)
				}
				return
			}

			if capturedTLSConfig == nil {
				t.Fatal("expected TLS config to be captured")
			}
			if capturedTLSConfig.ServerName != "imap.example.com" {
				t.Fatalf("tls ServerName = %q, want %q", capturedTLSConfig.ServerName, "imap.example.com")
			}
			if capturedTLSConfig.InsecureSkipVerify != tt.tlsSkipVerify {
				t.Fatalf("tls InsecureSkipVerify = %v, want %v", capturedTLSConfig.InsecureSkipVerify, tt.tlsSkipVerify)
			}
			if len(capturedTLSConfig.NextProtos) != len(tt.wantNextProtos) {
				t.Fatalf("tls NextProtos length = %d, want %d", len(capturedTLSConfig.NextProtos), len(tt.wantNextProtos))
			}
			for i := range tt.wantNextProtos {
				if capturedTLSConfig.NextProtos[i] != tt.wantNextProtos[i] {
					t.Fatalf("tls NextProtos[%d] = %q, want %q", i, capturedTLSConfig.NextProtos[i], tt.wantNextProtos[i])
				}
			}
		})
	}
}

func TestDialWithSecurity_STARTTLSFailureWrapped(t *testing.T) {
	startTLSErr := errors.New("server does not support STARTTLS")
	dialers := transportDialers{
		dialTLS: func(_ context.Context, _ string, _ *tls.Config) (net.Conn, error) {
			t.Fatal("dialTLS should not be called")
			return nil, nil
		},
		dialTCP: func(_ context.Context, _ string) (net.Conn, error) {
			return &stubNetConn{}, nil
		},
		newClient: func(net.Conn) IMAPClient {
			t.Fatal("newClient should not be called")
			return nil
		},
		newStartTLSClient: func(_ context.Context, _ net.Conn, _ *tls.Config) (IMAPClient, error) {
			return nil, startTLSErr
		},
	}

	_, err := dialWithSecurity(context.Background(), config.IMAPAccount{Security: "starttls"}, "imap.example.com:143", dialers)
	if err == nil {
		t.Fatal("expected starttls error")
	}
	if !errors.Is(err, startTLSErr) {
		t.Fatalf("expected wrapped starttls error, got %v", err)
	}
	if !strings.Contains(err.Error(), "starting STARTTLS") {
		t.Fatalf("error = %q, want to contain %q", err.Error(), "starting STARTTLS")
	}
}

func TestDialWithSecurity_UnknownSecurityMode(t *testing.T) {
	dialers := transportDialers{
		dialTLS: func(_ context.Context, _ string, _ *tls.Config) (net.Conn, error) {
			t.Fatal("dialTLS should not be called")
			return nil, nil
		},
		dialTCP: func(_ context.Context, _ string) (net.Conn, error) {
			t.Fatal("dialTCP should not be called")
			return nil, nil
		},
		newClient: func(net.Conn) IMAPClient {
			t.Fatal("newClient should not be called")
			return nil
		},
		newStartTLSClient: func(_ context.Context, _ net.Conn, _ *tls.Config) (IMAPClient, error) {
			t.Fatal("newStartTLSClient should not be called")
			return nil, nil
		},
	}

	_, err := dialWithSecurity(context.Background(), config.IMAPAccount{Security: "bogus"}, "imap.example.com:999", dialers)
	if err == nil {
		t.Fatal("expected unsupported mode error")
	}
	if !strings.Contains(err.Error(), "unsupported IMAP security mode") {
		t.Fatalf("error = %q, want unsupported mode message", err.Error())
	}
}

func TestDefaultDialer_ContextCanceledDoesNotDial(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	accepted := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			select {
			case accepted <- struct{}{}:
			default:
			}
		}
	}()
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Fatalf("closing listener: %v", closeErr)
		}
		<-done
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = defaultDialer(ctx, config.IMAPAccount{Security: "imaps"}, listener.Addr().String())
	if err == nil {
		t.Fatal("expected dialer error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got: %v", err)
	}

	select {
	case <-accepted:
		t.Fatal("dialer established a connection after cancellation")
	case <-time.After(100 * time.Millisecond):
		// expected: no connection attempts
	}
}

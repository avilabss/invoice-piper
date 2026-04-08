package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jhillyerd/enmime"

	"github.com/avilabss/invoice-piper/internal/config"
	"github.com/avilabss/invoice-piper/internal/logger"
)

// Message represents a parsed email with its metadata and attachments.
type Message struct {
	Subject     string
	From        string // raw sender email address
	Date        time.Time
	TextBody    string
	HTMLBody    string
	Attachments []Attachment
}

// Attachment represents a single email attachment.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

type waitCommand interface {
	Wait() error
}

type listCommand interface {
	Collect() ([]*imap.ListData, error)
}

type selectCommand interface {
	Wait() (*imap.SelectData, error)
}

type searchCommand interface {
	Wait() (*imap.SearchData, error)
}

type fetchMessage interface {
	Collect() (*imapclient.FetchMessageBuffer, error)
}

type fetchCommand interface {
	Next() fetchMessage
	Close() error
}

// IMAPClient defines the operations needed from an IMAP client.
// This interface exists for testability.
type IMAPClient interface {
	Login(username, password string) waitCommand
	List(ref, pattern string, options *imap.ListOptions) listCommand
	Select(mailbox string, options *imap.SelectOptions) selectCommand
	UIDSearch(criteria *imap.SearchCriteria, options *imap.SearchOptions) searchCommand
	Fetch(numSet imap.NumSet, options *imap.FetchOptions) fetchCommand
	Logout() waitCommand
	Close() error
}

type imapClientAdapter struct {
	client *imapclient.Client
}

func (a *imapClientAdapter) Login(username, password string) waitCommand {
	return a.client.Login(username, password)
}

func (a *imapClientAdapter) List(ref, pattern string, options *imap.ListOptions) listCommand {
	return a.client.List(ref, pattern, options)
}

func (a *imapClientAdapter) Select(mailbox string, options *imap.SelectOptions) selectCommand {
	return a.client.Select(mailbox, options)
}

func (a *imapClientAdapter) UIDSearch(criteria *imap.SearchCriteria, options *imap.SearchOptions) searchCommand {
	return a.client.UIDSearch(criteria, options)
}

func (a *imapClientAdapter) Fetch(numSet imap.NumSet, options *imap.FetchOptions) fetchCommand {
	return &imapFetchCommandAdapter{cmd: a.client.Fetch(numSet, options)}
}

func (a *imapClientAdapter) Logout() waitCommand {
	return a.client.Logout()
}

func (a *imapClientAdapter) Close() error {
	return a.client.Close()
}

type imapFetchCommandAdapter struct {
	cmd *imapclient.FetchCommand
}

func (a *imapFetchCommandAdapter) Next() fetchMessage {
	msg := a.cmd.Next()
	if msg == nil {
		return nil
	}
	return msg
}

func (a *imapFetchCommandAdapter) Close() error {
	return a.cmd.Close()
}

const (
	imapSecurityIMAPS    = "imaps"
	imapSecuritySTARTTLS = "starttls"
	imapSecurityPlain    = "plain"
)

// dialTimeout is the max time to wait for an IMAP connection.
const dialTimeout = 30 * time.Second

type transportDialers struct {
	dialTLS           func(ctx context.Context, addr string, tlsConfig *tls.Config) (net.Conn, error)
	dialTCP           func(ctx context.Context, addr string) (net.Conn, error)
	newClient         func(conn net.Conn) IMAPClient
	newStartTLSClient func(ctx context.Context, conn net.Conn, tlsConfig *tls.Config) (IMAPClient, error)
}

var defaultTransportDialers = transportDialers{
	dialTLS: defaultTLSDial,
	dialTCP: defaultTCPDial,
	newClient: func(conn net.Conn) IMAPClient {
		return &imapClientAdapter{client: imapclient.New(conn, nil)}
	},
	newStartTLSClient: defaultStartTLSClient,
}

// Client wraps IMAP operations for a single account.
type Client struct {
	account config.IMAPAccount
	dialer  func(ctx context.Context, addr string) (IMAPClient, error)
}

func NewClient(account config.IMAPAccount) *Client {
	return &Client{
		account: account,
		dialer: func(ctx context.Context, addr string) (IMAPClient, error) {
			return defaultDialer(ctx, account, addr)
		},
	}
}

func defaultDialer(ctx context.Context, account config.IMAPAccount, addr string) (IMAPClient, error) {
	return dialWithSecurity(ctx, account, addr, defaultTransportDialers)
}

func dialWithSecurity(ctx context.Context, account config.IMAPAccount, addr string, dialers transportDialers) (IMAPClient, error) {
	security := account.Security
	if security == "" {
		security = imapSecurityIMAPS
	}

	switch security {
	case imapSecurityIMAPS:
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		tlsConfig := &tls.Config{
			NextProtos:         []string{"imap"},
			ServerName:         host,
			InsecureSkipVerify: account.TLSSkipVerify,
		}

		conn, err := dialers.dialTLS(ctx, addr, tlsConfig)
		if err != nil {
			return nil, err
		}

		return dialers.newClient(conn), nil

	case imapSecuritySTARTTLS:
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		conn, err := dialers.dialTCP(ctx, addr)
		if err != nil {
			return nil, err
		}

		tlsConfig := &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: account.TLSSkipVerify,
		}

		client, err := dialers.newStartTLSClient(ctx, conn, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("starting STARTTLS: %w", err)
		}

		return client, nil

	case imapSecurityPlain:
		conn, err := dialers.dialTCP(ctx, addr)
		if err != nil {
			return nil, err
		}

		return dialers.newClient(conn), nil

	default:
		return nil, fmt.Errorf("unsupported IMAP security mode %q", account.Security)
	}
}

func defaultTLSDial(ctx context.Context, addr string, tlsConfig *tls.Config) (net.Conn, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{},
		Config:    tlsConfig,
	}

	return dialer.DialContext(ctx, "tcp", addr)
}

func defaultTCPDial(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{}
	return dialer.DialContext(ctx, "tcp", addr)
}

func defaultStartTLSClient(ctx context.Context, conn net.Conn, tlsConfig *tls.Config) (IMAPClient, error) {
	stopCancelWatch := closeConnOnCancel(ctx, conn)
	defer stopCancelWatch()

	client, err := imapclient.NewStartTLS(conn, &imapclient.Options{TLSConfig: tlsConfig})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		if closeErr := client.Close(); closeErr != nil {
			slog.Debug("Connection close error", "error", closeErr)
		}
		return nil, ctxErr
	}

	return &imapClientAdapter{client: client}, nil
}

func closeConnOnCancel(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
				slog.Debug("Connection close error", "error", closeErr)
			}
		case <-done:
		}
	}()

	return func() {
		close(done)
	}
}

// ListMailboxes connects and returns all available mailbox names.
func (c *Client) ListMailboxes(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	slog.Debug("Listing mailboxes", "account", c.account.Name)
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.disconnect(conn)

	names, err := listMailboxNames(conn.List("", "*", nil))
	if err != nil {
		return names, fmt.Errorf("listing mailboxes: %w", err)
	}
	slog.Debug("Mailboxes listed", "count", len(names))
	return names, nil
}

// FetchMessages connects, searches for emails in the given year/month across
// configured mailboxes, and returns parsed messages with attachments.
func (c *Client) FetchMessages(ctx context.Context, year, month int) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.disconnect(conn)

	since := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	before := since.AddDate(0, 1, 0)
	criteria := &imap.SearchCriteria{
		Since:  since,
		Before: before,
	}

	var allMessages []Message
	var hadFailures bool

	mailboxes, err := c.resolveMailboxes(conn)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || len(mailboxes) == 0 {
			return nil, err
		}
		hadFailures = true
		slog.Warn("Mailbox listing partially failed", "account", c.account.Name, "error", err)
	}

	for _, mbox := range mailboxes {
		if err := ctx.Err(); err != nil {
			return allMessages, err
		}

		slog.Debug("Searching mailbox", "mailbox", mbox)
		msgs, err := c.fetchFromMailbox(ctx, conn, mbox, criteria)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return append(allMessages, msgs...), err
			}
			hadFailures = true
			slog.Warn("Mailbox processing failed", "mailbox", mbox, "error", err)
		}
		logger.Trace("Mailbox results", "mailbox", mbox, "messages", len(msgs))
		allMessages = append(allMessages, msgs...)
	}

	if hadFailures {
		return allMessages, fmt.Errorf("one or more mailbox/message operations failed")
	}

	return allMessages, nil
}

func (c *Client) connect(ctx context.Context) (IMAPClient, error) {
	addr := fmt.Sprintf("%s:%d", c.account.Host, c.account.Port)
	logger.Trace("Connecting to IMAP", "addr", addr)

	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	conn, err := c.dialer(dialCtx, addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	slog.Debug("Logging in", "username", c.account.Username)
	if err := conn.Login(c.account.Username, c.account.Password).Wait(); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Debug("Connection close error", "error", closeErr)
		}
		return nil, fmt.Errorf("logging in as %s: %w", c.account.Username, err)
	}
	slog.Debug("Login successful", "username", c.account.Username)

	return conn, nil
}

func (c *Client) disconnect(conn IMAPClient) {
	if err := conn.Logout().Wait(); err != nil {
		slog.Debug("Logout error", "error", err)
	}
	if err := conn.Close(); err != nil {
		slog.Debug("Connection close error", "error", err)
	}
}

func (c *Client) resolveMailboxes(conn IMAPClient) ([]string, error) {
	if len(c.account.Mailboxes) == 1 && c.account.Mailboxes[0] == "ALL" {
		names, err := listMailboxNames(conn.List("", "*", nil))
		if err != nil {
			return names, fmt.Errorf("listing mailboxes: %w", err)
		}
		return names, nil
	}
	return c.account.Mailboxes, nil
}

func (c *Client) fetchFromMailbox(ctx context.Context, conn IMAPClient, mailbox string, criteria *imap.SearchCriteria) ([]Message, error) {
	if _, err := conn.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("selecting %s: %w", mailbox, err)
	}

	searchData, err := conn.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("searching %s: %w", mailbox, err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		logger.Trace("No messages found", "mailbox", mailbox)
		return nil, nil
	}
	slog.Debug("Messages found in mailbox", "mailbox", mailbox, "count", len(uids))

	bodySection := &imap.FetchItemBodySection{Peek: true}
	fetchOptions := &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	// Stream messages instead of loading all into memory.
	fetchCmd := conn.Fetch(imap.UIDSetNum(uids...), fetchOptions)
	return collectFetchedMessages(ctx, mailbox, bodySection, fetchCmd)
}

func listMailboxNames(cmd listCommand) ([]string, error) {
	mailboxes, err := cmd.Collect()
	names := make([]string, 0, len(mailboxes))
	for _, mbox := range mailboxes {
		names = append(names, mbox.Mailbox)
	}
	return names, err
}

func collectFetchedMessages(ctx context.Context, mailbox string, bodySection *imap.FetchItemBodySection, fetchCmd fetchCommand) (messages []Message, retErr error) {
	defer func() {
		if err := fetchCmd.Close(); err != nil {
			closeErr := fmt.Errorf("finalizing fetch for %s: %w", mailbox, err)
			if retErr != nil {
				retErr = errors.Join(retErr, closeErr)
			} else {
				retErr = closeErr
			}
		}
	}()

	var hadMessageFailures bool
	for {
		if err := ctx.Err(); err != nil {
			return messages, err
		}

		msgData := fetchCmd.Next()
		if msgData == nil {
			break
		}

		buf, err := msgData.Collect()
		if err != nil {
			slog.Warn("Failed to collect message data", "mailbox", mailbox, "error", err)
			hadMessageFailures = true
			continue
		}

		parsed, err := parseMessage(buf, bodySection)
		if err != nil {
			slog.Warn("Failed to parse message", "uid", buf.UID, "mailbox", mailbox, "error", err)
			hadMessageFailures = true
			continue
		}
		logger.Trace("Parsed message", "uid", buf.UID, "subject", parsed.Subject, "attachments", len(parsed.Attachments))
		messages = append(messages, *parsed)
	}

	if hadMessageFailures {
		return messages, fmt.Errorf("one or more messages in %s failed to parse", mailbox)
	}

	return messages, nil
}

func parseMessage(msg *imapclient.FetchMessageBuffer, bodySection *imap.FetchItemBodySection) (*Message, error) {
	rawBody := msg.FindBodySection(bodySection)
	if rawBody == nil {
		return nil, fmt.Errorf("no body data")
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("parsing MIME: %w", err)
	}

	var from string
	if msg.Envelope != nil && len(msg.Envelope.From) > 0 {
		from = msg.Envelope.From[0].Addr()
	}

	var date time.Time
	var subject string
	if msg.Envelope != nil {
		date = msg.Envelope.Date
		subject = msg.Envelope.Subject
	}

	var attachments []Attachment
	for _, att := range env.Attachments {
		attachments = append(attachments, Attachment{
			Filename:    att.FileName,
			ContentType: att.ContentType,
			Data:        att.Content,
		})
	}

	return &Message{
		Subject:     subject,
		From:        from,
		Date:        date,
		TextBody:    env.Text,
		HTMLBody:    env.HTML,
		Attachments: attachments,
	}, nil
}

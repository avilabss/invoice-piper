package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func TestSetVerbosity_Levels(t *testing.T) {
	tests := []struct {
		name      string
		verbosity int
		level     slog.Level
		label     string
	}{
		{"default warns only", 0, slog.LevelWarn, "WARN"},
		{"v shows info", 1, slog.LevelInfo, "INFO"},
		{"vv shows debug", 2, slog.LevelDebug, "DEBUG"},
		{"vvv shows trace", 3, LevelTrace, "TRACE"},
		{"beyond 3 still trace", 5, LevelTrace, "TRACE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := &handler{
				level: tt.level,
				mu:    &sync.Mutex{},
				out:   &buf,
			}
			slog.SetDefault(slog.New(h))

			// Log at the expected level
			slog.Log(context.TODO(), tt.level, "test message", "key", "value")

			output := buf.String()
			if !strings.Contains(output, tt.label) {
				t.Errorf("expected %q in output, got %q", tt.label, output)
			}
			if !strings.Contains(output, "test message") {
				t.Errorf("expected 'test message' in output, got %q", output)
			}
			if !strings.Contains(output, "key=value") {
				t.Errorf("expected 'key=value' in output, got %q", output)
			}
		})
	}
}

func TestSetVerbosity_FiltersLowerLevels(t *testing.T) {
	var buf bytes.Buffer
	h := &handler{
		level: slog.LevelWarn,
		mu:    &sync.Mutex{},
		out:   &buf,
	}
	slog.SetDefault(slog.New(h))

	slog.Info("should not appear")
	slog.Debug("should not appear")

	if buf.Len() > 0 {
		t.Errorf("expected no output at warn level for info/debug, got %q", buf.String())
	}

	slog.Warn("should appear")
	if !strings.Contains(buf.String(), "should appear") {
		t.Error("warn message should appear at warn level")
	}
}

func TestTrace(t *testing.T) {
	var buf bytes.Buffer
	h := &handler{
		level: LevelTrace,
		mu:    &sync.Mutex{},
		out:   &buf,
	}
	slog.SetDefault(slog.New(h))

	Trace("trace msg", "detail", "extra")

	output := buf.String()
	if !strings.Contains(output, "TRACE") {
		t.Errorf("expected TRACE in output, got %q", output)
	}
	if !strings.Contains(output, "trace msg") {
		t.Errorf("expected 'trace msg' in output, got %q", output)
	}
	if !strings.Contains(output, "detail=extra") {
		t.Errorf("expected 'detail=extra' in output, got %q", output)
	}
}

func TestHandler_Enabled(t *testing.T) {
	h := &handler{level: slog.LevelInfo}

	if h.Enabled(context.TODO(), slog.LevelDebug) {
		t.Error("debug should not be enabled at info level")
	}
	if !h.Enabled(context.TODO(), slog.LevelInfo) {
		t.Error("info should be enabled at info level")
	}
	if !h.Enabled(context.TODO(), slog.LevelWarn) {
		t.Error("warn should be enabled at info level")
	}
}

func TestHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := &handler{
		level: slog.LevelInfo,
		mu:    &sync.Mutex{},
		out:   &buf,
	}

	logger := slog.New(h).With("component", "imap")
	logger.Info("connected")

	output := buf.String()
	if !strings.Contains(output, "component=imap") {
		t.Errorf("expected pre-attached attr in output, got %q", output)
	}
	if !strings.Contains(output, "connected") {
		t.Errorf("expected message in output, got %q", output)
	}
}

func TestHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	h := &handler{
		level: slog.LevelInfo,
		mu:    &sync.Mutex{},
		out:   &buf,
	}

	logger := slog.New(h).WithGroup("email")
	logger.Info("fetching", "account", "test")

	output := buf.String()
	if !strings.Contains(output, "email.account=test") {
		t.Errorf("expected grouped attr in output, got %q", output)
	}
}

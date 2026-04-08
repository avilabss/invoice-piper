package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// LevelTrace is a custom level below Debug for -vvv output.
const LevelTrace = slog.Level(-8)

type handler struct {
	level  slog.Level
	mu     *sync.Mutex
	out    io.Writer
	attrs  []slog.Attr
	groups []string
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	levelStr := r.Level.String()
	if r.Level == LevelTrace {
		levelStr = "TRACE"
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	timestamp := r.Time.Format(time.TimeOnly)
	_, _ = fmt.Fprintf(h.out, "%s [%-5s] %s", timestamp, levelStr, r.Message)

	// Print pre-attached attrs first
	for _, a := range h.attrs {
		_, _ = fmt.Fprintf(h.out, " %s=%v", h.prefixedKey(a.Key), a.Value)
	}

	// Print record attrs
	r.Attrs(func(a slog.Attr) bool {
		_, _ = fmt.Fprintf(h.out, " %s=%v", h.prefixedKey(a.Key), a.Value)
		return true
	})

	_, _ = fmt.Fprintln(h.out)
	return nil
}

func (h *handler) prefixedKey(key string) string {
	if len(h.groups) == 0 {
		return key
	}
	prefix := ""
	for _, g := range h.groups {
		prefix += g + "."
	}
	return prefix + key
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	newAttrs = append(newAttrs, attrs...)
	return &handler{
		level:  h.level,
		mu:     h.mu,
		out:    h.out,
		attrs:  newAttrs,
		groups: h.groups,
	}
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups), len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups = append(newGroups, name)
	return &handler{
		level:  h.level,
		mu:     h.mu,
		out:    h.out,
		attrs:  h.attrs,
		groups: newGroups,
	}
}

// SetVerbosity configures slog based on the verbosity level.
//
//	0: errors/warnings only (slog.LevelWarn)
//	1 (-v): info level (slog.LevelInfo)
//	2 (-vv): debug level (slog.LevelDebug)
//	3 (-vvv): trace level (LevelTrace)
func SetVerbosity(level int) {
	slogLevel := slog.LevelWarn
	switch {
	case level >= 3:
		slogLevel = LevelTrace
	case level >= 2:
		slogLevel = slog.LevelDebug
	case level >= 1:
		slogLevel = slog.LevelInfo
	}

	h := &handler{
		level: slogLevel,
		mu:    &sync.Mutex{},
		out:   os.Stderr,
	}

	slog.SetDefault(slog.New(h))
}

// Trace logs at trace level (-vvv). Raw data and internals.
func Trace(msg string, args ...any) {
	slog.Log(context.TODO(), LevelTrace, msg, args...)
}

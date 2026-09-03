package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger_JSONByDefault(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(config{logLevel: slog.LevelInfo, logFormat: "json"}, &buf)
	logger.Info("hello")

	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("json format did not produce a JSON line: %q", buf.String())
	}
}

func TestNewLogger_Text(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(config{logLevel: slog.LevelInfo, logFormat: "text"}, &buf)
	logger.Info("hello")

	if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
		t.Errorf("text format produced a JSON line: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("text line missing the message: %q", buf.String())
	}
}

func TestNewLogger_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(config{logLevel: slog.LevelWarn, logFormat: "text"}, &buf)
	logger.Info("should be filtered")

	if buf.Len() != 0 {
		t.Errorf("info line was not filtered at warn level: %q", buf.String())
	}
}

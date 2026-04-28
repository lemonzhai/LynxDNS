package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input  string
		expect Level
	}{
		{"debug", DEBUG},
		{"info", INFO},
		{"warn", WARN},
		{"error", ERROR},
		{"unknown", INFO},
	}

	for _, tt := range tests {
		got := ParseLevel(tt.input)
		if got != tt.expect {
			t.Errorf("ParseLevel(%s) = %d, want %d", tt.input, got, tt.expect)
		}
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level  Level
		expect string
	}{
		{DEBUG, "DEBUG"},
		{INFO, "INFO"},
		{WARN, "WARN"},
		{ERROR, "ERROR"},
	}

	for _, tt := range tests {
		got := tt.level.String()
		if got != tt.expect {
			t.Errorf("Level(%d).String() = %s, want %s", tt.level, got, tt.expect)
		}
	}
}

func TestLogOutput(t *testing.T) {
	var buf bytes.Buffer
	l := New(DEBUG)
	l.AddOutput(&buf)

	l.Info("test message %s", "arg")

	output := buf.String()
	if !strings.Contains(output, "INFO") {
		t.Errorf("expected output to contain 'INFO', got: %s", output)
	}
	if !strings.Contains(output, "test message arg") {
		t.Errorf("expected output to contain 'test message arg', got: %s", output)
	}
}

func TestLogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := New(WARN)
	l.AddOutput(&buf)

	l.Debug("debug msg")
	l.Info("info msg")
	l.Warn("warn msg")
	l.Error("error msg")

	output := buf.String()
	if strings.Contains(output, "debug msg") {
		t.Error("debug should be filtered at WARN level")
	}
	if strings.Contains(output, "info msg") {
		t.Error("info should be filtered at WARN level")
	}
	if !strings.Contains(output, "warn msg") {
		t.Error("warn should be present")
	}
	if !strings.Contains(output, "error msg") {
		t.Error("error should be present")
	}
}

func TestSubscribe(t *testing.T) {
	l := New(DEBUG)

	ch := l.Subscribe()
	defer l.Unsubscribe(ch)

	l.Info("subscribe test")

	select {
	case entry := <-ch:
		if entry.Message != "subscribe test" {
			t.Errorf("expected message 'subscribe test', got '%s'", entry.Message)
		}
		if entry.Level != INFO {
			t.Errorf("expected level INFO, got %d", entry.Level)
		}
	default:
		t.Error("expected to receive log entry from subscription")
	}
}

func TestSetLevel(t *testing.T) {
	l := New(DEBUG)
	l.SetLevel(ERROR)

	var buf bytes.Buffer
	l.AddOutput(&buf)

	l.Info("should be filtered")
	l.Error("should appear")

	output := buf.String()
	if strings.Contains(output, "should be filtered") {
		t.Error("info should be filtered at ERROR level")
	}
	if !strings.Contains(output, "should appear") {
		t.Error("error should appear")
	}
}

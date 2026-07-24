package logger

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// captureStdout swaps os.Stdout for a pipe around fn. NewLogger reads
// os.Stdout when it builds the writer, so it has to be called inside fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}
	return string(out)
}

func TestNewLogger_JSONFormat(t *testing.T) {
	out := captureStdout(t, func() {
		l := NewLogger(&config.Config{Log: config.Logger{Format: config.LogFormatJSON, Level: "info"}})
		l.Error().Str("device", "wg0").Msg("boom")
	})

	var entry map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &entry); err != nil {
		t.Fatalf("output is not JSON: %v (got %q)", err, out)
	}

	if entry["level"] != "error" {
		t.Errorf("level = %v, want error", entry["level"])
	}
	if entry["message"] != "boom" {
		t.Errorf("message = %v, want boom", entry["message"])
	}
	if entry["device"] != "wg0" {
		t.Errorf("device = %v, want wg0", entry["device"])
	}
}

func TestNewLogger_ConsoleFormat(t *testing.T) {
	out := captureStdout(t, func() {
		l := NewLogger(&config.Config{Log: config.Logger{Format: config.LogFormatConsole, Level: "info"}})
		l.Error().Msg("boom")
	})

	var entry map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &entry) == nil {
		t.Errorf("console output parsed as JSON, want the human-readable form (got %q)", out)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("output = %q, want it to contain the message", out)
	}
}

// A zero Config must log in the console format, and must still log at all:
// an empty level reads as NoLevel, which would silence even errors.
func TestNewLogger_ZeroConfig(t *testing.T) {
	out := captureStdout(t, func() {
		l := NewLogger(&config.Config{})
		l.Error().Msg("boom")
	})

	if !strings.Contains(out, "boom") {
		t.Fatalf("output = %q, want the error to be logged", out)
	}

	var entry map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &entry) == nil {
		t.Errorf("output parsed as JSON, want console (got %q)", out)
	}
}

func TestNewLogger_Level(t *testing.T) {
	tests := []struct {
		level string
		want  zerolog.Level
	}{
		{"trace", zerolog.TraceLevel},
		{"debug", zerolog.DebugLevel},
		{"info", zerolog.InfoLevel},
		{"warn", zerolog.WarnLevel},
		{"error", zerolog.ErrorLevel},
		{"fatal", zerolog.FatalLevel},
		{"panic", zerolog.PanicLevel},
		{"disabled", zerolog.Disabled},
		{"INFO", zerolog.InfoLevel},
		{"", zerolog.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			l := NewLogger(&config.Config{Log: config.Logger{Level: tt.level}})
			if got := l.GetLevel(); got != tt.want {
				t.Errorf("GetLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Below the configured level nothing is written at all.
func TestNewLogger_LevelFilters(t *testing.T) {
	out := captureStdout(t, func() {
		l := NewLogger(&config.Config{Log: config.Logger{Format: config.LogFormatJSON, Level: "warn"}})
		l.Debug().Msg("hidden")
		l.Info().Msg("hidden")
		l.Warn().Msg("shown")
	})

	if strings.Contains(out, "hidden") {
		t.Errorf("output = %q, want entries below warn dropped", out)
	}
	if !strings.Contains(out, "shown") {
		t.Errorf("output = %q, want the warn entry", out)
	}
}

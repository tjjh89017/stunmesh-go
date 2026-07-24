package logger

import (
	"io"
	"os"

	"github.com/google/wire"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

var DefaultSet = wire.NewSet(
	NewLogger,
)

func NewLogger(cfg *config.Config) *zerolog.Logger {
	// zerolog writes JSON natively; ConsoleWriter reformats it for humans.
	var writer io.Writer = zerolog.ConsoleWriter{Out: os.Stdout}
	if cfg.Log.Format == config.LogFormatJSON {
		writer = os.Stdout
	}

	logger := zerolog.New(writer).With().Timestamp().Logger()

	// Empty is the unset case; ParseLevel would read it as NoLevel and silence
	// everything, including errors. config.Load has already rejected the rest.
	level := cfg.Log.Level
	if level == "" {
		level = config.DefaultLogLevel
	}
	if parsed, err := zerolog.ParseLevel(level); err == nil {
		logger = logger.Level(parsed)
	}

	return &logger
}

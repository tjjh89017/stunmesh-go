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
	// everything, including errors. config.Load has already rejected the rest,
	// but fail safe to InfoLevel here too rather than falling through to
	// zerolog.New's own default (TraceLevel), in case a caller builds a
	// *config.Config directly and skips that validation.
	level := cfg.Log.Level
	if level == "" {
		level = config.DefaultLogLevel
	}
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	logger = logger.Level(parsed)

	return &logger
}

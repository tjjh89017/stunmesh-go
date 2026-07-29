package entity

import (
	"os"

	"github.com/rs/zerolog"
)

// NewStartupLogger returns a throwaway console logger for warnings raised
// before logger.NewLogger's fully configured logger exists — entity
// construction and config loading both need one. Centralizing the
// zerolog.ConsoleWriter setup here avoids repeating it at each call site.
func NewStartupLogger() zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()
}

package logger

import (
	"os"

	"github.com/google/wire"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

var DefaultSet = wire.NewSet(
	NewLogger,
)

var LevelMap = map[string]zerolog.Level{
	"debug": zerolog.DebugLevel,
	"info":  zerolog.InfoLevel,
	"warn":  zerolog.WarnLevel,
	"error": zerolog.ErrorLevel,
}

func NewLogger(config *config.Config) *zerolog.Logger {
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	if level, ok := LevelMap[config.Log.Level]; ok {
		logger = logger.Level(level)
	}

	return &logger
}

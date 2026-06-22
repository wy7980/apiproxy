package log

import (
	"io"
	"log/slog"
	"os"

	"github.com/wangyong/apiproxy/internal/config"
)

func Setup(level, format string, fileCfg config.LogFileConfig) (*slog.Logger, io.Closer) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	writer := io.Writer(os.Stdout)
	var closer io.Closer = nopCloser{}
	if fileCfg.Enabled {
		fileWriter, err := NewFileWriter(fileCfg.Dir, fileCfg.MaxDays, fileCfg.MaxSize)
		if err != nil {
			panic(err)
		}
		writer = io.MultiWriter(os.Stdout, fileWriter)
		closer = fileWriter
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: lvl}
	switch format {
	case "json":
		handler = slog.NewJSONHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}

	return slog.New(handler), closer
}

// nopCloser is a no-op Closer used when file logging is disabled.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }

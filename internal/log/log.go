package log

import (
	"io"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func Init(isProduction bool) {
	var writers []io.Writer

	// Only log to file in development
	if !isProduction {
		_ = os.MkdirAll("logs", 0755)
		fileWriter := &lumberjack.Logger{
			Filename:   dailyLogFile(),
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     28,
			Compress:   true,
		}
		writers = append(writers, fileWriter)
	}

	writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr})

	multi := zerolog.MultiLevelWriter(writers...)

	Log = zerolog.New(multi).With().Timestamp().Logger()
}

func dailyLogFile() string {
	today := time.Now().Format("2006-01-02")

	return "logs/" + today + ".log"
}

func Info() *zerolog.Event  { return Log.Info() }
func Error() *zerolog.Event { return Log.Error() }
func Warn() *zerolog.Event  { return Log.Warn() }
func Debug() *zerolog.Event { return Log.Debug() }
func Fatal() *zerolog.Event { return Log.Fatal() }

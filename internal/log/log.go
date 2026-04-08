package log

import (
	"io"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/rs/zerolog"
)

var Log = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).With().Timestamp().Logger()

func Init(isProduction bool) {
	_ = os.MkdirAll("logs", 0755)

	// Rotated file writer used in both production and development
	fileWriter := &lumberjack.Logger{
		Filename:   dailyLogFile(),
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     28,
		Compress:   true,
	}

	var output io.Writer

	if isProduction {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		zerolog.TimeFieldFormat = time.RFC3339
		output = zerolog.MultiLevelWriter(fileWriter, os.Stdout)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		output = zerolog.MultiLevelWriter(fileWriter, zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})
	}

	Log = Log.Output(output)
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

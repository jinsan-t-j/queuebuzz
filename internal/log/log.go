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
	// 1. Attempt to create/verify logs directory
	err := os.MkdirAll("logs", 0755)
	useFile := err == nil

	// 2. Defensive check: Try creating a dummy file to ensure it's actually writable
	// (Some environments allow Mkdir but deny Write)
	if useFile {
		testFile := "logs/.write_test"
		if f, err := os.Create(testFile); err != nil {
			useFile = false
		} else {
			f.Close()
			os.Remove(testFile)
		}
	}

	var outputs []io.Writer
	
	// Always include Stdout/Stderr based on environment
	if isProduction {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		zerolog.TimeFieldFormat = time.RFC3339
		outputs = append(outputs, os.Stdout)
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		outputs = append(outputs, zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})
	}

	// Only include file writer if we actually have write access
	if useFile {
		fileWriter := &lumberjack.Logger{
			Filename:   dailyLogFile(),
			MaxSize:    10,
			MaxBackups: 3,
			MaxAge:     28,
			Compress:   true,
		}
		outputs = append(outputs, fileWriter)
	} else {
		// If on Render or restricted env, warn that file logs are disabled
		Log.Warn().Msg("Logging to file disabled: 'logs/' directory is not writable")
	}

	Log = Log.Output(zerolog.MultiLevelWriter(outputs...))
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

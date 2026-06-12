package sentry

import (
	"runtime/debug"
	"time"

	"queuebuzz/internal/log"

	gosentry "github.com/getsentry/sentry-go"
)

// Init initializes the Sentry SDK. If dsn is empty, Sentry is disabled (zero overhead).
func Init(dsn, env string, tracesSampleRate float64) {
	if dsn == "" {
		log.Info().Msg("Sentry disabled (SENTRY_DSN is empty)")
		return
	}

	release := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		release = info.Main.Path + "@" + info.Main.Version
	}

	err := gosentry.Init(gosentry.ClientOptions{
		Dsn:              dsn,
		Environment:      env,
		Release:          release,
		TracesSampleRate: tracesSampleRate,
		EnableTracing:    tracesSampleRate > 0,
	})
	if err != nil {
		log.Error().Err(err).Msg("Sentry initialization failed")
		return
	}

	log.Info().
		Str("env", env).
		Float64("traces_sample_rate", tracesSampleRate).
		Msg("Sentry initialized")
}

// CaptureException sends an error to Sentry asynchronously.
func CaptureException(err error) {
	gosentry.CaptureException(err)
}

// Flush waits up to 2 seconds for buffered events to be sent.
// Call this on graceful shutdown or after panic recovery.
func Flush() {
	gosentry.Flush(2 * time.Second)
}

// RecoverWithSentry captures a recovered panic value and flushes immediately.
func RecoverWithSentry(r any) {
	gosentry.CurrentHub().Recover(r)
	gosentry.Flush(2 * time.Second)
}

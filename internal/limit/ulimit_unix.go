//go:build !windows

package limit

import (
	"syscall"

	"queuebuzz/internal/log"
)

// AutoTuneFileLimits raises the process soft limit for file descriptors.
func AutoTuneFileLimits() {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to get file descriptor limit")
		return
	}

	const targetLimit = 65535

	if rLimit.Max < targetLimit {
		rLimit.Cur = rLimit.Max
	} else {
		rLimit.Cur = targetLimit
	}

	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		// Fallback to setting soft limit to whatever the hard limit is
		rLimit.Cur = rLimit.Max
		err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to raise file descriptor limit programmatically")
			return
		}
	}

	log.Info().Uint64("cur", rLimit.Cur).Uint64("max", rLimit.Max).Msg("File descriptor limits configured programmatically")
}

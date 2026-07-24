package limit

import (
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"queuebuzz/internal/log"
)

var (
	isOverloaded int32 // 0 = normal, 1 = overloaded
	maxMemory    int64 // memory limit from cgroups in bytes
)

// AutoTuneMemoryLimit reads container cgroup limits and programmatically sets Go GOMEMLIMIT.
// It also sets up memory tracking for overload detection.
func AutoTuneMemoryLimit() {
	// Try cgroups v2
	limitBytes, err := readIntFromFile("/sys/fs/cgroup/memory.max")
	if err != nil {
		// Fallback to cgroups v1
		limitBytes, err = readIntFromFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	}

	if err == nil && limitBytes > 0 {
		// Apply a 10% safety margin for operating system / TCP socket buffer overheads
		safetyLimit := int64(float64(limitBytes) * 0.90)
		debug.SetMemoryLimit(safetyLimit)
		atomic.StoreInt64(&maxMemory, limitBytes)
		log.Info().Int64("limitBytes", limitBytes).Int64("safetyLimit", safetyLimit).Msg("Go runtime memory limit auto-configured via cgroups")
	}

	// Start resource monitor loop
	StartResourceMonitor()
}

// IsSystemOverloaded returns true if goroutine counts or memory usage exceed safe thresholds
func IsSystemOverloaded() bool {
	return atomic.LoadInt32(&isOverloaded) == 1
}

// GetContainerMemoryUsage returns current memory usage and max limit in bytes.
func GetContainerMemoryUsage() (current int64, limitVal int64) {
	limitVal = atomic.LoadInt64(&maxMemory)
	if limitVal <= 0 {
		return 0, 0
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	current = int64(m.Sys)

	return current, limitVal
}

// StartResourceMonitor ticks every 5 seconds checking system stats.
func StartResourceMonitor() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			// 1. Check goroutines count (concurrency pressure)
			if runtime.NumGoroutine() > 50000 {
				atomic.StoreInt32(&isOverloaded, 1)
				log.Warn().Int("goroutines", runtime.NumGoroutine()).Msg("System marked OVERLOADED: Goroutine threshold exceeded")
				continue
			}

			// 2. Check cgroups memory limits (memory pressure)
			current, limitVal := GetContainerMemoryUsage()
			if limitVal > 0 && current > 0 {
				usageRatio := float64(current) / float64(limitVal)
				if usageRatio > 0.90 {
					atomic.StoreInt32(&isOverloaded, 1)
					log.Warn().Float64("memoryUsageRatio", usageRatio).Msg("System marked OVERLOADED: Memory threshold exceeded 90%")
					continue
				}
			}

			atomic.StoreInt32(&isOverloaded, 0)
		}
	}()
}

func readIntFromFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	valStr := strings.TrimSpace(string(data))
	if valStr == "max" || valStr == "" {
		return 0, nil
	}
	return strconv.ParseInt(valStr, 10, 64)
}

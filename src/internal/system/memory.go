package system

import (
	"log/slog"
	"runtime"
)

// LogMemoryUsage logs the current memory usage of the process.
func LogMemoryUsage(tag string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	slog.Info("Memory Usage Report",
		"tag", tag,
		"alloc_mb", bToMb(m.Alloc),
		"total_alloc_mb", bToMb(m.TotalAlloc),
		"sys_mb", bToMb(m.Sys),
		"num_gc", m.NumGC,
	)
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

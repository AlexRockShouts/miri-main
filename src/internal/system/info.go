package system

import (
	"fmt"
	"runtime"
)

// Info holds basic hardware and OS information.
type Info struct {
	OS   string
	Arch string
}

// GetInfo returns the current system information in a human-readable way.
func GetInfo() string {
	return fmt.Sprintf("OS: %s, Architecture: %s, Go Version: %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
}

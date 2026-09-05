//go:build !windows

package backend

import "fmt"

func useWindowsPrivilegedHelper() bool { return false }

func applyWindowsViaPrivilegedHelper(_ string, _ []string, _ wgLogFunc) error {
	return fmt.Errorf("Windows privileged helper is unavailable on this platform")
}

func teardownWindowsPrivilegedHelper(_ wgLogFunc) {}

func cleanupStaleWindowsState(logf wgLogFunc) {
	cleanupStaleIPv6LeakProtection(logf)
}

func platformPrivilegeReport() string { return "" }

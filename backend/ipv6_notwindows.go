//go:build !windows

package backend

func enableIPv6LeakProtection(logf wgLogFunc) error {
	return nil
}

func restoreIPv6LeakProtection(logf wgLogFunc) error {
	return nil
}

func cleanupStaleIPv6LeakProtection(logf wgLogFunc) {}

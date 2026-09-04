//go:build !windows

package backend

import "errors"

func setAutoStartWindows(bool) error {
	return errors.New("Windows autostart is unavailable on this platform")
}

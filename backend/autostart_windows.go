//go:build windows

package backend

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsRunKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsRunValueName = "PWDTT"
)

func setAutoStartWindows(enabled bool) error {
	if !enabled {
		key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open Windows Run key: %w", err)
		}
		defer key.Close()

		if err := key.DeleteValue(windowsRunValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("delete Windows autostart value: %w", err)
		}
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsRunKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Windows Run key: %w", err)
	}
	defer key.Close()

	command := `"` + executable + `"`
	if err := key.SetStringValue(windowsRunValueName, command); err != nil {
		return fmt.Errorf("set Windows autostart value: %w", err)
	}
	return nil
}

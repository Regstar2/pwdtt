//go:build !windows

package backend

import (
	"context"
	"errors"
)

func (a *App) IsVKAuthAvailable() bool {
	return false
}

func (a *App) IsVKLoggedIn() bool {
	return false
}

func (a *App) VKLogin() error {
	return errors.New("автогенерация VK-хешей доступна только в Windows")
}

func (a *App) VKLogout() error {
	return nil
}

func (a *App) GenerateVKHashes(count int, existing []string) ([]string, error) {
	return nil, errors.New("автогенерация VK-хешей доступна только в Windows")
}

func (a *App) generateVKHashesWithContext(_ context.Context, _ int, _ []string) ([]string, error) {
	return nil, errors.New("автогенерация VK-хешей доступна только в Windows")
}

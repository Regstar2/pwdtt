//go:build !windows

package backend

import (
	"context"
	"errors"
)

func solveVKCaptchaInBrowser(context.Context, string) (string, error) {
	return "", errors.New("ручная CAPTCHA VK поддерживается только на Windows")
}

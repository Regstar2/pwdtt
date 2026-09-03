//go:build windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// loginLegacyVKSessionInWebView performs only the interactive VK login step.
// qWDTT treats a VK browser session (remixsid) separately from obtaining the
// legacy API access token, so PWDTT does the same. The API token is acquired
// lazily when hashes are generated.
func loginLegacyVKSessionInWebView(ctx context.Context) error {
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := clearLegacyVKBrowserProfile(); err != nil {
				return fmt.Errorf("не удалось очистить VK-сессию перед повтором: %w", err)
			}
		}

		userAgent := vkLegacyMobileUserAgent
		if attempt >= 2 {
			userAgent = vkLegacyDesktopUserAgent
		}

		session, err := startVKEdgeSession(ctx, legacyVKLoginStartURL(attempt), userAgent)
		if err != nil {
			return err
		}

		retry, err := waitForLegacyVKSession(ctx, session)
		session.close()
		if err == nil {
			return nil
		}
		if !retry {
			return err
		}
	}

	return errors.New("VK ID отклонил все три варианта входа: Unknown method passed")
}

func waitForLegacyVKSession(ctx context.Context, session *vkEdgeSession) (bool, error) {
	for {
		select {
		case <-ctx.Done():
			return false, context.Canceled
		case <-session.waitCh:
			return false, errors.New("окно авторизации VK закрыто")
		case <-time.After(vkEdgePollInterval):
		}

		state, err := session.pageState(ctx)
		if err != nil {
			if session.exited() {
				return false, errors.New("окно авторизации VK закрыто")
			}
			continue
		}

		if isVKLoginRateLimitedText(state.Body) {
			return false, errors.New("VK временно ограничил вход: слишком много попыток. Попробуйте позже; PWDTT не будет повторять вход автоматически")
		}
		if strings.Contains(strings.ToLower(state.Body), "unknown method") {
			return true, errors.New("VK ID вернул Unknown method passed")
		}

		hasSession, err := session.hasVKSessionCookie(ctx)
		if err != nil || !hasSession {
			continue
		}
		if isLegacyVKIDLoginFlow(state.URL) {
			continue
		}

		return false, nil
	}
}

//go:build windows

package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

// solveVKCaptchaInBrowser opens the genuine VK challenge in Microsoft Edge.
// The user solves it; CDP observes the documented captchaNotRobot.check
// response, without synthesizing a CAPTCHA answer or scraping private HTML.
func solveVKCaptchaInBrowser(ctx context.Context, challengeURL string) (string, error) {
	parsed, err := url.Parse(challengeURL)
	if err != nil || parsed.Scheme != "https" || !isOfficialVKHost(parsed.Hostname()) {
		return "", errors.New("VK предоставил неподдерживаемую ссылку CAPTCHA")
	}
	const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	session, err := startVKEdgeSession(ctx, challengeURL, userAgent)
	if err != nil {
		return "", err
	}
	defer session.close()
	return waitForVKCaptchaCheck(ctx, session)
}

func isOfficialVKHost(host string) bool {
	host = strings.ToLower(host)
	for _, domain := range []string{"vk.com", "vk.ru"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func isCaptchaCheckEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !isOfficialVKHost(u.Hostname()) {
		return false
	}
	return strings.HasSuffix(u.Path, "/captchaNotRobot.check")
}

type captchaCDPEvent struct {
	Method string `json:"method"`
	Params struct {
		RequestID string `json:"requestId"`
		Response struct {
			URL string `json:"url"`
		} `json:"response"`
	} `json:"params"`
}

func captchaTokenFromVKResponse(payload []byte) string {
	var body struct {
		Response struct {
			Status string `json:"status"`
			Token  string `json:"success_token"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &body) != nil || body.Response.Status != "ok" {
		return ""
	}
	return strings.TrimSpace(body.Response.Token)
}

func waitForVKCaptchaCheck(ctx context.Context, session *vkEdgeSession) (string, error) {
	if session == nil || session.conn == nil {
		return "", errors.New("Edge DevTools не подключён")
	}
	// startup CDP calls leave a short read deadline; remove it before the
	// interactive wait, and close the socket to interrupt ReadMessage on Cancel.
	_ = session.conn.SetReadDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = session.conn.Close() })
	defer stop()

	pending := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		_, raw, err := session.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", errors.New("соединение с окном Edge закрыто")
		}
		var event captchaCDPEvent
		if json.Unmarshal(raw, &event) != nil || event.Params.RequestID == "" {
			continue
		}
		switch event.Method {
		case "Network.responseReceived":
			if isCaptchaCheckEndpoint(event.Params.Response.URL) {
				pending[event.Params.RequestID] = true
			}
		case "Network.loadingFailed":
			delete(pending, event.Params.RequestID)
		case "Network.loadingFinished":
			if !pending[event.Params.RequestID] {
				continue
			}
			delete(pending, event.Params.RequestID)
			result, err := session.call(ctx, "Network.getResponseBody", map[string]any{
				"requestId": event.Params.RequestID,
			})
			if err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				continue
			}
			var response struct {
				Body          string `json:"body"`
				Base64Encoded bool   `json:"base64Encoded"`
			}
			if json.Unmarshal(result, &response) != nil {
				continue
			}
			payload := []byte(response.Body)
			if response.Base64Encoded {
				payload, err = base64.StdEncoding.DecodeString(response.Body)
				if err != nil {
					continue
				}
			}
			if token := captchaTokenFromVKResponse(payload); token != "" {
				return token, nil
			}
			// A rejected check is not a successful CAPTCHA. Let the user
			// retry in the same official VK window until deadline/cancel.
		}
	}
}

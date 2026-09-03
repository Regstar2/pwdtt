//go:build windows

package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	vkAuthHelperArg          = "--pwdtt-vk-auth-helper"
	vkAuthHelperResultPrefix = "PWDTT_VK_AUTH_RESULT:"
	vkAuthHelperErrorPrefix  = "PWDTT_VK_AUTH_ERROR:"
)

type vkAuthHelperResponse struct {
	AccessToken      string `json:"access_token,omitempty"`
	ExpiresInSeconds int64  `json:"expires_in_seconds,omitempty"`
	Error            string `json:"error,omitempty"`
}

// RunVKAuthHelperIfRequested switches the current executable into the isolated
// VK browser helper mode. The main Wails process must call this before
// initialising Wails itself.
func RunVKAuthHelperIfRequested() bool {
	if len(os.Args) < 3 || os.Args[1] != vkAuthHelperArg {
		return false
	}

	action := os.Args[2]
	response := runVKAuthHelperProcess(action)
	payload, err := json.Marshal(response)
	if err != nil {
		payload = []byte(`{"error":"не удалось сформировать ответ VK helper"}`)
	}
	encoded := base64.RawStdEncoding.EncodeToString(payload)
	_, _ = fmt.Fprintln(os.Stdout, vkAuthHelperResultPrefix+encoded)
	return true
}

func runVKAuthHelperProcess(action string) (response vkAuthHelperResponse) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = vkAuthHelperResponse{Error: fmt.Sprintf("внутренняя ошибка окна авторизации VK: %v", recovered)}
		}
	}()

	switch action {
	case "login":
		ctx, cancel := context.WithTimeout(context.Background(), vkOAuthTimeout)
		defer cancel()
		if err := loginLegacyVKSessionInWebView(ctx); err != nil {
			response.Error = helperFriendlyVKError(err, ctx)
		}
		return response
	case "token":
		ctx, cancel := context.WithTimeout(context.Background(), vkOAuthTimeout)
		defer cancel()
		token, err := obtainLegacyVKTokenQWDTT(ctx)
		if err != nil {
			response.Error = helperFriendlyVKError(err, ctx)
			return response
		}
		response.AccessToken = token.AccessToken
		if token.ExpiresIn > 0 {
			response.ExpiresInSeconds = int64(token.ExpiresIn / time.Second)
		}
		return response
	case "clear":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := clearLegacyVKWebViewCookies(ctx); err != nil {
			response.Error = helperFriendlyVKError(err, ctx)
		}
		return response
	default:
		return vkAuthHelperResponse{Error: "неизвестная операция VK helper"}
	}
}

func runLegacyVKAuthHelper(ctx context.Context, action string) (vkLegacyToken, error) {
	executable, err := os.Executable()
	if err != nil {
		return vkLegacyToken{}, fmt.Errorf("не удалось запустить окно авторизации VK: %w", err)
	}

	cmd := exec.CommandContext(ctx, executable, vkAuthHelperArg, action)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if ctx.Err() != nil {
		return vkLegacyToken{}, ctx.Err()
	}
	if runErr != nil {
		if detail := extractVKHelperError(stderr.String()); detail != "" {
			return vkLegacyToken{}, errors.New(detail)
		}
		if detail := compactVKHelperStderr(stderr.String()); detail != "" {
			return vkLegacyToken{}, fmt.Errorf("окно авторизации VK аварийно завершилось (%v): %s", runErr, detail)
		}
		return vkLegacyToken{}, fmt.Errorf("окно авторизации VK аварийно завершилось: %w", runErr)
	}

	response, err := parseVKHelperResponse(stdout.String())
	if err != nil {
		return vkLegacyToken{}, err
	}
	if response.Error != "" {
		return vkLegacyToken{}, errors.New(response.Error)
	}
	if action == "clear" || action == "login" {
		return vkLegacyToken{}, nil
	}
	if response.AccessToken == "" {
		return vkLegacyToken{}, errors.New("окно авторизации VK завершилось без access token")
	}

	token := vkLegacyToken{AccessToken: response.AccessToken}
	if response.ExpiresInSeconds > 0 {
		token.ExpiresIn = time.Duration(response.ExpiresInSeconds) * time.Second
	}
	return token, nil
}

func parseVKHelperResponse(output string) (vkAuthHelperResponse, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var encoded string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, vkAuthHelperResultPrefix) {
			encoded = strings.TrimSpace(strings.TrimPrefix(line, vkAuthHelperResultPrefix))
		}
	}
	if err := scanner.Err(); err != nil {
		return vkAuthHelperResponse{}, errors.New("не удалось прочитать ответ окна авторизации VK")
	}
	if encoded == "" {
		return vkAuthHelperResponse{}, errors.New("окно авторизации VK не вернуло результат")
	}
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return vkAuthHelperResponse{}, errors.New("окно авторизации VK вернуло повреждённый результат")
	}
	var response vkAuthHelperResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return vkAuthHelperResponse{}, errors.New("окно авторизации VK вернуло некорректный результат")
	}
	return response, nil
}

func extractVKHelperError(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var detail string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, vkAuthHelperErrorPrefix) {
			detail = strings.TrimSpace(strings.TrimPrefix(line, vkAuthHelperErrorPrefix))
		}
	}
	if detail == "" {
		return ""
	}
	return "окно авторизации VK аварийно завершилось: " + detail
}

func compactVKHelperStderr(output string) string {
	var lines []string
	home, _ := os.UserHomeDir()

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "access_token=") ||
			strings.Contains(lower, "#access_token") ||
			strings.Contains(lower, "authorization: bearer") ||
			strings.Contains(lower, "cookie:") {
			continue
		}
		if home != "" {
			line = strings.ReplaceAll(line, home, "%USERPROFILE%")
		}
		if len(line) > 320 {
			line = line[:320] + "…"
		}
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return ""
	}
	if len(lines) > 10 {
		lines = append(append([]string{}, lines[:3]...), append([]string{"…"}, lines[len(lines)-6:]...)...)
	}
	result := strings.Join(lines, " | ")
	if len(result) > 1600 {
		result = result[:1600] + "…"
	}
	return result
}

func helperFriendlyVKError(err error, ctx context.Context) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "время ожидания авторизации VK истекло"
	}
	if errors.Is(err, context.Canceled) {
		return "операция VK отменена"
	}
	return err.Error()
}

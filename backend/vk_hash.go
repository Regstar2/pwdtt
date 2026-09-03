package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	maxVKHashes     = 4
	minVKHashLen    = 16
	vkAPIVersion    = "5.199"
	vkCallsStartURL = "https://api.vk.ru/method/calls.start"
)

var vkHashPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type vkAPIError struct {
	Code    int
	Message string
}

func (e *vkAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("VK API вернул ошибку %d", e.Code)
	}
	return fmt.Sprintf("VK API вернул ошибку %d: %s", e.Code, e.Message)
}

type vkAPIClient struct {
	httpClient *http.Client
	callsURL   string
	delay      time.Duration
}

func newVKAPIClient() *vkAPIClient {
	return &vkAPIClient{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		callsURL:   vkCallsStartURL,
		delay:      2 * time.Second,
	}
}

func normalizeVKCallHash(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("VK-хеш пуст")
	}

	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", errors.New("некорректная ссылка VK")
		}
		host := strings.ToLower(u.Hostname())
		switch host {
		case "vk.com", "vk.ru", "m.vk.com", "m.vk.ru":
		default:
			return "", errors.New("ссылка должна вести на VK")
		}

		const marker = "/call/join/"
		idx := strings.Index(strings.ToLower(u.Path), marker)
		if idx < 0 {
			return "", errors.New("в ссылке нет VK call hash")
		}
		value = strings.Trim(strings.TrimSpace(u.Path[idx+len(marker):]), "/")
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
	}

	value = strings.TrimSpace(strings.SplitN(strings.SplitN(value, "?", 2)[0], "#", 2)[0])
	if len(value) < minVKHashLen {
		return "", fmt.Errorf("VK-хеш слишком короткий: минимум %d символов", minVKHashLen)
	}
	if !vkHashPattern.MatchString(value) {
		return "", errors.New("VK-хеш содержит недопустимые символы")
	}
	return value, nil
}

func (c *vkAPIClient) startCall(ctx context.Context, accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("VK не авторизован")
	}

	form := url.Values{"v": {vkAPIVersion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.callsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("не удалось подготовить запрос VK: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", context.Canceled
		}
		return "", fmt.Errorf("VK API недоступен: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Response struct {
			JoinLink string `json:"join_link"`
		} `json:"response"`
		Error *struct {
			Code    int    `json:"error_code"`
			Message string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", errors.New("VK API вернул некорректный ответ")
	}
	if payload.Error != nil {
		return "", &vkAPIError{Code: payload.Error.Code, Message: payload.Error.Message}
	}
	if strings.TrimSpace(payload.Response.JoinLink) == "" {
		return "", errors.New("VK не вернул ссылку на звонок")
	}
	return payload.Response.JoinLink, nil
}

func generateVKHashes(
	ctx context.Context,
	client *vkAPIClient,
	accessToken string,
	count int,
	existing []string,
	onGenerated func(hash string, current, total int),
) ([]string, error) {
	if count < 1 || count > maxVKHashes {
		return nil, fmt.Errorf("можно создать от 1 до %d VK-хешей", maxVKHashes)
	}

	seen := make(map[string]struct{}, len(existing)+count)
	for _, raw := range existing {
		hash, err := normalizeVKCallHash(raw)
		if err == nil {
			seen[hash] = struct{}{}
		}
	}

	result := make([]string, 0, count)
	attempts := 0
	maxAttempts := count + maxVKHashes
	for len(result) < count {
		if attempts >= maxAttempts {
			return result, errors.New("VK несколько раз вернул уже используемый хеш")
		}
		if attempts > 0 && client.delay > 0 {
			timer := time.NewTimer(client.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, context.Canceled
			case <-timer.C:
			}
		}

		joinLink, err := client.startCall(ctx, accessToken)
		attempts++
		if err != nil {
			return result, err
		}
		hash, err := normalizeVKCallHash(joinLink)
		if err != nil {
			return result, fmt.Errorf("VK вернул некорректную ссылку на звонок: %w", err)
		}
		if _, duplicate := seen[hash]; duplicate {
			continue
		}
		seen[hash] = struct{}{}
		result = append(result, hash)
		if onGenerated != nil {
			onGenerated(hash, len(result), count)
		}
	}
	return result, nil
}

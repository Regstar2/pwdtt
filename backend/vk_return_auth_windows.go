//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"html"
	"strings"
)

func (s *vkEdgeSession) modernRUReturnAuthFromWindowInit(ctx context.Context) (string, error) {
	result, err := s.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": `String((window.init && window.init.data && window.init.data.hash && window.init.data.hash.return_auth) || '')`,
		"returnByValue": true,
	})
	if err != nil {
		return "", err
	}
	var eval vkCDPEvalResult
	if err := json.Unmarshal(result, &eval); err != nil {
		return "", err
	}
	return strings.TrimSpace(eval.Result.Value), nil
}

func extractModernRUReturnAuthFromWindowInitHTML(body string) string {
	body = html.UnescapeString(body)
	raw := extractWindowInitJSONObject(body)
	if raw == "" {
		return ""
	}

	var payload struct {
		Data struct {
			Hash struct {
				ReturnAuth string `json:"return_auth"`
			} `json:"hash"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.Data.Hash.ReturnAuth)
}

func extractWindowInitJSONObject(body string) string {
	const marker = "window.init"
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	rest := body[start+len(marker):]
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[eq+1:])
	if rest == "" || rest[0] != '{' {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(rest); i++ {
		ch := rest[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	return ""
}

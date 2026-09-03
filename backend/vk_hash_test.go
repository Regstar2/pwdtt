package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeVKCallHash(t *testing.T) {
	const hash = "AbCdEfGh12345678_xyz"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"raw", hash, hash},
		{"vk.com", "https://vk.com/call/join/" + hash, hash},
		{"vk.ru", "https://vk.ru/call/join/" + hash, hash},
		{"m.vk.com", "https://m.vk.com/call/join/" + hash, hash},
		{"m.vk.ru", "https://m.vk.ru/call/join/" + hash, hash},
		{"query", "https://vk.com/call/join/" + hash + "?from=test", hash},
		{"fragment", "https://vk.ru/call/join/" + hash + "#section", hash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeVKCallHash(tt.in)
			if err != nil {
				t.Fatalf("normalizeVKCallHash() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeVKCallHash() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeVKCallHashRejectsInvalid(t *testing.T) {
	for _, value := range []string{
		"",
		"short",
		"https://example.com/call/join/AbCdEfGh12345678",
		"https://vk.com/not-a-call/AbCdEfGh12345678",
		"AbCd EfGh12345678",
	} {
		if _, err := normalizeVKCallHash(value); err == nil {
			t.Fatalf("normalizeVKCallHash(%q) expected error", value)
		}
	}
}

func TestVKAPIClientStartCallSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if r.Form.Get("v") != "5.199" {
			t.Fatalf("v = %q, want 5.199", r.Form.Get("v"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"join_link":"https://vk.com/call/join/AbCdEfGh12345678"}}`))
	}))
	defer server.Close()

	client := &vkAPIClient{httpClient: server.Client(), callsURL: server.URL}
	link, err := client.startCall(context.Background(), "token")
	if err != nil {
		t.Fatalf("startCall() error = %v", err)
	}
	if link != "https://vk.com/call/join/AbCdEfGh12345678" {
		t.Fatalf("link = %q", link)
	}
}

func TestVKAPIClientStartCallErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"api error", `{"error":{"error_code":5,"error_msg":"User authorization failed"}}`, "5"},
		{"missing join link", `{"response":{}}`, "не вернул ссылку"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := &vkAPIClient{httpClient: server.Client(), callsURL: server.URL}
			_, err := client.startCall(context.Background(), "token")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("startCall() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestGenerateVKHashesCountLimit(t *testing.T) {
	client := &vkAPIClient{}
	for _, count := range []int{0, 5} {
		if _, err := generateVKHashes(context.Background(), client, "token", count, nil, nil); err == nil {
			t.Fatalf("count %d expected error", count)
		}
	}
}

func TestGenerateVKHashesSkipsDuplicates(t *testing.T) {
	links := []string{
		"https://vk.com/call/join/ExistingHash123456",
		"https://vk.com/call/join/NewHashValue123456",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		link := links[0]
		links = links[1:]
		_, _ = w.Write([]byte(`{"response":{"join_link":"` + link + `"}}`))
	}))
	defer server.Close()

	client := &vkAPIClient{httpClient: server.Client(), callsURL: server.URL}
	got, err := generateVKHashes(
		context.Background(),
		client,
		"token",
		1,
		[]string{"ExistingHash123456"},
		nil,
	)
	if err != nil {
		t.Fatalf("generateVKHashes() error = %v", err)
	}
	if len(got) != 1 || got[0] != "NewHashValue123456" {
		t.Fatalf("got %v", got)
	}
}

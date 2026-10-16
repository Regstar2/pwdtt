package core

import "testing"

func TestClassifyLevelStructuredVKAuthResult(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "successful VKCalls stage is not an error",
			message: "[STREAM 300] [VK Auth] stage=vkcalls.step1 auth.getAnonymToken error_kind=success duration_ms=64 attempt=1 endpoint=api.vk.me",
			want:    "INFO",
		},
		{
			name:    "failed VKCalls stage remains an error",
			message: "[STREAM 300] [VK Auth] stage=vkcalls.step1 auth.getAnonymToken error_kind=network duration_ms=64 attempt=1 endpoint=api.vk.me",
			want:    "ERROR",
		},
		{
			name:    "other error is not hidden by successful metadata",
			message: "[VK Auth] error_kind=success but unexpected error while processing result",
			want:    "ERROR",
		},
		{
			name:    "DTLS failure remains an error",
			message: "[ВОРКЕР #1] Ошибка сессии: DTLS timeout через WRAP",
			want:    "ERROR",
		},
		{
			name:    "retry warning retains severity",
			message: "[ВОРКЕР #1] retry after timeout",
			want:    "WARN",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLevel(tt.message); got != tt.want {
				t.Fatalf("classifyLevel(%q) = %q; want %q", tt.message, got, tt.want)
			}
		})
	}
}

func TestLogWriterVKAuthSuccessDebugLevel(t *testing.T) {
	c := &Core{events: make(chan Event, 2)}
	c.debugLogging.Store(true)
	writer := &logWriter{core: c}
	messages := []struct {
		message string
		want    string
	}{
		{
			message: "[STREAM 300] [VK Auth] stage=vkcalls.step1 auth.getAnonymToken error_kind=success duration_ms=64",
			want:    "DEBUG",
		},
		{
			message: "[STREAM 300] [VK Auth] stage=vkcalls.step1 auth.getAnonymToken error_kind=network duration_ms=64",
			want:    "ERROR",
		},
	}
	for _, tt := range messages {
		input := []byte(tt.message + "\n")
		n, err := writer.Write(input)
		if err != nil || n != len(input) {
			t.Fatalf("Write() = (%d, %v); want (%d, nil)", n, err, len(input))
		}
		select {
		case event := <-c.events:
			if event.Type != EventLog || event.Level != tt.want || event.Message != tt.message {
				t.Fatalf("event = %+v; want log with level %s and original message", event, tt.want)
			}
		default:
			t.Fatal("expected log event")
		}
	}
}

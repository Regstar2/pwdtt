package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// captchaCoordinator owns exactly one active, correlated browser challenge.
// No token or challenge URL is ever placed in a diagnostic/event payload.
type captchaCoordinator struct {
	mu      sync.Mutex
	gate    chan struct{}
	current *captchaPending
}

type captchaPending struct {
	id       string
	url      string
	mode     string
	ctx      context.Context
	cancel   context.CancelFunc
	result   chan string
	launched bool
}

type captchaEvent struct {
	RequestID  string `json:"requestId"`
	Mode       string `json:"mode"`
	DeadlineMs int64  `json:"deadlineMs"`
}

func newCaptchaCoordinator() *captchaCoordinator {
	return &captchaCoordinator{gate: make(chan struct{}, 1)}
}

func (b *captchaCoordinator) begin(parent context.Context, url, mode string, timeout time.Duration) (*captchaPending, error) {
	select {
	case b.gate <- struct{}{}:
	case <-parent.Done():
		return nil, parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	r := &captchaPending{
		id: uuid.NewString(), url: url, mode: mode,
		ctx: ctx, cancel: cancel, result: make(chan string, 1),
	}
	b.mu.Lock()
	b.current = r
	b.mu.Unlock()
	return r, nil
}

func (b *captchaCoordinator) finish(r *captchaPending) {
	b.mu.Lock()
	if b.current == r {
		b.current = nil
	}
	b.mu.Unlock()
	r.cancel()
	<-b.gate
}

func (b *captchaCoordinator) active(id string) (context.Context, string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.current
	if r == nil || r.id != id || r.ctx.Err() != nil || r.launched {
		return nil, "", false
	}
	r.launched = true
	return r.ctx, r.url, true
}

func (b *captchaCoordinator) launchFailed(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current != nil && b.current.id == id {
		b.current.launched = false
	}
}

func (b *captchaCoordinator) submit(id, token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.current
	if r == nil || r.id != id || r.ctx.Err() != nil || strings.TrimSpace(token) == "" {
		return false
	}
	select {
	case r.result <- token:
		return true
	default:
		return false
	}
}

func (b *captchaCoordinator) cancel(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.current == nil || b.current.id != id {
		return false
	}
	b.current.cancel()
	return true
}

// CaptchaBrowserRequest returns private challenge data only to the native bridge.
// A request can launch only one browser; a failed startup can be retried.
func (c *Core) CaptchaBrowserRequest(id string) (context.Context, string, bool) {
	if c == nil || c.captcha == nil {
		return nil, "", false
	}
	return c.captcha.active(id)
}

func (c *Core) CaptchaBrowserLaunchFailed(id string) {
	if c != nil && c.captcha != nil {
		c.captcha.launchFailed(id)
	}
}

func (c *Core) SubmitCaptcha(id, token string) bool {
	return c != nil && c.captcha != nil && c.captcha.submit(id, token)
}

func (c *Core) CancelCaptcha(id string) bool {
	return c != nil && c.captcha != nil && c.captcha.cancel(id)
}

func (c *Core) waitForCaptcha(ctx context.Context, streamID int, captchaErr *VkCaptchaError, mode string, timeout time.Duration) (string, error) {
	if captchaErr == nil || captchaErr.RedirectURI == "" || captchaErr.SessionToken == "" {
		return "", errors.New("captcha challenge data incomplete")
	}
	if timeout <= 0 {
		return "", errors.New("captcha deadline missing")
	}
	r, err := c.captcha.begin(ctx, captchaErr.RedirectURI, mode, timeout)
	if err != nil {
		return "", err
	}
	defer c.captcha.finish(r)

	deadline, _ := r.ctx.Deadline()
	payload, _ := json.Marshal(captchaEvent{
		RequestID: r.id, Mode: mode, DeadlineMs: deadline.UnixMilli(),
	})
	c.emit(Event{Type: EventEvent, Name: "captcha_required", Data: string(payload)})
	c.emitConnectionProgress("vk", "running", "Требуется ручная CAPTCHA VK")
	defer c.emit(Event{Type: EventEvent, Name: "captcha_closed", Data: r.id})

	select {
	case token := <-r.result:
		if err := r.ctx.Err(); err != nil {
			return "", err
		}
		if strings.TrimSpace(token) == "" {
			return "", errors.New("empty captcha result")
		}
		c.emitConnectionProgress("vk", "running", "CAPTCHA решена, продолжаем подключение")
		return token, nil
	case <-r.ctx.Done():
		if errors.Is(r.ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("manual captcha timed out: %w", r.ctx.Err())
		}
		return "", r.ctx.Err()
	}
}

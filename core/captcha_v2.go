package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	mathrand "math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	neturl "net/url"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
)

const (
	captchaV2APIVersion    = "5.131"
	captchaV2ScriptVersion = "1.1.1370"
	captchaV2DeviceInfo    = `{"screenWidth":1920,"screenHeight":1080,"screenAvailWidth":1920,"screenAvailHeight":1040,"innerWidth":1920,"innerHeight":970,"devicePixelRatio":1,"language":"ru-RU","languages":["ru-RU","ru","en-US","en"],"webdriver":false,"hardwareConcurrency":8,"notificationsPermission":"default"}`
)

var (
	reCaptchaV2PowInput   = regexp.MustCompile(`const\s+powInput\s*=\s*"([^"]+)"`)
	reCaptchaV2Difficulty = regexp.MustCompile(`const\s+difficulty\s*=\s*(\d+)`)
	reCaptchaV2WindowInit = regexp.MustCompile(`window\.init\s*=\s*`)
	reCaptchaV2ScriptSrc  = regexp.MustCompile(`src="(https://[^"]+not_robot_captcha[^"]+)"`)
	reCaptchaV2DebugInfo  = regexp.MustCompile(`debug_info:(?:[^"]*\|\|)?"([a-fA-F0-9]{64})"`)
	reCaptchaV2Version    = regexp.MustCompile(`vkid/([0-9.]*)/not_robot_captcha\.js`)

	errCaptchaV2RateLimit = errors.New("captcha session rate limit reached")
	errCaptchaV2Bot       = errors.New("captcha bot challenge")

	captchaV2MaxAttempts = 10

	captchaV2DebugCache  sync.Map // scriptURL -> string
	captchaV2HeaderOrder = []string{
		"host",
		"content-length",
		"sec-ch-ua-platform",
		"accept-language",
		"sec-ch-ua",
		"content-type",
		"sec-ch-ua-mobile",
		"user-agent",
		"accept",
		"origin",
		"sec-fetch-site",
		"sec-fetch-mode",
		"sec-fetch-dest",
		"referer",
		"accept-encoding",
		"priority",
	}
	captchaV2PHeaderOrder = []string{":method", ":path", ":authority", ":scheme"}
)

type captchaV2Init struct {
	Data captchaV2InitData `json:"data"`
}

type captchaV2InitData struct {
	ShowCaptchaType string                 `json:"show_captcha_type"`
	CaptchaSettings []captchaV2InitSetting `json:"captcha_settings"`
}

type captchaV2InitSetting struct {
	Type     string `json:"type"`
	Settings string `json:"settings"`
}

// captchaV2Failure intentionally contains only bounded metadata. In particular,
// neither raw HTML, redirect URLs, cookies nor CAPTCHA tokens are printable.
type captchaV2Failure struct {
	Phase       string
	Kind        string
	Reason      string
	Status      int
	ContentType string
	Redirected  bool
	Bytes       int
	DurationMs  int64
}

func (e *captchaV2Failure) Error() string {
	return fmt.Sprintf("captcha v2 phase=%s category=%s reason=%s http_status=%d content_type=%s redirected=%t bytes=%d duration_ms=%d",
		e.Phase, e.Kind, e.Reason, e.Status, e.ContentType, e.Redirected, e.Bytes, e.DurationMs)
}

func captchaV2Terminal(err error) bool {
	var failure *captchaV2Failure
	return errors.As(err, &failure) && (failure.Kind == "parse" || failure.Kind == "unsupported_page" || failure.Kind == "http_status")
}

func captchaV2MediaType(value string) string {
	value, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "other"
	}
	switch value {
	case "text/html", "application/xhtml+xml", "application/json", "text/javascript", "application/javascript":
		return value
	default:
		return "other"
	}
}

type captchaV2Page struct {
	PowInput      string
	PowDifficulty int
	ScriptURL     string
	Init          *captchaV2Init
}

type captchaV2Check struct {
	Status       string
	SuccessToken string
	ShowType     string
}

type captchaV2ShowTypeError struct {
	ShowType string
}

func (e *captchaV2ShowTypeError) Error() string {
	return "captcha show type mismatch: " + e.ShowType
}

type captchaV2Session struct {
	ctx          context.Context
	client       tlsclient.HttpClient
	profile      Profile
	savedProfile *SavedProfile
}

func solveVkCaptchaV2(
	ctx context.Context,
	captchaErr *VkCaptchaError,
	client tlsclient.HttpClient,
	profile Profile,
	savedProfile *SavedProfile,
) (string, error) {
	return solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, captchaV2MaxAttempts)
}

func solveVkCaptchaV2Attempts(
	ctx context.Context,
	captchaErr *VkCaptchaError,
	client tlsclient.HttpClient,
	profile Profile,
	savedProfile *SavedProfile,
	maxAttempts int,
) (string, error) {
	if captchaErr == nil || captchaErr.SessionToken == "" {
		return "", fmt.Errorf("no session_token in redirect_uri")
	}
	if maxAttempts < 1 {
		maxAttempts = 10
	}
	log.Printf("[КАПЧА] Решаю VK Smart Captcha автоматически (v2, попыток=%d)...", maxAttempts)

	s := &captchaV2Session{ctx: ctx, client: client, profile: profile, savedProfile: savedProfile}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		token, solveErr := s.solveOnce(captchaErr)
		if solveErr == nil {
			return token, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		lastErr = solveErr
		log.Printf("[КАПЧА] v2 попытка %d/%d: %v", attempt, maxAttempts, solveErr)
		if captchaV2Terminal(solveErr) {
			return "", solveErr
		}
		if attempt == maxAttempts {
			break
		}
		delay := time.Duration(attempt)*500*time.Millisecond + time.Duration(mathrand.Intn(700))*time.Millisecond
		if errors.Is(solveErr, errCaptchaV2RateLimit) {
			delay = 5 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("captcha v2 attempts exhausted: %w", lastErr)
}

func (s *captchaV2Session) solveOnce(captchaErr *VkCaptchaError) (string, error) {
	html, err := s.fetchCaptchaHTML(captchaErr.RedirectURI)
	if err != nil {
		return "", err
	}

	page, err := parseCaptchaV2Page(html)
	if err != nil {
		log.Printf("[КАПЧА] v2 phase=parse_page result=error: %v", err)
		return "", err
	}
	if page.PowInput == "" {
		return "", &captchaV2Failure{Phase: "parse_page", Kind: "unsupported_page", Reason: "pow_missing", Bytes: len(html)}
	}

	sliderSettings := ""
	if page.Init != nil {
		for _, setting := range page.Init.Data.CaptchaSettings {
			if setting.Type == "slider" {
				sliderSettings = setting.Settings
			}
		}
	}
	if page.Init != nil && page.Init.Data.ShowCaptchaType == "slider" && sliderSettings == "" {
		return "", errors.New("failed to find slider captcha settings")
	}

	log.Printf("[КАПЧА] v2 solving pow difficulty=%d", page.PowDifficulty)
	hash := solveCaptchaPoWV2(s.ctx, page.PowInput, page.PowDifficulty)
	if hash == "" {
		return "", errors.New("captcha pow failed")
	}
	log.Printf("[КАПЧА] v2 pow solved")

	base := captchaV2BaseValues(captchaErr.SessionToken)
	if _, settingsErr := s.captchaRequest("captchaNotRobot.settings", base); settingsErr != nil {
		return "", fmt.Errorf("captcha settings failed: %w", settingsErr)
	}

	browserFP, err := captchaV2BrowserFP()
	if err != nil {
		return "", err
	}
	if s.savedProfile != nil && strings.TrimSpace(s.savedProfile.BrowserFp) != "" {
		browserFP = s.savedProfile.BrowserFp
	}

	if m := reCaptchaV2Version.FindStringSubmatch(page.ScriptURL); len(m) > 1 {
		if m[1] != captchaV2ScriptVersion {
			log.Printf("[КАПЧА] v2 script version drift: known=%s latest=%s", captchaV2ScriptVersion, m[1])
		}
	}

	debugInfo, err := s.fetchDebugInfo(page.ScriptURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch debug info: %w (script_version=%s)", err, captchaV2ScriptVersion)
	}

	showType := ""
	if page.Init != nil {
		showType = page.Init.Data.ShowCaptchaType
	}
	var token string
	for {
		log.Printf("[КАПЧА] v2 solving show_type=%s", showType)
		switch showType {
		case "slider":
			token, err = s.solveSliderCaptcha(captchaErr.SessionToken, browserFP, hash, sliderSettings, debugInfo)
		case "checkbox", "":
			token, err = s.solveCheckboxCaptcha(captchaErr.SessionToken, browserFP, hash, debugInfo)
		default:
			return "", fmt.Errorf("unsupported captcha type: %s", showType)
		}
		if err == nil {
			break
		}
		if errors.Is(err, errCaptchaV2Bot) && !strings.EqualFold(showType, "slider") && sliderSettings != "" {
			log.Printf("[КАПЧА] v2 checkbox returned BOT, trying slider")
			showType = "slider"
			continue
		}
		var stErr *captchaV2ShowTypeError
		if !errors.As(err, &stErr) || stErr.ShowType == "" {
			return "", err
		}
		showType = stErr.ShowType
	}

	if _, endErr := s.captchaRequest("captchaNotRobot.endSession", base); endErr != nil {
		log.Printf("[КАПЧА] v2 endSession failed: %v", endErr)
	}
	return token, nil
}

func captchaV2BaseValues(sessionToken string) [][2]string {
	return [][2]string{
		{"session_token", sessionToken},
		{"domain", "vk.com"},
		{"adFp", ""},
		{"access_token", ""},
	}
}

func captchaV2BrowserFP() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("browser fp generate: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *captchaV2Session) fetchCaptchaHTML(redirectURI string) (string, error) {
	body, err := s.doRaw("fetch_page", fhttp.MethodGet, redirectURI, nil, map[string]string{
		"Accept":         "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Sec-Fetch-Dest": "document",
		"Sec-Fetch-Mode": "navigate",
		"Sec-Fetch-Site": "cross-site",
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (s *captchaV2Session) fetchDebugInfo(scriptURL string) (string, error) {
	if cached, ok := captchaV2DebugCache.Load(scriptURL); ok {
		if cachedDebugInfo, ok := cached.(string); ok {
			return cachedDebugInfo, nil
		}
		captchaV2DebugCache.Delete(scriptURL)
	}
	body, err := s.doRaw("fetch_script", fhttp.MethodGet, scriptURL, nil, map[string]string{
		"Accept":  "text/javascript,*/*",
		"Referer": "https://id.vk.com/",
	})
	if err != nil {
		return "", err
	}
	m := reCaptchaV2DebugInfo.FindSubmatch(body)
	if len(m) < 2 {
		return "", errors.New("debug_info match not found")
	}
	v := string(m[1])
	captchaV2DebugCache.Store(scriptURL, v)
	log.Printf("[КАПЧА] v2 debug_info fetched")
	return v, nil
}

func parseCaptchaV2Page(html string) (*captchaV2Page, error) {
	page := &captchaV2Page{}
	marker := reCaptchaV2WindowInit.FindStringIndex(html)
	if marker == nil {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "unsupported_page", Reason: "init_missing", Bytes: len(html)}
	}
	// Use the JSON decoder, not a regex stopping at the first closing brace:
	// known VK init JSON may contain nested objects.
	decoder := json.NewDecoder(strings.NewReader(html[marker[1]:]))
	var init captchaV2Init
	if err := decoder.Decode(&init); err != nil {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "parse", Reason: "init_invalid_json", Bytes: len(html)}
	}
	if init.Data.ShowCaptchaType == "" {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "unsupported_page", Reason: "init_schema", Bytes: len(html)}
	}
	page.Init = &init

	match := reCaptchaV2ScriptSrc.FindStringSubmatch(html)
	if len(match) < 2 {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "unsupported_page", Reason: "script_missing", Bytes: len(html)}
	}
	page.ScriptURL = match[1]

	if m := reCaptchaV2PowInput.FindStringSubmatch(html); len(m) >= 2 {
		page.PowInput = m[1]
	}
	if page.PowInput == "" {
		return page, nil
	}

	match = reCaptchaV2Difficulty.FindStringSubmatch(html)
	if len(match) < 2 {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "unsupported_page", Reason: "difficulty_missing", Bytes: len(html)}
	}
	difficulty, err := strconv.Atoi(match[1])
	if err != nil || difficulty <= 0 {
		return nil, &captchaV2Failure{Phase: "parse_page", Kind: "parse", Reason: "difficulty_invalid", Bytes: len(html)}
	}
	page.PowDifficulty = difficulty
	return page, nil
}

func (s *captchaV2Session) captchaRequest(method string, form [][2]string) (map[string]any, error) {
	endpoint := "https://api.vk.ru/method/" + method + "?v=" + captchaV2APIVersion
	body, err := s.doRaw("api_request", fhttp.MethodPost, endpoint, form, map[string]string{
		"Origin":   "https://id.vk.com",
		"Referer":  "https://id.vk.com/",
		"Priority": "u=1, i",
	})
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("captcha api decode: %w", err)
	}
	return out, nil
}

func (s *captchaV2Session) performCaptchaCheck(
	sessionToken string,
	browserFP string,
	hash string,
	answerJSON string,
	cursor string,
	debugInfo string,
) (*captchaV2Check, error) {
	values := [][2]string{
		{"session_token", sessionToken},
		{"domain", "vk.com"},
		{"adFp", ""},
		{"accelerometer", "[]"},
		{"gyroscope", "[]"},
		{"motion", "[]"},
		{"cursor", cursor},
		{"taps", "[]"},
		{"connectionRtt", "[]"},
		{"connectionDownlink", "[]"},
		{"browser_fp", browserFP},
		{"hash", hash},
		{"answer", base64.StdEncoding.EncodeToString([]byte(answerJSON))},
		{"debug_info", debugInfo},
		{"access_token", ""},
	}
	resp, err := s.captchaRequest("captchaNotRobot.check", values)
	if err != nil {
		return nil, fmt.Errorf("captcha check failed: %w", err)
	}
	check, err := parseCaptchaV2Check(resp)
	if err != nil {
		return nil, err
	}
	if check.ShowType != "" {
		log.Printf("[КАПЧА] v2 check status=%s show_type=%s", check.Status, check.ShowType)
	} else {
		log.Printf("[КАПЧА] v2 check status=%s", check.Status)
	}
	return check, nil
}

func parseCaptchaV2Check(raw map[string]any) (*captchaV2Check, error) {
	resp, ok := raw["response"].(map[string]any)
	if !ok {
		return nil, errors.New("invalid captcha check response")
	}
	out := &captchaV2Check{
		Status:       captchaV2StringifyAny(resp["status"]),
		SuccessToken: captchaV2StringifyAny(resp["success_token"]),
		ShowType:     captchaV2StringifyAny(resp["show_captcha_type"]),
	}
	if out.Status == "" {
		return nil, errors.New("captcha check status missing")
	}
	return out, nil
}

func (s *captchaV2Session) solveCheckboxCaptcha(
	sessionToken string,
	browserFP string,
	hash string,
	debugInfo string,
) (string, error) {
	deviceJSON := captchaV2DeviceInfo
	if s.savedProfile != nil && strings.TrimSpace(s.savedProfile.DeviceJSON) != "" {
		deviceJSON = s.savedProfile.DeviceJSON
	}
	if _, err := s.captchaRequest("captchaNotRobot.componentDone", [][2]string{
		{"session_token", sessionToken},
		{"domain", "vk.com"},
		{"adFp", ""},
		{"browser_fp", browserFP},
		{"device", deviceJSON},
		{"access_token", ""},
	}); err != nil {
		return "", fmt.Errorf("captcha componentDone failed: %w", err)
	}

	select {
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	case <-time.After(time.Duration(400+mathrand.Intn(250)) * time.Millisecond):
	}

	check, err := s.performCaptchaCheck(sessionToken, browserFP, hash, "{}", "[]", debugInfo)
	if err != nil {
		return "", err
	}
	if check.ShowType != "" && !strings.EqualFold(check.ShowType, "checkbox") {
		return "", &captchaV2ShowTypeError{ShowType: check.ShowType}
	}
	if strings.EqualFold(check.Status, "error_limit") {
		return "", errCaptchaV2RateLimit
	}
	if strings.EqualFold(check.Status, "bot") {
		return "", fmt.Errorf("%w: checkbox captcha rejected: status=%s", errCaptchaV2Bot, check.Status)
	}
	if !strings.EqualFold(check.Status, "ok") {
		return "", fmt.Errorf("checkbox captcha rejected: status=%s", check.Status)
	}
	if check.SuccessToken == "" {
		return "", errors.New("captcha success token not found")
	}
	return check.SuccessToken, nil
}

func solveCaptchaPoWV2(ctx context.Context, input string, difficulty int) string {
	time.Sleep(time.Duration(200+mathrand.Intn(300)) * time.Millisecond)
	if input == "" || difficulty <= 0 {
		return ""
	}
	target := strings.Repeat("0", difficulty)
	for nonce := 1; nonce <= 10_000_000; nonce++ {
		if nonce%4096 == 0 {
			select {
			case <-ctx.Done():
				return ""
			default:
			}
		}
		sum := sha256.Sum256([]byte(input + strconv.Itoa(nonce)))
		hashHex := hex.EncodeToString(sum[:])
		if strings.HasPrefix(hashHex, target) {
			return hashHex
		}
	}
	return ""
}

func (s *captchaV2Session) doRaw(
	phase string,
	method string,
	endpoint string,
	form [][2]string,
	extraHeaders map[string]string,
) ([]byte, error) {
	started := time.Now()
	var body []byte
	if form != nil {
		body = []byte(captchaV2EncodeForm(form))
	}
	req, err := fhttp.NewRequestWithContext(s.ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &captchaV2Failure{Phase: phase, Kind: "network", Reason: "invalid_request"}
	}
	applyBrowserProfileFhttp(req, s.profile)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Origin", "https://vk.com")
	req.Header.Set("Referer", "https://vk.com/")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	req.Header[fhttp.HeaderOrderKey] = captchaV2HeaderOrder
	req.Header[fhttp.PHeaderOrderKey] = captchaV2PHeaderOrder

	resp, err := s.client.Do(req)
	if err != nil {
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		failure := &captchaV2Failure{Phase: phase, Kind: "network", Reason: "request_failed", DurationMs: time.Since(started).Milliseconds()}
		log.Printf("[КАПЧА] v2 %v", failure)
		return nil, failure
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[КАПЧА] v2 response body close failed phase=%s", phase)
		}
	}()

	const maxCaptchaResponseBytes = 2 << 20
	buf, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCaptchaResponseBytes+1))
	redirected := resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.String() != req.URL.String()
	failure := &captchaV2Failure{
		Phase: phase, Status: resp.StatusCode,
		ContentType: captchaV2MediaType(resp.Header.Get("Content-Type")),
		Redirected: redirected, Bytes: len(buf),
		DurationMs: time.Since(started).Milliseconds(),
	}
	switch {
	case readErr != nil:
		if ctxErr := s.ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		failure.Kind, failure.Reason = "network", "read_failed"
	case len(buf) > maxCaptchaResponseBytes:
		failure.Kind, failure.Reason = "unsupported_page", "body_too_large"
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		failure.Kind, failure.Reason = "http_status", "http_non_success"
	case phase == "fetch_page" && failure.ContentType != "text/html" && failure.ContentType != "application/xhtml+xml":
		failure.Kind, failure.Reason = "unsupported_page", "not_html"
	default:
		log.Printf("[КАПЧА] v2 phase=%s result=ok http_status=%d content_type=%s redirected=%t bytes=%d duration_ms=%d",
			phase, failure.Status, failure.ContentType, failure.Redirected, failure.Bytes, failure.DurationMs)
		return buf, nil
	}
	log.Printf("[КАПЧА] v2 %v", failure)
	return nil, failure
}

func captchaV2EncodeForm(values [][2]string) string {
	if len(values) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, kv := range values {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(captchaV2QueryEscape(kv[0]))
		sb.WriteByte('=')
		sb.WriteString(captchaV2QueryEscape(kv[1]))
	}
	return sb.String()
}

func captchaV2QueryEscape(s string) string {
	const upper = "0123456789ABCDEF"
	hexDigits := func(b byte) [3]byte {
		return [3]byte{'%', upper[b>>4], upper[b&0xF]}
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			out = append(out, '+')
		case ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~':
			out = append(out, c)
		default:
			h := hexDigits(c)
			out = append(out, h[:]...)
		}
	}
	return string(out)
}

func captchaV2StringifyAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	}
}

func applyBrowserProfileFhttp(req *fhttp.Request, profile Profile) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("sec-ch-ua", profile.SecChUa)
	req.Header.Set("sec-ch-ua-mobile", profile.SecChUaMobile)
	req.Header.Set("sec-ch-ua-platform", profile.SecChUaPlatform)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("DNT", "1")
}

type VkCaptchaError struct {
	ErrorCode      int
	ErrorMsg       string
	CaptchaSid     string
	CaptchaImg     string
	RedirectURI    string
	SessionToken   string
	CaptchaTs      string
	CaptchaAttempt string
}

func (e *VkCaptchaError) Error() string {
	if e == nil {
		return "VK captcha required"
	}
	if e.ErrorCode != 0 && e.ErrorCode != 14 {
		if e.ErrorMsg != "" {
			return fmt.Sprintf("VK API error %d: %s", e.ErrorCode, e.ErrorMsg)
		}
		return fmt.Sprintf("VK API error %d", e.ErrorCode)
	}
	if e.RedirectURI != "" {
		return fmt.Sprintf("VK captcha required: redirect_uri, sid=%q", e.CaptchaSid)
	}
	if e.CaptchaImg != "" {
		return fmt.Sprintf("VK captcha required: captcha_img, sid=%q", e.CaptchaSid)
	}
	if e.CaptchaSid != "" {
		return fmt.Sprintf("VK captcha required: sid=%q", e.CaptchaSid)
	}
	if e.ErrorMsg != "" {
		return fmt.Sprintf("VK captcha required: %s", e.ErrorMsg)
	}
	return "VK captcha required"
}

func parseVkCaptchaError(errData map[string]interface{}) *VkCaptchaError {
	codeFloat, _ := errData["error_code"].(float64)
	redirectUri, _ := errData["redirect_uri"].(string)
	errorMsg, _ := errData["error_msg"].(string)
	captchaImg, _ := errData["captcha_img"].(string)

	captchaSid, _ := errData["captcha_sid"].(string)
	if captchaSid == "" {
		if sidNum, ok := errData["captcha_sid"].(float64); ok {
			captchaSid = fmt.Sprintf("%.0f", sidNum)
		}
	}

	var sessionToken string
	if redirectUri != "" {
		if parsed, err := neturl.Parse(redirectUri); err == nil {
			sessionToken = parsed.Query().Get("session_token")
		}
	}

	var captchaTs string
	if tsFloat, ok := errData["captcha_ts"].(float64); ok {
		captchaTs = fmt.Sprintf("%.0f", tsFloat)
	} else if tsStr, ok := errData["captcha_ts"].(string); ok {
		captchaTs = tsStr
	}

	var captchaAttempt string
	if attFloat, ok := errData["captcha_attempt"].(float64); ok {
		captchaAttempt = fmt.Sprintf("%.0f", attFloat)
	} else if attStr, ok := errData["captcha_attempt"].(string); ok {
		captchaAttempt = attStr
	}

	return &VkCaptchaError{
		ErrorCode:      int(codeFloat),
		ErrorMsg:       errorMsg,
		CaptchaSid:     captchaSid,
		CaptchaImg:     captchaImg,
		RedirectURI:    redirectUri,
		SessionToken:   sessionToken,
		CaptchaTs:      captchaTs,
		CaptchaAttempt: captchaAttempt,
	}
}

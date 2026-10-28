package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

type VKCredentials struct {
	ClientID     string
	ClientSecret string
}

var vkCredentialsList = loadVKCredentials()

func deobf(s string, shift int) string {
	b := []byte(s)
	for i := range b {
		b[i] = byte(int(b[i]) + shift)
	}
	return string(b)
}

func loadVKCredentials() []VKCredentials {
	if env := os.Getenv("WDTT_VK_CREDENTIALS"); env != "" {
		creds, err := parseVKCredentialsEnv(env)
		if err != nil {
			log.Printf("[VK Auth] WDTT_VK_CREDENTIALS parse error: %v; using fallback", err)
		} else if len(creds) > 0 {
			log.Printf("[VK Auth] Loaded %d VK credential set(s) from environment", len(creds))
			return creds
		}
	}
	log.Printf("[VK Auth] WARNING: using embedded VK credentials.")
	return []VKCredentials{

		{ClientID: deobf(";535939", -3), ClientSecret: deobf("oPUvWlPF|Sqs8yirogpq", -3)},

		{ClientID: deobf("95;:7;:", -3), ClientSecret: deobf("PxD{IdNG\\GRLF}JqHRks", -3)},
	}
}

func parseVKCredentialsEnv(env string) ([]VKCredentials, error) {
	var out []VKCredentials
	for _, pair := range strings.Split(env, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid credential pair %q (expected id:secret)", pair)
		}
		id, secret := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if id == "" || secret == "" {
			return nil, fmt.Errorf("empty id or secret in pair %q", pair)
		}
		out = append(out, VKCredentials{ClientID: id, ClientSecret: secret})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no credentials found")
	}
	return out, nil
}

type CallUnavailableError struct {
	Code    int
	Message string
}

func (e *CallUnavailableError) Error() string {
	if e == nil {
		return "VK call is unavailable"
	}
	if e.Message != "" {
		return fmt.Sprintf("VK returns error: %s (error_code=%d)", e.Message, e.Code)
	}
	return fmt.Sprintf("VK call is unavailable (error_code=%d)", e.Code)
}

func asCallUnavailableError(err error) (*CallUnavailableError, bool) {
	var callErr *CallUnavailableError
	if errors.As(err, &callErr) {
		return callErr, true
	}
	return nil, false
}

func fatalCallError(resp map[string]interface{}) *CallUnavailableError {
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		return nil
	}

	code := vkErrorCode(errObj["error_code"])
	switch {
	case code == 951, code == 954:

	case code >= 9000 && code <= 9999:

	default:
		return nil
	}

	msg, _ := errObj["error_msg"].(string)
	return &CallUnavailableError{Code: code, Message: msg}
}

func vkErrorCode(raw interface{}) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

var knownVKCredentials = map[string]VKCredentials{
	"6287487": {ClientID: "6287487", ClientSecret: "MuAxFaKDYDOICzGnEOhp"},
	"8202606": {ClientID: "8202606", ClientSecret: "lMRsTiMCyPnp5vfoldmn"},
}

func SetActiveClientIds(ids string) {
	if ids == "" {
		return
	}
	var newCreds []VKCredentials
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if cred, ok := knownVKCredentials[id]; ok {
			newCreds = append(newCreds, cred)
		}
	}
	if len(newCreds) > 0 {
		vkCredentialsList = newCreds
	}
}

func GetActiveClientIdsString() string {
	var ids []string
	for _, cred := range vkCredentialsList {
		ids = append(ids, cred.ClientID)
	}
	return strings.Join(ids, ", ")
}

const vkCredentialAttemptLimit = 4

type TurnCredentials struct {
	Username    string
	Password    string
	ServerAddrs []string
	ExpiresAt   time.Time
	Link        string
}

type StreamCredentialsCache struct {
	creds         TurnCredentials
	mutex         sync.RWMutex
	errorCount    atomic.Int32
	lastErrorTime atomic.Int64
}

const (
	credentialLifetime = 10 * time.Minute
	cacheSafetyMargin  = 60 * time.Second
	maxCacheErrors     = 3
	errorWindow        = 10 * time.Second
)

var streamsPerCache = 10

func getCacheID(streamID int) int {
	return streamID / streamsPerCache
}

var credentialsStore = struct {
	mu     sync.RWMutex
	caches map[int]*StreamCredentialsCache
}{
	caches: make(map[int]*StreamCredentialsCache),
}

func getStreamCache(streamID int) *StreamCredentialsCache {
	cacheID := getCacheID(streamID)

	credentialsStore.mu.RLock()
	cache, exists := credentialsStore.caches[cacheID]
	credentialsStore.mu.RUnlock()

	if exists {
		return cache
	}

	credentialsStore.mu.Lock()
	defer credentialsStore.mu.Unlock()

	if cache, exists = credentialsStore.caches[cacheID]; exists {
		return cache
	}

	cache = &StreamCredentialsCache{}
	credentialsStore.caches[cacheID] = cache
	return cache
}

func (c *StreamCredentialsCache) invalidate(streamID int) {
	c.mutex.Lock()
	c.creds = TurnCredentials{}
	c.mutex.Unlock()

	c.errorCount.Store(0)
	c.lastErrorTime.Store(0)

	log.Printf("[STREAM %d] [VK Auth] Credentials cache invalidated", streamID)
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "Unauthorized") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "invalid credential") ||
		strings.Contains(errStr, "stale nonce")
}

func handleAuthError(streamID int) bool {
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	now := time.Now().Unix()

	if now-cache.lastErrorTime.Load() > int64(errorWindow.Seconds()) {
		cache.errorCount.Store(0)
	}

	count := cache.errorCount.Add(1)
	cache.lastErrorTime.Store(now)

	log.Printf("[STREAM %d] Auth error (cache=%d, count=%d/%d)", streamID, cacheID, count, maxCacheErrors)

	if count >= maxCacheErrors {
		log.Printf("[VK Auth] Multiple auth errors detected (%d), invalidating cache %d", count, cacheID)
		cache.invalidate(streamID)
		return true
	}
	return false
}

var globalCaptchaLockout atomic.Int64

const captchaManualWebViewTimeout = 60 * time.Second

func vkDelayRandom(ctx context.Context, minMs, maxMs int) error {
	ms := minMs + rand.Intn(maxMs-minMs+1)
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func getVkCredsCached(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, vkCredentialsDeadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	for !cache.mutex.TryRLock() {
		select {
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		expires := time.Until(cache.creds.ExpiresAt)
		u, p := cache.creds.Username, cache.creds.Password
		addr := cache.creds.ServerAddrs[streamID%len(cache.creds.ServerAddrs)]
		addrs := cloneStringSlice(cache.creds.ServerAddrs)
		cache.mutex.RUnlock()
		log.Printf("[STREAM %d] [VK Auth] Using cached credentials (cache=%d, expires in %v, selected=%s, urls=%d)", streamID, cacheID, expires.Truncate(time.Second), addr, len(addrs))
		return u, p, addrs, nil
	}
	cache.mutex.RUnlock()

	// A concurrent refresh must not trap canceled streams behind a mutex.
	for !cache.mutex.TryLock() {
		select {
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer cache.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}

	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		return cache.creds.Username, cache.creds.Password, cloneStringSlice(cache.creds.ServerAddrs), nil
	}

	user, pass, addrs, err := fetchVkCredsSerialized(ctx, link, streamID)
	if err != nil {
		return "", "", nil, err
	}

	cache.creds = TurnCredentials{
		Username:    user,
		Password:    pass,
		ServerAddrs: addrs,
		ExpiresAt:   time.Now().Add(credentialLifetime - cacheSafetyMargin),
		Link:        link,
	}
	return user, pass, cloneStringSlice(addrs), nil
}

var (
	vkRequestGate         = make(chan struct{}, 1)
	globalLastVkFetchTime time.Time
)

func fetchVkCredsSerialized(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, vkCredentialsDeadline)
	defer cancel()

	select {
	case vkRequestGate <- struct{}{}:
	case <-ctx.Done():
		return "", "", nil, ctx.Err()
	}
	defer func() { <-vkRequestGate }()
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}

	minInterval := 3*time.Second + time.Duration(rand.Intn(3000))*time.Millisecond
	elapsed := time.Since(globalLastVkFetchTime)

	if !globalLastVkFetchTime.IsZero() && elapsed < minInterval {
		wait := minInterval - elapsed
		log.Printf("[STREAM %d] [VK Auth] Throttling: waiting %v to prevent rate limit...", streamID, wait.Truncate(time.Millisecond))
		select {
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	defer func() {
		globalLastVkFetchTime = time.Now()
	}()

	return fetchVkCreds(ctx, link, streamID)
}

// Only a typed VK Calls step-5 expiry permits replay of the short-lived
// anonymous chain. Never replay CAPTCHA, rate-limit, call or network errors.
func shouldRetryVKCallsOutdatedToken(err error) bool {
	var failure *vkCallsFailure
	return errors.As(err, &failure) &&
		failure.Kind == vkCallsFailureOKCDN &&
		strings.HasPrefix(failure.Step, "step5 ") &&
		strings.Contains(strings.ToLower(failure.Error()), "error.webrtc.auth.anonym_token.outdated")
}

func fetchVkCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if time.Now().Unix() < globalCaptchaLockout.Load() {
		return "", "", nil, errVKCaptchaWait
	}
	if getVKAuthMode() == "vkcalls" {
		user, pass, addrs, err := getVKCredsViaVKCallsPath(ctx, link, streamID)
		if shouldRetryVKCallsOutdatedToken(err) {
			log.Printf("[STREAM %d] [VK Auth] Step 5 anonymous token expired; retry once", streamID)
			select {
			case <-ctx.Done(): return "", "", nil, ctx.Err()
			case <-time.After(750 * time.Millisecond):
			}
			user, pass, addrs, err = getVKCredsViaVKCallsPath(ctx, link, streamID)
			if err != nil {
				// The second error, including another expiry, is final.
				return "", "", nil, err
			}
		}
		if err == nil {
			log.Printf("[STREAM %d] [VK Auth] Success via VK Calls path", streamID)
			return user, pass, addrs, nil
		}
		kind := classifyVKAuthError(err)
		if !vkAuthShouldUseLegacy(err) {
			log.Printf("[STREAM %d] [VK Auth] VK Calls stopped: error_kind=%s", streamID, kind)
			return "", "", nil, err
		}
		log.Printf("[STREAM %d] [VK Auth] VK Calls failure kind=%s; legacy fallback allowed", streamID, kind)
	} else {
		log.Printf("[STREAM %d] [VK Auth] Legacy mode selected", streamID)
	}
	jar := tlsclient.NewCookieJar()
	return tryVKCredentialPairs(ctx, vkCredentialsList, func(ctx context.Context, creds VKCredentials, attempt int) (string, string, []string, error) {
		log.Printf("[STREAM %d] [VK Auth] Legacy credentials attempt %d", streamID, attempt)
		return getTokenChain(ctx, link, streamID, creds, jar, attempt)
	})
}

func getTokenChain(ctx context.Context, link string, streamID int, creds VKCredentials, jar tlsclient.CookieJar, attempt int) (string, string, []string, error) {
	profile := getRandomProfile()

	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutSeconds(20),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithCookieJar(jar),
	)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to initialize tls_client: %w", err)
	}

	name := generateName()
	escapedName := neturl.QueryEscape(name)

	log.Printf("[STREAM %d] [VK Auth] Identity ready | UA: %s", streamID, profile.UserAgent)

	doRequest := func(step, data, url string) (resp map[string]interface{}, err error) {
		parsedURL, err := neturl.Parse(url)
		if err != nil {
			return nil, fmt.Errorf("parse request URL: %w", err)
		}
		domain := parsedURL.Hostname()
		started := time.Now()
		defer func() { traceVKAuthAttempt(streamID, "legacy."+step, domain, attempt, started, err) }()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, err := fhttp.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer([]byte(data)))
		if err != nil {
			return nil, err
		}

		req.Host = domain
		applyBrowserProfileFhttp(req, profile)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://vk.ru")
		req.Header.Set("Referer", "https://vk.ru/")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Priority", "u=1, i")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, newVKAuthTransportError(ctx, "legacy."+step, domain, err)
		}
		defer func() {
			if closeErr := httpResp.Body.Close(); closeErr != nil {
				log.Printf("close response body: %s", closeErr)
			}
		}()

		if httpResp.StatusCode >= 400 {
			return nil, &vkAuthHTTPError{StatusCode: httpResp.StatusCode}
		}
		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, newVKAuthTransportError(ctx, "legacy."+step, domain, err)
		}

		err = json.Unmarshal(body, &resp)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	data := fmt.Sprintf("client_id=%s&token_type=messages&client_secret=%s&version=1&app_id=%s", creds.ClientID, creds.ClientSecret, creds.ClientID)
	resp, err := doRequest("anonymous_token", data, "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return "", "", nil, err
	}
	if errObj, exists := resp["error"].(map[string]interface{}); exists {
		return "", "", nil, &vkAuthAPIError{Code: vkErrorCode(errObj["error_code"])}
	}
	dataMap, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("unexpected anon token response: %v", resp)
	}
	token1, ok := dataMap["access_token"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing access_token in response: %v", resp)
	}

	if err := vkDelayRandom(ctx, 100, 150); err != nil {
		return "", "", nil, err
	}

	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&fields=photo_200&access_token=%s", link, token1)
	resp, err = doRequest("call_preview", data, "https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id="+creds.ClientID)
	if err != nil {
		if vkAuthIsTransport(classifyVKAuthError(err)) || classifyVKAuthError(err) == vkAuthRateLimit || errors.Is(err, context.Canceled) {
			return "", "", nil, err
		}
		log.Printf("[STREAM %d] [VK Auth] getCallPreview returned non-transport error kind=%s", streamID, classifyVKAuthError(err))
	} else if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
		if callErr := fatalCallError(resp); callErr != nil {
			return "", "", nil, callErr
		}
		apiErr := &vkAuthAPIError{Code: vkErrorCode(errObj["error_code"])}
		switch classifyVKAuthError(apiErr) {
		case vkAuthRateLimit, vkAuthCaptcha, vkAuthInvalidClient:
			return "", "", nil, apiErr
		}
	}

	if err := vkDelayRandom(ctx, 200, 400); err != nil {
		return "", "", nil, err
	}

	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, escapedName, token1)
	urlAddr := fmt.Sprintf("https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id=%s", creds.ClientID)

	var token2 string
	var savedProfile *SavedProfile
	savedProfile, _ = LoadProfileFromDisk()

	for attempt := 0; ; attempt++ {
		resp, err = doRequest("anonymous_call_token", data, urlAddr)
		if err != nil {
			return "", "", nil, err
		}

		if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
			if callErr := fatalCallError(resp); callErr != nil {
				log.Printf("[STREAM %d] [VK Auth] getAnonymousToken returned non-retryable call error: %v", streamID, callErr)
				return "", "", nil, callErr
			}

			captchaErr := parseVkCaptchaError(errObj)
			if captchaErr != nil && captchaErr.RedirectURI != "" && captchaErr.SessionToken != "" {
				if attempt >= 3 {
					log.Printf("[STREAM %d] [Captcha] Max attempts reached", streamID)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, errVKCaptchaWait
				}

				successToken, solveErr := solveCaptchaBySelectedMode(ctx, streamID, attempt+1, captchaErr, client, profile, savedProfile)
				if solveErr != nil {
					if ctx.Err() != nil {
						return "", "", nil, ctx.Err()
					}
					if errors.Is(solveErr, context.Canceled) || errors.Is(solveErr, context.DeadlineExceeded) {
						return "", "", nil, solveErr
					}
					log.Printf("[STREAM %d] [Captcha] Solve failed: %v", streamID, solveErr)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, errVKCaptchaWait
				}

				captchaAttempt := captchaErr.CaptchaAttempt
				if captchaAttempt == "0" || captchaAttempt == "" {
					captchaAttempt = "1"
				}

				data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&captcha_key=&captcha_sid=%s&is_sound_captcha=0&success_token=%s&captcha_ts=%s&captcha_attempt=%s&access_token=%s",
					link, escapedName, captchaErr.CaptchaSid, neturl.QueryEscape(successToken), captchaErr.CaptchaTs, captchaAttempt, token1)
				continue
			}
			return "", "", nil, &vkAuthAPIError{Code: vkErrorCode(errObj["error_code"])}
		}

		respMap, okLoop := resp["response"].(map[string]interface{})
		if !okLoop {
			return "", "", nil, fmt.Errorf("unexpected getAnonymousToken response: %v", resp)
		}
		token2, okLoop = respMap["token"].(string)
		if !okLoop {
			return "", "", nil, fmt.Errorf("missing token in response: %v", resp)
		}
		break
	}

	if err := vkDelayRandom(ctx, 100, 150); err != nil {
		return "", "", nil, err
	}

	sessionData := fmt.Sprintf(`{"version":2,"device_id":"%s","client_version":1.1,"client_type":"SDK_JS"}`, uuid.New())
	data = fmt.Sprintf("session_data=%s&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", neturl.QueryEscape(sessionData))
	resp, err = doRequest("okcdn_login", data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}
	token3, ok := resp["session_key"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing session_key in response: %v", resp)
	}

	if err := vkDelayRandom(ctx, 100, 150); err != nil {
		return "", "", nil, err
	}

	data = fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&capabilities=2F7F&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3)
	resp, err = doRequest("okcdn_join", data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}

	tsRaw, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("missing turn_server in response: %v", resp)
	}
	user, ok := tsRaw["username"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing username in turn_server")
	}
	pass, ok := tsRaw["credential"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing credential in turn_server")
	}
	urlsRaw, ok := tsRaw["urls"].([]interface{})
	if !ok || len(urlsRaw) == 0 {
		return "", "", nil, fmt.Errorf("missing or empty urls in turn_server")
	}

	log.Printf("[STREAM %d] [VK Auth] TURN urls (%d total):", streamID, len(urlsRaw))
	for i, u := range urlsRaw {
		log.Printf("[STREAM %d] [VK Auth]   [%d] %v", streamID, i, u)
	}

	var addresses []string
	for _, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			continue
		}
		clean := strings.Split(urlStr, "?")[0]
		address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
		addresses = append(addresses, address)
	}

	if len(addresses) == 0 {
		return "", "", nil, fmt.Errorf("no valid TURN addresses found")
	}

	return user, pass, addresses, nil
}

func solveCaptchaBySelectedMode(
	ctx context.Context,
	streamID int,
	attempt int,
	captchaErr *VkCaptchaError,
	client tlsclient.HttpClient,
	profile Profile,
	savedProfile *SavedProfile,
) (string, error) {
	mode := getCaptchaMode()
	if mode != "wv" {
		log.Printf("[STREAM %d] [КАПЧА] Go v2: запуск (attempt %d)", streamID, attempt)
		token, err := solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 2)
		if err == nil {
			return token, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Re-running the same unsupported page is not a useful fallback.
		log.Printf("[STREAM %d] [КАПЧА] Go v2 failed (%v); ручное решение VK", streamID, err)
	}
	return requestWebViewCaptcha(ctx, streamID, captchaErr, "manual", captchaManualWebViewTimeout)
}

func requestWebViewCaptcha(ctx context.Context, streamID int, captchaErr *VkCaptchaError, mode string, timeout time.Duration) (string, error) {
	c := captchaCoreFromContext(ctx)
	if c == nil {
		return "", errors.New("captcha browser unavailable: no running core")
	}
	return c.waitForCaptcha(ctx, streamID, captchaErr, mode, timeout)
}

func GetCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	return getVkCredsCached(ctx, link, streamID)
}

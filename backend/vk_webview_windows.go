//go:build windows

package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
)

const (
	vkAuthWindowStyle = 0x00C00000 | 0x00080000 | 0x00020000 | 0x10000000
	vkSWShow          = 5
	vkPMRemove        = 0x0001
	vkWMClose         = 0x0010
	vkCOInitSTA       = 0x2

	vkLegacyMobileUserAgent = "Mozilla/5.0 (Linux; Android 14; K; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/131.0.0.0 Mobile Safari/537.36"
)

type vkWinPoint struct {
	X int32
	Y int32
}

type vkWinMessage struct {
	HWND     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Point    vkWinPoint
	LPrivate uint32
}

type vkWebViewResult struct {
	token vkLegacyToken
	err   error
}

type vkWebViewEvent struct {
	URL           string `json:"url"`
	UnknownMethod bool   `json:"unknownMethod"`
}

var (
	vkUser32DLL          = syscall.NewLazyDLL("user32.dll")
	vkCreateWindowExProc = vkUser32DLL.NewProc("CreateWindowExW")
	vkShowWindowProc     = vkUser32DLL.NewProc("ShowWindow")
	vkDestroyWindowProc  = vkUser32DLL.NewProc("DestroyWindow")
	vkIsWindowProc       = vkUser32DLL.NewProc("IsWindow")
	vkPeekMessageProc    = vkUser32DLL.NewProc("PeekMessageW")
	vkTranslateMsgProc   = vkUser32DLL.NewProc("TranslateMessage")
	vkDispatchMsgProc    = vkUser32DLL.NewProc("DispatchMessageW")
	vkPostMessageProc    = vkUser32DLL.NewProc("PostMessageW")
	vkOle32DLL           = syscall.NewLazyDLL("ole32.dll")
	vkCoInitializeProc   = vkOle32DLL.NewProc("CoInitializeEx")
	vkCoUninitializeProc = vkOle32DLL.NewProc("CoUninitialize")
)

func obtainLegacyVKTokenInWebView(ctx context.Context) (vkLegacyToken, error) {
	resultCh := make(chan vkWebViewResult, 1)
	go runLegacyVKWebView(ctx, false, resultCh)

	select {
	case <-ctx.Done():
		return vkLegacyToken{}, context.Canceled
	case result := <-resultCh:
		return result.token, result.err
	}
}

func clearLegacyVKWebViewCookies(ctx context.Context) error {
	resultCh := make(chan vkWebViewResult, 1)
	go runLegacyVKWebView(ctx, true, resultCh)

	select {
	case <-ctx.Done():
		return context.Canceled
	case result := <-resultCh:
		return result.err
	}
}

func runLegacyVKWebView(ctx context.Context, clearCookies bool, resultCh chan<- vkWebViewResult) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var once sync.Once
	finish := func(result vkWebViewResult) {
		once.Do(func() {
			resultCh <- result
		})
	}

	hr, _, _ := vkCoInitializeProc.Call(0, vkCOInitSTA)
	if int32(hr) < 0 {
		finish(vkWebViewResult{err: fmt.Errorf("не удалось инициализировать WebView2 COM: 0x%08x", uint32(hr))})
		return
	}
	defer vkCoUninitializeProc.Call()

	hwnd, err := createVKAuthWindow(clearCookies)
	if err != nil {
		finish(vkWebViewResult{err: err})
		return
	}
	defer vkDestroyWindowProc.Call(hwnd)

	chromium := edge.NewChromium()
	chromium.DataPath = vkWebViewDataPath()
	chromium.AdditionalBrowserArgs = []string{"--lang=ru-RU"}
	chromium.SetErrorCallback(func(webViewErr error) {
		finish(vkWebViewResult{err: fmt.Errorf("WebView2 VK: %w", webViewErr)})
	})

	messageCh := make(chan string, 16)
	chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		select {
		case messageCh <- message:
		default:
		}
	}

	if !chromium.Embed(hwnd) {
		finish(vkWebViewResult{err: errors.New("не удалось запустить WebView2 для VK")})
		return
	}
	defer func() {
		chromium.ShuttingDown()
		if controller := chromium.GetController(); controller != nil {
			controller.Release()
		}
	}()

	chromium.Resize()
	cookieManager, managerErr := chromium.GetCookieManager()
	if managerErr != nil {
		finish(vkWebViewResult{err: fmt.Errorf("не удалось открыть хранилище cookies VK: %w", managerErr)})
		return
	}
	defer cookieManager.Release()

	if clearCookies {
		if deleteErr := cookieManager.DeleteAllCookies(); deleteErr != nil {
			finish(vkWebViewResult{err: fmt.Errorf("не удалось удалить cookies VK: %w", deleteErr)})
			return
		}
		finish(vkWebViewResult{})
		return
	}

	if err := applyLegacyVKWebViewSettings(chromium, 0); err != nil {
		finish(vkWebViewResult{err: err})
		return
	}

	chromium.Init(`(function(){
		function pwdttEmit(){
			try {
				var body = (document.body && document.body.innerText) || '';
				window.chrome.webview.postMessage(JSON.stringify({
					url: String(window.location.href || ''),
					unknownMethod: body.toLowerCase().indexOf('unknown method') !== -1
				}));
			} catch (_) {}
		}
		pwdttEmit();
		window.addEventListener('load', pwdttEmit);
		window.addEventListener('hashchange', pwdttEmit);
		window.addEventListener('popstate', pwdttEmit);
		setInterval(pwdttEmit, 400);
	})();`)

	loginAttempt := 0
	phase := "login"
	var retryNotBefore time.Time
	chromium.Navigate(legacyVKLoginStartURL(loginAttempt))
	vkShowWindowProc.Call(hwnd, vkSWShow)

	for {
		select {
		case <-ctx.Done():
			vkPostMessageProc.Call(hwnd, vkWMClose, 0, 0)
			finish(vkWebViewResult{err: context.Canceled})
			return
		case message := <-messageCh:
			var event vkWebViewEvent
			if err := json.Unmarshal([]byte(message), &event); err != nil {
				continue
			}

			if phase == "login" {
				if event.UnknownMethod && time.Now().After(retryNotBefore) {
					if loginAttempt >= 2 {
						finish(vkWebViewResult{err: errors.New("VK ID отклонил все три варианта входа: Unknown method passed")})
						return
					}
					if err := cookieManager.DeleteAllCookies(); err != nil {
						finish(vkWebViewResult{err: fmt.Errorf("не удалось очистить VK-сессию перед повтором: %w", err)})
						return
					}
					loginAttempt++
					if err := applyLegacyVKWebViewSettings(chromium, loginAttempt); err != nil {
						finish(vkWebViewResult{err: err})
						return
					}
					retryNotBefore = time.Now().Add(1500 * time.Millisecond)
					chromium.Navigate(legacyVKLoginStartURL(loginAttempt))
					continue
				}

				hasSession, sessionErr := hasLegacyVKSessionCookie(cookieManager)
				if sessionErr != nil {
					finish(vkWebViewResult{err: fmt.Errorf("не удалось проверить VK-сессию: %w", sessionErr)})
					return
				}
				if !hasSession || isLegacyVKIDLoginFlow(event.URL) {
					continue
				}

				phase = "token"
				chromium.Navigate(buildLegacyVKAuthorizeURL())
				continue
			}

			token, terminal, parseErr := parseLegacyVKTokenURL(event.URL)
			if !terminal {
				continue
			}
			vkPostMessageProc.Call(hwnd, vkWMClose, 0, 0)
			finish(vkWebViewResult{token: token, err: parseErr})
			return
		default:
		}

		if isWindow, _, _ := vkIsWindowProc.Call(hwnd); isWindow == 0 {
			finish(vkWebViewResult{err: errors.New("окно авторизации VK закрыто")})
			return
		}

		pumpVKWindowMessages()
		time.Sleep(10 * time.Millisecond)
	}
}

func applyLegacyVKWebViewSettings(chromium *edge.Chromium, attempt int) error {
	settings, err := chromium.GetSettings()
	if err != nil {
		return fmt.Errorf("не удалось получить настройки WebView2 VK: %w", err)
	}
	defer settings.Release()

	if err := settings.PutIsScriptEnabled(true); err != nil {
		return fmt.Errorf("не удалось включить JavaScript для VK: %w", err)
	}
	if err := settings.PutIsWebMessageEnabled(true); err != nil {
		return fmt.Errorf("не удалось включить WebView2 messages для VK: %w", err)
	}

	userAgent := vkLegacyMobileUserAgent
	if attempt >= 2 {
		userAgent = vkLegacyDesktopUserAgent
	}
	if err := settings.PutUserAgent(userAgent); err != nil {
		return fmt.Errorf("не удалось установить User-Agent VK: %w", err)
	}
	return nil
}

func hasLegacyVKSessionCookie(manager *edge.ICoreWebView2CookieManager) (bool, error) {
	for _, origin := range []string{"https://vk.ru", "https://vk.com", "https://m.vk.ru", "https://m.vk.com"} {
		list, err := manager.GetCookies(origin)
		if err != nil {
			return false, err
		}
		count, err := list.GetCount()
		if err != nil {
			list.Release()
			return false, err
		}
		for index := uint32(0); index < count; index++ {
			cookie, err := list.GetItem(index)
			if err != nil {
				list.Release()
				return false, err
			}
			name, nameErr := cookie.GetName()
			if nameErr != nil {
				cookie.Release()
				list.Release()
				return false, nameErr
			}
			if strings.EqualFold(name, "remixsid") {
				value, valueErr := cookie.GetValue()
				cookie.Release()
				if valueErr != nil {
					list.Release()
					return false, valueErr
				}
				if len(strings.TrimSpace(value)) >= 8 {
					list.Release()
					return true, nil
				}
				continue
			}
			cookie.Release()
		}
		list.Release()
	}
	return false, nil
}

func createVKAuthWindow(hidden bool) (uintptr, error) {
	className, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		return 0, err
	}
	title, err := syscall.UTF16PtrFromString("VK — PWDTT")
	if err != nil {
		return 0, err
	}
	style := uintptr(vkAuthWindowStyle)
	if hidden {
		style &^= 0x10000000
	}

	hwnd, _, callErr := vkCreateWindowExProc.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		style,
		200,
		100,
		720,
		780,
		0,
		0,
		0,
		0,
	)
	if hwnd == 0 {
		if callErr != syscall.Errno(0) {
			return 0, fmt.Errorf("не удалось открыть окно авторизации VK: %w", callErr)
		}
		return 0, errors.New("не удалось открыть окно авторизации VK")
	}
	return hwnd, nil
}

func pumpVKWindowMessages() {
	var message vkWinMessage
	for {
		hasMessage, _, _ := vkPeekMessageProc.Call(
			uintptr(unsafe.Pointer(&message)),
			0,
			0,
			0,
			vkPMRemove,
		)
		if hasMessage == 0 {
			return
		}
		vkTranslateMsgProc.Call(uintptr(unsafe.Pointer(&message)))
		vkDispatchMsgProc.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func vkWebViewDataPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "PWDTT", "vk-webview2")
}

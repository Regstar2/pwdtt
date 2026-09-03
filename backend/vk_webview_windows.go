//go:build windows

package backend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	chromium.SetErrorCallback(func(webViewErr error) {
		finish(vkWebViewResult{err: fmt.Errorf("WebView2 VK: %w", webViewErr)})
	})

	messageCh := make(chan string, 8)
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
	if clearCookies {
		manager, managerErr := chromium.GetCookieManager()
		if managerErr != nil {
			finish(vkWebViewResult{err: fmt.Errorf("не удалось открыть хранилище cookies VK: %w", managerErr)})
			return
		}
		defer manager.Release()
		if deleteErr := manager.DeleteAllCookies(); deleteErr != nil {
			finish(vkWebViewResult{err: fmt.Errorf("не удалось удалить cookies VK: %w", deleteErr)})
			return
		}
		finish(vkWebViewResult{})
		return
	}

	chromium.Init(`(function(){
		function pwdttSendLocation(){
			try { window.chrome.webview.postMessage(String(window.location.href)); } catch (_) {}
		}
		pwdttSendLocation();
		window.addEventListener('load', pwdttSendLocation);
		window.addEventListener('hashchange', pwdttSendLocation);
		window.addEventListener('popstate', pwdttSendLocation);
		setInterval(pwdttSendLocation, 400);
	})();`)
	chromium.Navigate(buildLegacyVKAuthorizeURL())
	vkShowWindowProc.Call(hwnd, vkSWShow)

	for {
		select {
		case <-ctx.Done():
			vkPostMessageProc.Call(hwnd, vkWMClose, 0, 0)
			finish(vkWebViewResult{err: context.Canceled})
			return
		case message := <-messageCh:
			token, terminal, parseErr := parseLegacyVKTokenURL(message)
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

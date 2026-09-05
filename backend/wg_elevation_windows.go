//go:build windows

package backend

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	windowsWGHelperArg           = "--pwdtt-wg-helper"
	windowsWGHelperAcceptTimeout = 60 * time.Second
	windowsWGHelperIOTimeout     = 90 * time.Second
)

var (
	windowsWGHelperMode  bool
	windowsWGHelperState struct {
		sync.Mutex
		client *windowsWGHelperClient
	}
)

type windowsWGHelperMessage struct {
	Kind     string   `json:"kind"`
	Token    string   `json:"token,omitempty"`
	Action   string   `json:"action,omitempty"`
	Config   string   `json:"config,omitempty"`
	TurnIPs  []string `json:"turn_ips,omitempty"`
	OK       bool     `json:"ok,omitempty"`
	Error    string   `json:"error,omitempty"`
	Message  string   `json:"message,omitempty"`
	Elevated bool     `json:"elevated,omitempty"`
}

type windowsWGHelperClient struct {
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder
	token   string
	mu      sync.Mutex
}

func RunWindowsWGHelperIfRequested(wintunDLL []byte) bool {
	if len(os.Args) < 4 || os.Args[1] != windowsWGHelperArg {
		return false
	}

	windowsWGHelperMode = true
	InitWintun(wintunDLL)
	runWindowsWGHelper(os.Args[2], os.Args[3])
	return true
}

func runWindowsWGHelper(address, token string) {
	conn, err := net.DialTimeout("tcp4", address, 15*time.Second)
	if err != nil {
		return
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	var sendMu sync.Mutex
	send := func(message windowsWGHelperMessage) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return encoder.Encode(message)
	}

	elevated := isCurrentProcessElevated()
	if err := send(windowsWGHelperMessage{
		Kind:     "hello",
		Token:    token,
		Elevated: elevated,
	}); err != nil || !elevated {
		return
	}

	var helperWG WG
	logf := func(message string) {
		_ = send(windowsWGHelperMessage{Kind: "log", Message: message})
	}
	defer helperWG.teardownWindowsLocalWithLog(logf)

	for {
		var request windowsWGHelperMessage
		if err := decoder.Decode(&request); err != nil {
			return
		}
		if request.Token != token {
			_ = send(windowsWGHelperMessage{
				Kind:  "result",
				OK:    false,
				Error: "отклонена команда с неверным токеном privileged helper",
			})
			return
		}

		switch request.Action {
		case "apply":
			err := helperWG.applyWindowsLocal(request.Config, request.TurnIPs, logf)
			if err != nil {
				_ = send(windowsWGHelperMessage{Kind: "result", OK: false, Error: err.Error()})
				continue
			}
			_ = send(windowsWGHelperMessage{Kind: "result", OK: true})
		case "teardown":
			helperWG.teardownWindowsLocalWithLog(logf)
			_ = send(windowsWGHelperMessage{Kind: "result", OK: true})
			return
		default:
			_ = send(windowsWGHelperMessage{
				Kind:  "result",
				OK:    false,
				Error: "неизвестная команда privileged helper",
			})
		}
	}
}

func useWindowsPrivilegedHelper() bool {
	return !windowsWGHelperMode && !isCurrentProcessElevated()
}

func isCurrentProcessElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func applyWindowsViaPrivilegedHelper(conf string, turnIPs []string, logf wgLogFunc) error {
	client, err := getOrStartWindowsWGHelper()
	if err != nil {
		return err
	}
	return client.request(windowsWGHelperMessage{
		Action:  "apply",
		Config:  conf,
		TurnIPs: turnIPs,
	}, logf)
}

func teardownWindowsPrivilegedHelper(logf wgLogFunc) {
	windowsWGHelperState.Lock()
	client := windowsWGHelperState.client
	windowsWGHelperState.client = nil
	windowsWGHelperState.Unlock()
	if client == nil {
		return
	}

	if err := client.request(windowsWGHelperMessage{Action: "teardown"}, logf); err != nil && logf != nil {
		logf(fmt.Sprintf("Privileged helper teardown завершился с ошибкой: %v", err))
	}
	_ = client.conn.Close()
}

func getOrStartWindowsWGHelper() (*windowsWGHelperClient, error) {
	windowsWGHelperState.Lock()
	defer windowsWGHelperState.Unlock()

	if windowsWGHelperState.client != nil {
		return windowsWGHelperState.client, nil
	}

	client, err := startWindowsWGHelper()
	if err != nil {
		return nil, err
	}
	windowsWGHelperState.client = client
	return client, nil
}

func startWindowsWGHelper() (*windowsWGHelperClient, error) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть локальный канал privileged helper: %w", err)
	}
	defer listener.Close()

	token, err := newWindowsWGHelperToken()
	if err != nil {
		return nil, fmt.Errorf("не удалось создать токен privileged helper: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("не удалось определить путь PWDTT для UAC: %w", err)
	}

	args := strings.Join([]string{
		syscall.EscapeArg(windowsWGHelperArg),
		syscall.EscapeArg(listener.Addr().String()),
		syscall.EscapeArg(token),
	}, " ")

	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(executable)
	argsPtr, _ := syscall.UTF16PtrFromString(args)
	cwdPtr, _ := syscall.UTF16PtrFromString(filepath.Dir(executable))

	if err := windows.ShellExecute(0, verbPtr, exePtr, argsPtr, cwdPtr, 1); err != nil {
		return nil, friendlyWindowsElevationError(err)
	}

	if err := listener.SetDeadline(time.Now().Add(windowsWGHelperAcceptTimeout)); err != nil {
		return nil, fmt.Errorf("не удалось задать timeout privileged helper: %w", err)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			return nil, fmt.Errorf("privileged helper не подключился после подтверждения UAC: %w", err)
		}

		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		decoder := json.NewDecoder(conn)
		var hello windowsWGHelperMessage
		if err := decoder.Decode(&hello); err != nil ||
			hello.Kind != "hello" ||
			hello.Token != token {
			_ = conn.Close()
			continue
		}
		if !hello.Elevated {
			_ = conn.Close()
			return nil, fmt.Errorf("Windows не предоставила privileged helper права администратора; подключение не начато")
		}

		_ = conn.SetDeadline(time.Time{})
		return &windowsWGHelperClient{
			conn:    conn,
			encoder: json.NewEncoder(conn),
			decoder: decoder,
			token:   token,
		}, nil
	}
}

func (c *windowsWGHelperClient) request(request windowsWGHelperMessage, logf wgLogFunc) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	request.Kind = "request"
	request.Token = c.token
	if err := c.conn.SetDeadline(time.Now().Add(windowsWGHelperIOTimeout)); err != nil {
		return err
	}
	defer c.conn.SetDeadline(time.Time{})

	if err := c.encoder.Encode(request); err != nil {
		return fmt.Errorf("не удалось отправить команду privileged helper: %w", err)
	}

	for {
		var response windowsWGHelperMessage
		if err := c.decoder.Decode(&response); err != nil {
			return fmt.Errorf("privileged helper оборвал соединение: %w", err)
		}

		switch response.Kind {
		case "log":
			if logf != nil && response.Message != "" {
				logf(response.Message)
			}
		case "result":
			if response.OK {
				return nil
			}
			if response.Error == "" {
				response.Error = "privileged helper завершил операцию с неизвестной ошибкой"
			}
			return fmt.Errorf("%s", response.Error)
		}
	}
}

func newWindowsWGHelperToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func friendlyWindowsElevationError(err error) error {
	if errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(1223)) {
		return fmt.Errorf("права администратора не предоставлены: запрос UAC отменён; подключение не начато")
	}
	return fmt.Errorf("не удалось запросить права администратора через UAC: %w", err)
}

func cleanupStaleWindowsState(logf wgLogFunc) {
	if useWindowsPrivilegedHelper() {
		if logf != nil {
			logf("Очистка stale Windows-сетевого состояния будет выполнена privileged helper при следующем подключении")
		}
		return
	}
	cleanupStaleIPv6LeakProtection(logf)
}

func platformPrivilegeReport() string {
	if windowsWGHelperMode {
		return "Windows privileges: elevated network helper"
	}
	if isCurrentProcessElevated() {
		return "Windows privileges: GUI process elevated by user"
	}
	return "Windows privileges: GUI asInvoker; network setup uses on-demand UAC helper"
}

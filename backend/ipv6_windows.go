//go:build windows

package backend

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
)

const (
	ipv6LeakProtectionRuleName        = "PWDTT-IPv6-Leak-Protection"
	ipv6LeakProtectionRuleDisplayName = "PWDTT IPv6 leak protection"
)

var (
	ipv6LeakProtectionMu     sync.Mutex
	ipv6LeakProtectionActive bool
)

func enableIPv6LeakProtection(logf wgLogFunc) error {
	ipv6LeakProtectionMu.Lock()
	defer ipv6LeakProtectionMu.Unlock()

	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}

	if _, err := removeIPv6LeakProtectionRule(); err != nil {
		return fmt.Errorf("remove stale firewall rule: %w", err)
	}
	ipv6LeakProtectionActive = false

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
New-NetFirewallRule -Name '%s' -DisplayName '%s' -Description 'Temporary outbound IPv6 block used by PWDTT while an IPv4-only full tunnel is active.' -Direction Outbound -Action Block -Enabled True -Profile Any -RemoteAddress 'Internet6' -ErrorAction Stop | Out-Null
`, ipv6LeakProtectionRuleName, ipv6LeakProtectionRuleDisplayName)

	if _, err := runPowerShellWindows(script); err != nil {
		_, _ = removeIPv6LeakProtectionRule()
		return err
	}

	ipv6LeakProtectionActive = true
	logf("IPv6 leak protection включена: прямой IPv6-доступ в интернет временно заблокирован")
	return nil
}

func restoreIPv6LeakProtection(logf wgLogFunc) error {
	ipv6LeakProtectionMu.Lock()
	defer ipv6LeakProtectionMu.Unlock()

	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}

	removed, err := removeIPv6LeakProtectionRule()
	if err != nil {
		return err
	}
	ipv6LeakProtectionActive = false
	if removed {
		logf("IPv6 leak protection отключена, исходная IPv6-связность восстановлена")
	}
	return nil
}

func cleanupStaleIPv6LeakProtection(logf wgLogFunc) {
	ipv6LeakProtectionMu.Lock()
	defer ipv6LeakProtectionMu.Unlock()

	if ipv6LeakProtectionActive {
		return
	}

	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}

	removed, err := removeIPv6LeakProtectionRule()
	if err != nil {
		logf(fmt.Sprintf("Не удалось убрать stale IPv6 leak protection: %v", err))
		return
	}
	if removed {
		logf("Убрана IPv6 leak protection, оставшаяся после предыдущего запуска")
	}
}

func removeIPv6LeakProtectionRule() (bool, error) {
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$rule = Get-NetFirewallRule -Name '%s' -ErrorAction SilentlyContinue
if ($null -ne $rule) {
    $rule | Remove-NetFirewallRule -Confirm:$false -ErrorAction Stop
    Write-Output 'removed'
} else {
    Write-Output 'absent'
}
`, ipv6LeakProtectionRuleName)

	out, err := runPowerShellWindows(script)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "removed", nil
}

func runPowerShellWindows(script string) (string, error) {
	wrappedScript := "[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new(); $OutputEncoding = [Console]::OutputEncoding; " + script
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", wrappedScript)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

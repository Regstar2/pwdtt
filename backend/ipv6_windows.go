//go:build windows

package backend

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

const (
	ipv6LeakProtectionRuleName        = "PWDTT-IPv6-Leak-Protection"
	ipv6LeakProtectionRuleDisplayName = "PWDTT IPv6 leak protection"
)

func enableIPv6LeakProtection(logf wgLogFunc) error {
	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}

	if _, err := removeIPv6LeakProtectionRule(); err != nil {
		return fmt.Errorf("remove stale firewall rule: %w", err)
	}

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$aliases = @(Get-NetAdapter -Name '*' -Physical -ErrorAction Stop | Select-Object -ExpandProperty Name)
if ($aliases.Count -eq 0) { throw 'No physical network adapters found' }
New-NetFirewallRule -Name '%s' -DisplayName '%s' -Description 'Temporary outbound IPv6 block used by PWDTT while an IPv4-only full tunnel is active.' -Direction Outbound -Action Block -Enabled True -Profile Any -RemoteAddress '::/0' -InterfaceAlias $aliases -ErrorAction Stop | Out-Null
`, ipv6LeakProtectionRuleName, ipv6LeakProtectionRuleDisplayName)

	if _, err := runPowerShellWindows(script); err != nil {
		_, _ = removeIPv6LeakProtectionRule()
		return err
	}

	logf("IPv6 leak protection включена для физических сетевых интерфейсов")
	return nil
}

func restoreIPv6LeakProtection(logf wgLogFunc) error {
	if logf == nil {
		logf = func(msg string) { log.Printf("[WG] %s", msg) }
	}

	removed, err := removeIPv6LeakProtectionRule()
	if err != nil {
		return err
	}
	if removed {
		logf("IPv6 leak protection отключена, исходная IPv6-связность восстановлена")
	}
	return nil
}

func cleanupStaleIPv6LeakProtection(logf wgLogFunc) {
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
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

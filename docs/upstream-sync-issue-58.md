# Upstream selective sync (#58)

Integration branch: `sync/upstream-pwdtt`, fork `Regstar2/pwdtt`.
Compare point: fork main `46c7c4a`; upstream `luminescq/PWDTT` at `42f089b`.
Common ancestor: `055dce9`. Both branches diverged; upstream/main is NOT merged.

| Upstream reference | Status in this branch | Notes |
| --- | --- | --- |
| `core/session.go`, TURN UDP/TCP | Ported selectively | TCP reaches TURN through Pion STUN framing; DTLS stays datagram based. Explicit per-profile opt-in, UDP default |
| `core/session.go`, CreatePermission | Ported selectively | After Allocate, before WRAP/DTLS; distinct permission/auth errors; don't blacklist allocated relay on peer failure |
| `66b4a8b`, `core/allocation_gate.go` | Ported selectively | Per-Core pacing; context cancellation |
| `66b4a8b`, `core/worker_group.go` | Ported selectively | Relay rotation, config racers <=3, bounded credential refresh; retained fork worker-local WRAP mode |
| `96063fb`, `core/vk_auth.go` | Ported selectively | One retry only for typed step-5 outdated token; retain fork CAPTCHA, rate-limit and safe diagnostics |
| `ba66308`, `703189d`, `5e7894d`: profile workers | Ported selectively | qwdtt count validation and Go/Wails persistence; legacy fields/defaults |
| `636a5d6`, `main_windows.go`: tray | Deferred (P2) | Blocked by separate GUI launch issue #61; no tray changes in this integration |
| Upstream update, UAC, VK auth, whole-branch merge | Rejected | Keep fork-specific release channel, Windows UAC, CAPTCHA, logging and IPv6 protections |
| Domain name parsing | Out of scope | Tracked by #53; no duplicate resolver |

### Verification policy

Do **not** start GitHub Actions for this integration (Issue #58).
Tests are provided but must be executed on a local compatible workstation,
with Go 1.26, frontend Node/npm and Wails tooling. No real VK/VPS
end-to-end test or Windows GUI smoke is claimed.

* Go: `go test -count=1 ./...` in root and in `core`.
* Frontend: `npm ci`, `npm test`, `npm run lint`, `npm run build`.
* Windows: use project's existing Wails build tooling and verify the EXE opens.
* Local TURN: `TestIssue58TURNUDPAndTCPAllocatePermissionRelay` covers
  allocation, permission and bidirectional datagrams over both transports.
* VK: pure fixture tests cover expiry retry boundedness and nonretryable errors.
* Profiles: Go store round-trip plus TypeScript localStorage / qwdtt checks.

This work does not imply completion of issues #51, #53, #54, #55 or #61.

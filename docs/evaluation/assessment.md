<p align="center"><img src="../assets/brand/logo-mark-256.png" alt="Vutty" width="88"></p>

# Assessment of v1.0.0 against the design

An honest comparison of what was built with [Architecture.md](../design/architecture.md), including gaps and risks. "Verified" means covered by an automated test or the E2E walkthrough; "not verified" means built but not exercised in this environment.

## 1. Requirement coverage

| Requirement (Architecture.md) | Status | Evidence / note |
| --- | --- | --- |
| Hosts by group, tag, environment colour (§1, §4) | Done | store + domain tests, `HostList`/`HostForm` tests, E2E |
| Vault with master password, envelope encryption, Argon2id (m=64 MiB, t=3, p=4), AES-256-GCM, secret id as AAD (§5) | Done | `vault_test.go` (swap attack, tamper, wrong password, hostile KDF params) |
| Change master password re-wraps DEK only (§5) | Done | `TestChangePasswordKeepsSecrets` |
| Keychain quick unlock (§5) | Done (app-level gate; guarded item when signed) | The key sits in the OS keychain. On a Mac the vault first tries an item with a `.biometryCurrentSet` access control in the data protection keychain (`internal/vault/keychain_darwin.go`, `TieredKeychain`): macOS then asks for the fingerprint itself on every read. That needs the `keychain-access-groups` entitlement, which an ad hoc signed build does not have (OSStatus -34018, verified on this machine), so it falls back to the older plain item (`go-keyring`) behind an `LAContext` Touch ID check (`touchid_darwin.go`, wired in `app/app.go`). An older plain item is moved into the guarded one at the next unlock (see gap G1) |
| Auto-lock after N minutes (§5) | Done | timer tests, UI returns to unlock screen |
| Auto-lock when the machine sleeps or the screen locks (§5) | Done | `app/syswatch_darwin.go` listens for `NSWorkspaceWillSleepNotification` and `com.apple.screenIsLocked`; a test posts the notification |
| Strict `known_hosts`, TOFU prompt with SHA-256 fingerprint, no override on change (§5) | Done | `sshx` + session tests + E2E; `InsecureIgnoreHostKey` is never used |
| Frontend never receives secrets; strict CSP (§5) | Done | identity metadata only; CSP in `index.html`; tests grep for secret material |
| Multi-tab terminal, split panes (§1, §6) | Done | component tests, E2E |
| Jump host chains, depth limit (§6) | Done | store + sshx tests, E2E through a bastion |
| Output batched ~16 ms (§6, §8) | Done | `Batcher` tests, `TestOutputIsBatchedIntoFewEvents` |
| Keepalive, manual reconnect, auto_start forwards rebuilt (§6) | Done | keepalive + reconnect tests |
| Port forwarding L/R/D, loopback by default (§6) | Done | forward tests, real TCP check in E2E |
| Import `~/.ssh/config` incl. `Include`, group from file name, ProxyJump, forwards (§7) | Done | importer tests; custom parser (ADR-0003) |
| Export ssh config per group, keys opt-in with warning and 0600 (§7) | Done | exporter tests |
| Plain JSON export, encrypted JSON backup with its own password (§7) | Done | exporter + service tests |
| Git sync of `bundle.enc` + `vault-header.json`, per-record merge, retry on push race (§7) | Done (local remotes) | tests use real `git` against bare repos; **not verified against GitHub/GitLab** |
| Storage | **Changed** | SQLite instead of JSON files (ADR-0001, requested) |
| Snippets (§1 "out of scope") | **Added** | requested; sync, backup and export included |
| Settings: theme, terminal themes, font, cursor, scrollback | **Added** | a popup of the main window, like VS Code's (a window of its own from 1.1.0 until it became a popup, see G13) |
| Test connection and Connect-with-save in the host form | **Added** | `SessionService.TestConnection`: jump chain, inline host key trust, nothing stored |
| Lost master password | **Added** | reset vault (keeps hosts, erases secrets); cannot be recovered by design |
| Project structure and binding API (§8) | Done, adapted | services live in `internal/usecase`, Wails glue in `app/` (ADR-0002). Extra methods added: `Attach`, `Reconnect`, `EnableBiometric`, forward CRUD, settings |
| CLI `vvt` (§9 open question 1 mentions the name) | Not built | no CLI in scope of the listed requirements |
| `settings.json` keyboard shortcuts (§4) | **Done** | configurable in Settings → Shortcuts; overrides stored in `settings.json`, validated by `ValidateShortcuts`, conflicts refused |
| SFTP (not in the original scope) | **Added** | `SftpService` over a dedicated SSH connection (jump chain supported), `sftpx` adapter, atomic transfers that resume, no silent overwrite, a background queue, folder sync and edit-and-save-back |
| Settings: General, Account, zoom, maximise (not in the original scope) | **Added** | interface zoom, reduce motion, confirm close, local profile; sign-in is a disabled placeholder |
| Decide Svelte vs React (§9) | Decided: React + TypeScript | |
| Platforms (§1) | macOS only built | Windows/Linux not built or tested, as the design allows |

## 2. Gaps and risks

| ID | Gap | Impact | Suggested next step |
| --- | --- | --- | --- |
| G1 | **Partly closed.** The code stores the key with `SecAccessControl` `.biometryCurrentSet` (cgo bridge, `TieredKeychain`), shows the Touch ID sheet once (the item's own), never reads the key for a status check, moves old plain items at the next unlock and falls back to the plain item plus the app's `LAContext` check when macOS refuses. macOS refuses without the `keychain-access-groups` entitlement (-34018): a test binary and the ad hoc signed builds we publish both get that, so **for those builds the gap is still open**. Not exercised on a real Touch ID; only the refusal path ran for real. | For an ad hoc build, a process that reads the login keychain without going through Vutty is not stopped by Touch ID. | Sign with a Developer ID, add `keychain-access-groups` (`<TeamID>.<bundle id>`) to the entitlements and embed a provisioning profile that grants it (a restricted entitlement without a profile makes macOS kill the app at launch), then run the Touch ID part of [TestPlan.md](../development/test-plan.md): enable, relaunch, unlock, add a fingerprint and see the item refused. |
| G2 | ~~No lock on system sleep.~~ **Closed:** the vault locks on sleep and on screen lock. | | |
| G3 | Not launched as a native window in this environment. | The Wails build succeeds (`vutty.app`), but WKWebView rendering, native dialogs and keychain prompts were not exercised; E2E runs in Chromium against the real backend. | Run the manual checklist in [TestPlan.md](../development/test-plan.md). |
| G4 | ~~xterm.js heavy-output performance on WKWebView.~~ **Addressed:** the pane that has the focus is drawn with the WebGL renderer (Settings → Terminal → Fast drawing, on by default); other panes, and a pane where WebGL cannot be created or loses its context, keep the DOM renderer. In the real WKWebView a full-screen redraw with every cell coloured went from a lag of 66 ms (p99) to 5 ms at 60 fps (4 MB, paced). | Not checked by eye in the packaged app: the look of Vietnamese, CJK and box characters, the selection, and zoom. | Look at those on a Mac; turn the switch off if text looks wrong. |
| G5 | Sync merge window: edits made between the snapshot and the write-back of a sync run can be overwritten. | Rare data loss for concurrent edits during a sync (seconds). | Re-apply local changes after sync or lock writes during sync. |
| G6 | Last-write-wins relies on machine clocks (accepted in §7). | Wrong winner when clocks differ by more than the edit gap. | Version vectors (design already notes this). |
| G7 | Clean-architecture debt: the use cases still import `sshx`, `forward`, `importer`, `exporter`, `syncgit`, `vault` helpers directly. | Those cannot be swapped without touching use cases. | Introduce the ports listed in [CleanArchitecture.md](../design/clean-architecture.md); the allow-list test prevents growth. |
| G8 | SQLite saves rewrite all rows in one transaction. | Fine for hundreds of hosts; wasteful for very large inventories. | Per-record upserts behind the same persister. |
| G9 | `ProxyCommand`, `Match`, token expansion in `ssh_config` are unsupported. | Such hosts import with a warning or incompletely. | Extend the parser. |
| G10 | Legacy keys (RSA/SHA-1, DSA) are limited by `x/crypto/ssh`; there is no dedicated "unsupported key" message. | Old servers may fail with a generic handshake error. | Detect and explain. |
| G11 | ~~Servers that prompt for 2FA are not supported.~~ **Closed:** `sshx.InteractiveAuth` answers a password question with the saved password (once) and asks the user for any other question (a code). A connection that cannot ask fails with the question in the message. | | |
| G12 | The domain package's own coverage is 43 % because the merge tests still live in `syncgit` (they pass through type aliases). | Reporting artefact, not a behaviour gap. | Move those tests into `domain`. |
| G13 | ~~Settings is a second native window implemented as a second process.~~ **Closed for Settings:** it is a popup of the main window and calls the use cases directly; the `--settings` process, its launcher, the Dock-less helper bundle and the `/call` allow-list are gone. Only the app windows (Duplicate in new window) still use a second process and the bridge (`/appcall`). | Settings can no longer be dragged out of the main window or shown on a second screen; the bridge is still a local attack surface for the app windows (loopback, token). | Wails v3 multi-window would remove the app windows' process too. |
| G14 | ~~A host cannot pick its own terminal theme.~~ **Closed:** `Host.TerminalTheme` (`terminal_theme`) is set in the host form; an empty value follows Settings. | | |
| G15 | Replacing a host's saved password creates a new identity; the old one stays in Keychain. | Orphan identities until deleted by hand. | Offer to update the existing identity's secret. |
| G17 | Remote change detection while editing uses size and time to the second. (Closed: a copy now keeps the modification time of its source, and a session removes partial copies `.vutty-part-…` older than 7 days from the folder of a transfer, once per folder.) | A conflict made within one second of the same size goes unnoticed. | Compare a checksum when the server offers one. |
| G18 | Account is a local profile; Sign in is a placeholder. | No cloud features yet (by design). | Define the account service before enabling it. |
| G16 | Sync after a vault reset: the Git repository still holds the old vault. | `Sync now` fails with a different-key error. | Use a new repository (documented). |

## 3. Quality evidence

| Check | Result |
| --- | --- |
| Go tests (`go test -race ./...`) | 296 passing (test functions and sub-tests), 0 failing |
| Go statement coverage | 80.6 % overall (the figures per package below are from the first measurement); importer 92 %, exporter 89 %, usecase 88 %, store 85 %, sftpx 82 %, sshx 82 %, forward 81 %, syncgit 78 %, sqlitedb 76 %, vault 75 %, app 73 %, domain 44 % (see G12) |
| Frontend (Vitest) | 575 passing in 35 files, strict TypeScript typecheck clean |
| E2E | 1 walkthrough, 20 steps, 60 screenshots, uses the real Go backend, SQLite, vault, two SSH servers, a real TCP check through a tunnel, real SFTP transfers verified on disk, a window drag, maximise/roll-up, a changed shortcut that really triggers its action, and a narrow-window overflow guard |
| Architecture rule | enforced by 5 tests, and verified to fail on a deliberately introduced violation |
| `go vet` | clean |

## 4. Defects found by testing during development (all fixed, each with a regression test)

1. **Config injection**: a hostname/username containing a newline could add directives (e.g. `ProxyCommand`) to an exported ssh config. Now rejected by the domain rule and skipped by the exporter.
2. **Sync creating a commit on every run**: random GCM nonces made identical content look changed. Content (plaintext + header) is now compared first.
3. **Lost first output / host key prompt**: events fired before the UI subscribed. Fixed with `Attach` and buffered replay (ADR-0004).
4. **White screen on import**: Go returned `null` for empty slices and the UI crashed on `.length`. Found by the E2E walkthrough; fixed in the backend and defensively in the UI.
5. **Hostile KDF parameters** from a sync repo or backup could panic Argon2 or exhaust memory. Parameters are now validated before use.
6. **Remote forward test server** sent origin port 0, which the SSH client rejects; fixed in the test server (not product code).

### Found while redesigning (all fixed, each with a regression test)

7. **Layout overflow** at a 960 px window: the panel's slide-in animation widened the page and produced a horizontal scrollbar. Found by the narrow-window E2E guard.
8. **Icon-only navigation had no accessible names** at narrow widths. Found by the same step.
9. **Wordmark**: tight letter-spacing made "VV" read as "W" (spotted on a screenshot).
10. **Vault reset** had to handle identities still referenced by hosts; covered by use-case tests (including that no old ciphertext survives on disk).
11. **Settings validation**: a font-family check escaped `\n` wrongly and rejected "Menlo"; found by the settings tests.

## 5. Verdict

Feature complete for the MVP scope in the design, with the security properties that can be tested automatically verified. Before calling it ready for daily use as the main SSH tool (Gate 4), close G1–G3 and run the manual checklist on a real Mac; G4 and G5 are the next most valuable.

## 5. Added since this evaluation was first written

| Area | Status | Notes |
| --- | --- | --- |
| Terminal on this computer, Serial, Telnet (own login), Known Hosts, Logs | Done | `localtty`, `serialx`, `telnetx` adapters behind `usecase` ports; the Telnet password is a vault secret; Telnet is unencrypted (documented) |
| Choose protocol, connecting screen, tab badges, New Tab, sidebar, tab drag (order, split, pop out), tag filter, drag host to group, SFTP right-click and permissions | Done | component tests, E2E screenshots; no session is rebuilt when a pane moves (flat pane list) |
| Duplicate in new window | Done, **not exercised on a real second window** | an app window is a second process that calls the main process through the loopback bridge (`/appcall`, `/events`); covered by bridge and frontend tests only |
| Mosh | Not built | shown on the protocol screen as unsupported |
| AWS / DigitalOcean / Azure integrations, multiplayer | Not built | out of scope (paid / cloud) |
| Native macOS pieces (menu bar of an app window, Dock icon) | Checked by hand only | no automated check can screenshot another app's window |

## Terminal performance with heavy output (G4)

Measured on 2026-11-03 with `tools/termperf/run.sh`: the app's own terminal set-up (xterm.js 5.5, DOM renderer, 10000 lines of scrollback, the output highlighter, the same base64 → bytes → `term.write` path as a `term:data` event) in a real WKWebView (macOS 26.7, Apple silicon, a window of about 120 × 40 characters). A native Swift app feeds it 24 MB per scenario through `evaluateJavaScript`. *Paced* = a chunk of up to 64 KB every 16 ms (the most the backend's batcher sends on its timer, about 3.7 MB/s); *flood* = the next chunk as soon as the last was delivered, with no flow control. *lag* is how late a 4 ms timer fires; *fps* counts `requestAnimationFrame` calls while the data runs.

| Scenario (DOM renderer, highlighter on) | Paced: 24 MB in | fps | lag p99 | Flood: parse speed | Flood lag p99 |
| --- | --- | --- | --- | --- | --- |
| Plain lines | 7.0 s (keeps up) | 60 | 11 ms | 18 MB/s | 15 ms |
| Log lines (dates, IPs, paths, ERROR) | 7.1 s | 58 | 13 ms | 5.5 MB/s | 12 ms |
| Colours on every word (`ls --color`) | 7.4 s | 55 | 32 ms | 5 MB/s | 38 ms |
| Vietnamese, CJK and box characters | 7.0 s | 59 | 24 ms | 4.6 MB/s | 26 ms |
| Full-screen redraw, every cell coloured (synthetic worst case) | 52 s (**does not keep up**) | 21 | 146 ms | 0.4 MB/s | 125 ms |

- **At the rate the backend sends, text of every kind keeps up** and the interface stays responsive (about 60 fps). The highlighter has no measurable cost (the runs with it on and off are the same within the noise).
- **A screen redrawn in place with every cell coloured** (40 rows, each cell with its own colour and reverse video; much heavier than a real htop) is the weak spot of the DOM renderer: about 0.5 MB/s, 20 fps, input delayed by up to 150 ms. With the WebGL renderer the same data ran at 60 fps with a lag of 6 ms, and in the flood runs it parsed colours and Vietnamese/CJK 3–5 times faster (16 and 24 MB/s), plain text 1.6 times faster (28 MB/s) and log lines no faster (5 MB/s: there the highlighter and the parser limit it, not the drawing).
- **Flow control.** The backend now stops reading a session while more than 512 KB of its output is sent but not yet parsed (the page reports what xterm has parsed; `sshx.Gate`). In the measurements this changed nothing visible: with 24 MB per scenario, and with 80 MB of full-screen redraw pushed without pause, the page never stalled and no data was lost, with or without it, because the queue stayed below xterm.js's limit (50 MB, above which it throws *write data discarded*). It bounds memory and keeps that limit out of reach. *An earlier version of these notes said that the page stopped drawing for up to a minute when output was pushed without pause. That was wrong: the benchmark window had been behind other windows, and WebKit then runs timers once a second and stops animation frames. The runs are now made with the window on top and flagged `invalid` when the page was hidden.*
- **Limits of the measurement:** the data came from a generator in the same process, not from an SSH session, so the network, the SSH cipher and the Wails event transport are not in it; one machine and one window size; the lag and fps figures are of the page's main thread, not of what the screen showed. The WebGL renderer is not part of the app (it was installed by the script to compare).
- **What was done about it:** the WebGL renderer (`@xterm/addon-webgl` 0.18) is used for the pane that has the focus (`frontend/src/lib/terminal/gpu.ts`; a browser allows only about 16 WebGL contexts per page, so panes without focus keep the DOM renderer). A check on 2026-11-04 with `MODES=paced tools/termperf/run.sh 4` (the harness, not the app): full-screen redraw DOM lag p99 66 ms and 55 fps, WebGL 5 ms and 60 fps; the other four scenarios were the same (about 3.4 MB/s, 56–61 fps). **Not yet checked by eye in the packaged app:** fonts, the selection and zoom.

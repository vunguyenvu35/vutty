# Selling Vutty on Gumroad

Everything needed to publish the product page. Copy the text blocks as they are.

## Product

| Field | Value |
| --- | --- |
| Name | Vutty: SSH client with an encrypted vault |
| URL slug | `vutty` |
| Tagline | Your hosts, keys and passwords in one encrypted vault you own. No account, no cloud, and no telemetry unless you opt in. |
| Category | Software / Developer tools |
| Tags | ssh, sftp, terminal, devops, developer-tools, kubernetes, open-source |
| Cover | `docs/assets/gumroad/gumroad-cover-1280x720.png` |
| Thumbnail | `docs/assets/gumroad/gumroad-thumbnail-600x600.png` |
| Extra images | `docs/assets/screenshots/` (for example `16-hosts-in-group.png`, `48-settings-terminal-theme.png`) |

## Pricing

Vutty is MIT licensed and free on GitHub, so the page sells convenience and support, not access.

- **Option A (recommended while builds are unsigned):** pay what you want, minimum $0, suggested $15.
- **Option B (after macOS notarization and a Windows signing certificate):** fixed price, $19 to $29, one time, includes 1.x updates.

Gumroad is the merchant of record and collects sales tax and VAT. Check the current fee on Gumroad's pricing page before setting the price (about 10% plus a fixed amount per sale at the time of writing).

## Description

```
Vutty is an SSH client for macOS, Windows and Linux that keeps your hosts, keys and passwords in an encrypted vault on your own computer (AES-256-GCM, key from your master password with Argon2id).

WHAT YOU GET
- Tabs and split panes, jump hosts, port forwards (local, remote, SOCKS5)
- SFTP with a resumable transfer queue, folder sync and edit-in-place
- Serial, Telnet, Mosh, FTP/FTPS, RDP and VNC
- Panel: manage a Linux server (processes, systemd, Docker, logs, firewall, cron) with nothing installed on it
- Kubernetes: pods, workloads, Helm, logs, a shell in a pod
- Run one command on many hosts; snippets with variables
- Optional AI assistant (Claude Code, Codex CLI, Gemini CLI) that only sees redacted terminal text and only suggests commands
- Sync between your computers through your own Git repository or cloud folder
- Import and export ~/.ssh/config; encrypted backups
- Interface in English, Tiếng Việt, 日本語 and 中文

PRIVATE BY DESIGN
No account, no server of ours, and no analytics unless you say yes to anonymous usage statistics (asked once, off until you answer; only a random id, the version and your system type are sent). A changed server key blocks the connection with no override.

PLATFORMS
macOS (Apple silicon and Intel), Windows 64-bit, Linux 64-bit.

OPEN SOURCE
Vutty is MIT licensed. Buying here supports development; the source stays free at github.com/vunguyenvu35/vutty.
```

## Files to deliver

Upload from the release page (or link to it if a file is too large). Name the files in Gumroad as below.

| Gumroad file name | Release asset |
| --- | --- |
| Vutty for macOS (Apple silicon) | `vutty-macos-arm64.dmg` |
| Vutty for macOS (Intel) | `vutty-macos-amd64.dmg` |
| Vutty for Windows | `vutty-windows-amd64.exe` |
| Vutty for Linux | `vutty-linux-amd64.tar.gz` |
| Checksums | `SHA256SUMS` |
| Licenses | `THIRD_PARTY_NOTICES.md` and `LICENSE` |
| Opening the app on macOS | `docs/getting-started/install-macos.md` |

## Receipt / "content" text shown after purchase

```
Thank you for buying Vutty.

1. Download the file for your system below.
2. macOS: open the DMG and drag vutty.app to Applications. If macOS says the app "can't be opened", go to System Settings -> Privacy & Security -> Open Anyway.
   Windows: run the .exe; if SmartScreen warns, choose More info -> Run anyway.
   Linux: tar xzf vutty-linux-amd64.tar.gz and run ./vutty (needs GTK 3 and WebKitGTK 4.0).
3. Create your vault with a master password of 12+ characters. It cannot be recovered.

Verify a download with SHA256SUMS. Updates: Help -> About -> Check for updates.
Support: <your support email>
```

## Refund policy

```
14 days, no questions asked. Reply to your receipt email.
```

## Before publishing

- [ ] Replace `<your support email>` with a dedicated address (the one in `wails.json` is personal).
- [ ] Decide A or B above. Do not charge a fixed price for unsigned builds.
- [ ] Re-check the Gumroad fee and the account's payout and tax settings.
- [ ] Search USPTO and EUIPO for the name "Vutty" (no clash found in npm, PyPI, crates, Homebrew, GitHub or the web on 2026-11-02, trademark registers not checked).
- [ ] Download the latest release assets and confirm they open on each system.

# Vutty — security review

> Reviewed: 2026-10-29 · Scope: the whole repository at `main` after the AI assistant, with a closer look at the assistant, the local bridge, the MCP gateway and the supply chain. Method: automated scans (`govulncheck`, `npm audit`, secret patterns), reading the trust boundaries, and attacking the assistant's own rules against the real CLIs installed on the test machine.

## Summary

No secret, key or credential was found in the repository. The vault, host-key checking and the file permissions of the data folder are sound. The review found **eight defects, all in or around the AI assistant, all fixed in this change**, and a list of **residual risks** that cannot be removed in code and are stated plainly below. Two of the defects were weaknesses in the assistant's own safety rules that only showed when those rules were attacked, which is why the tests now include the attacks.

## Findings

| # | Severity | Finding | Status |
|---|---|---|---|
| 1 | High | **"Read-only" commands were not read-only.** The allow-list accepted `git branch -D <name>` (deletes a branch), `git log --output=<file>` (writes a file), `git diff --ext-diff` (runs a program from the configuration) and `journalctl --sync` / `--update-catalog`. With *Run read-only commands by itself* on, the assistant could have run them without asking. | Fixed: git and journalctl accept only a fixed set of reading flags; tests attack each of the above. |
| 2 | High | **The Codex provider could still reach the web.** With its shell tool disabled it kept a built-in `apps` connector (`search_service.web_run`) that searched the web, so text from the terminal could leave in a query. | Fixed: started with `--disable apps` and `web_search="disabled"`; verified live that the web probe now answers *NO TOOLS*. Detection refuses a Codex version that cannot turn these off. |
| 3 | High | **Antigravity's web search cannot be turned off.** Its `search_web` tool ran under every deny rule tried, including `*`. The self-test only asked for a command, so it would have passed. | Fixed by making the self-test probe the web too; Antigravity now fails it and cannot be enabled (verified live). The adapter stays for the day a version honours a rule. |
| 4 | Medium | **The MCP gateway token was on the command line** of the Claude process. Any process of the computer can read another's arguments, and the token lets its holder call the gateway (read the screen, run read-only commands) while a question runs. | Fixed: the config is a 0600 file in the agent's temporary folder; a test checks that no argument carries the token. |
| 5 | Medium | **Invisible characters in a suggested command.** Only control characters were refused, so a zero-width or right-to-left override character could make a command read differently from what it does once inserted. | Fixed: format characters (Unicode category Cf), U+2028 and U+2029 are refused with the other control characters. |
| 6 | Medium | **Go toolchain with 28 reachable standard-library vulnerabilities** (net/http, crypto/tls, crypto/x509…), all fixed in go1.26.10. | Fixed: `toolchain go1.26.10` in `go.mod`. |
| 7 | Low | **The local bridge checked only a token.** A request carrying an `Origin` (a web page) or another host name (DNS rebinding) was not refused before the token check. | Fixed: both are refused with 403 even with the right token; the gateway already did this. |
| 8 | Low | **CI**: no `permissions:` (default token rights) and it installed Wails 2.10.2 while `go.mod` uses 2.17.0. | Fixed: `contents: read`, Wails 2.17.0. (The workflow is paused; this applies when it is turned on.) |

## What was checked and found sound

- **Secrets in the repository:** none (private-key headers, cloud and token patterns, `deploy/env.*`, tracked database or vault files).
- **Vault:** Argon2id (64 MiB, 3 passes), AES-GCM, key in the OS keychain behind Touch ID; the data folder and the bridge connection file are 0600; deleted secrets stay as tombstones for sync.
- **SSH:** host keys are verified and trusted explicitly (`InsecureIgnoreHostKey` is never used); no weak randomness (`math/rand` is not used).
- **Bridge and gateway:** loopback only, 256-bit random tokens, constant-time comparison, the app windows' bridge serves every use case but only to the app's own processes (Origin and Host checks, token), and the AI assistant's gateway cannot ask, run, insert, preview or confirm on its own, session-scoped gateway with a cap on calls.
- **The assistant's boundaries:** the agent request type accepts only redacted text (`archtest` enforces it); redaction fails closed; the agent runs in an empty temporary folder with a cleaned environment; pictures need a confirmation and are the one thing that is not redacted.
- **Front end:** answers are rendered as text (no HTML injection), CSP `script-src 'self'`, no inline scripts.
- **Dependencies:** `npm audit --omit=dev` and the end-to-end project report none. `govulncheck` reports only the toolchain items above and one `golang.org/x/crypto` advisory (GO-2026-5932) that has no fix yet and is not called.

## Residual risks (stated, not fixable here)

1. **Redaction is best effort.** A password written in an ordinary sentence cannot be recognised. Hosts can be set to *Off* or *Ask first*, and *What will be sent* shows the text.
2. **Pictures are not redacted.** A confirmation says so each time.
3. **Prompt injection.** Terminal output is untrusted. The assistant has no tools of its own, cannot run anything that changes something without the user, and its read-only commands are on a strict allow-list, but a convincing suggestion can still be shown. The user reads every command.
4. **Reading commands can still read.** An allowed command such as `head /var/log/auth.log` shows its output in the terminal and, redacted, to the assistant. Paths that look like secrets are refused; a secret in an unusual place is not recognised.
5. **User-added MCP servers run with the user's rights.** The gateway checks arguments for secrets, redacts results and asks before anything that is not a plain read, but a server the user starts is code the user chose to run.
6. **The CLIs are not ours.** A new version of Claude Code or Codex can add a tool. The self-test (command and web) runs when a provider is enabled and when its version changes; `VUTTY_LIVE=1 go test ./internal/agentx -run Live` repeats it on demand.
7. **Development tools** (`vitest`, `vite`, `esbuild`: 6 advisories, 2 critical, dev-only) affect only a machine that runs `npm run dev` or the tests, through the development server. They do not ship.
8. **Telnet and serial are unencrypted** by nature; the host form says so.
9. **The CSP allows `connect-src` to local ports** (the development server needs it). A cross-site script is not possible today, so this is defence in depth only.

## Update 2026-11-05: the Kubernetes panel

> Scope: what was added after the review above: the kubeconfig client (`internal/kubeapi`), commands run with `kubectl` on a host, the changes (Edit YAML, Edit config, Add sidecar, Debug container), port-forward, and the files of a pod. Method: reading the code, plus a test for each defect. `govulncheck` (0 reachable), `npm audit --omit=dev` (0) and a secret scan were clean.

| # | Severity | Finding | Status |
|---|---|---|---|
| 9 | High (Windows), Medium (elsewhere) | **A login program ran without asking when its file name was a well-known one, whatever its folder.** A kubeconfig with `command: \\evil\share\aws.exe`, or a path to any file the person had downloaded and called `aws`, `kubectl` or `gcloud`, was run at once. | Fixed: a bare name is looked up as before; a path runs without asking only inside a folder an administrator writes (`/usr`, `/bin`, `/snap/bin`, `/opt/homebrew`, `Program Files`), never with `..`. Any other path asks. |
| 10 | Medium | **The kubeconfig could set any environment variable for the login program**, such as `LD_PRELOAD`, `DYLD_INSERT_LIBRARIES`, `PATH`, `AWS_CONFIG_FILE` or `CLOUDSDK_PYTHON`, and the variables were not part of what the person approved. | Fixed: only the names a provider's helper reads (`AWS_*`, `AAD_*`, `AZURE_*`, `ARM_*`, `GOOGLE_*`, `CLOUDSDK_CORE_*`, `KUBELOGIN_*`, none of them a path, a config file or a program) run without asking. The variables are shown in the approval, and are part of it: a file that adds one afterwards asks again. |
| 11 | Low | An approved login program given as a **relative path** (`./get-token`) meant a different file depending on the folder the app was started in. | Fixed: refused, with a message asking for the full path. |
| 12 | Low (Windows) | **The name of a file copied out of a pod** was chosen by the pod. `..\..\x` is one name to the pod and a way out of the folder on Windows; `NUL` or `C:x` are devices and drives. | Fixed: characters the systems do not take in a name are replaced, a trailing dot or space is dropped, device names get a prefix, and the length is limited. |

Checked and found sound: every name that reaches a `kubectl` command line is checked (it starts with a letter or a digit, so it cannot be a flag) and quoted by `shq`; the key of a Secret goes into a go-template only after a pattern that has no quote; the paths in a pod are arguments of the command (`"$1"`), never its text; the audit log never carries the text of an error or a value (a Secret key is named, not shown); redirects are not followed, so a token is sent only to the server of the file; the object of a change made on a host goes in on the standard input of `kubectl replace`, so a variable's value is not in the command line there; a local port-forward listens on `127.0.0.1` only.

Residual risks:

10. **A port-forward on a host listens on `127.0.0.1:18000-18999` of that host** (that is how `kubectl port-forward` works), so another login on the same machine can reach the service while it runs. On a shared host, use a cluster opened with a kubeconfig instead, or do not forward.
11. **A port-forward on this computer** can be reached by any process of this computer, as with `ssh -L`.
12. **Approving a login program is trusting the author of the kubeconfig.** The approval shows the whole command and its variables; it cannot say what the program does.

## Reporting a problem

Open a private security advisory on the repository rather than a public issue.

<p align="center"><img src="brand/logo-mark-256.png" alt="Vutty" width="88"></p>

# Development guide

For people who want to build, test or change Vutty. End users should read the [README](../README.md) and the [User guide](UserGuide.md).

## Prerequisites

- macOS, Go 1.26+, Node 22+
- Wails v2 CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2`
- Docker (optional: CI runs and demo servers)

## Everyday commands

```bash
make dev          # hot-reload development mode (wails dev)
make build        # build/bin/vutty.app
make test         # go vet + go test -race + frontend typecheck + vitest
./e2e/run.sh      # the walkthrough: also regenerates docs/screenshots and then runs scripts/check-doc-links.sh
make e2e          # end-to-end walkthrough on the real backend; refreshes docs/screenshots
./tools/termperf/run.sh   # heavy-output benchmark of the terminal in the real macOS WKWebView (about 10 minutes)
make inventory    # regenerate docs/TestInventory.md
make deploy-dev   # install a "Vutty Dev" build next to the production app
make deploy-prod  # release pipeline (see below)
```

The version comes from the `VERSION` file (single source of truth).

## Repository layout

| Path | Role |
| --- | --- |
| `internal/domain` | entities and pure business rules (validation, merge, jump-chain checks) |
| `internal/usecase` | application services and the ports they depend on (`ports.go`) |
| `internal/sqlitedb`, `store`, `vault`, `sshx`, `forward`, `importer`, `exporter`, `syncgit` | infrastructure adapters |
| `app/` | Wails delivery layer and composition root (`app.Build`) |
| `frontend/` | React + TypeScript + xterm.js UI |
| `app/bridge.go`, `app/terminal_window.go`, `app/window_process.go` | the app windows (`vutty --window`, Duplicate in new window): other processes and their bridge to the main process |
| `cmd/demoserver` | demo SSH server (used by Docker and the E2E walkthrough) |
| `e2e/` | Playwright walkthrough |
| `deploy/`, `scripts/` | deploy configuration and scripts |
| `docs/brand` | logo, wordmark, icons, social preview |

Dependencies point inward (`app → usecase → domain`, adapters implement the ports). `internal/archtest` fails the build if the rule is broken. Details and decisions: [CleanArchitecture.md](CleanArchitecture.md).

## Testing

| Level | Command | What it covers |
| --- | --- | --- |
| Go unit + integration | `go test -race ./...` | domain rules, SQLite, vault crypto, SSH sessions/jump hosts/host keys, forwards, git sync between two "machines", backup, snippets |
| Frontend | `cd frontend && npx vitest run` | every screen and panel against a fake backend |
| End to end | `make e2e` | the full journey in a browser against the real Go backend, SQLite and two SSH servers |
| Architecture | `go test ./internal/archtest` | the dependency rule |

The complete list of cases is generated into [TestInventory.md](TestInventory.md); the plan and the manual pre-release checklist are in [TestPlan.md](TestPlan.md).

**A real Kubernetes cluster.** The Kubernetes tab is also run against a real cluster by `TestLiveCluster`, through both ways in (a kubeconfig, and a host whose `kubectl` is the real one). It is skipped unless `VUTTY_LIVE_KUBECONFIG` (and `VUTTY_LIVE_CONTEXT`) are set. With [kind](https://kind.sigs.k8s.io/) and Helm:

```sh
kind create cluster --name vutty-test --kubeconfig /tmp/vutty-kube
export KUBECONFIG=/tmp/vutty-kube
kubectl create ns shop
helm create demo && printf 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: {{ include "demo.fullname" . }}-auth\nstringData:\n  password: {{ .Values.auth.password | quote }}\n' > demo/templates/secret.yaml && echo 'auth: {password: changeme}' >> demo/values.yaml
helm install web ./demo -n shop --set replicaCount=3 --set image.tag=alpine --set auth.password=hunter2
helm upgrade web ./demo -n shop --set replicaCount=3 --set image.tag=alpine --set auth.password=hunter2 --set service.port=8080
# a CRD widgets.example.com (printer columns Size .spec.size and Phase .status.phase) and a Widget w1 in shop: spec.size large, status.phase Ready
VUTTY_LIVE_KUBECONFIG=/tmp/vutty-kube VUTTY_LIVE_CONTEXT=kind-vutty-test go test ./internal/usecase -run TestLiveCluster -v
kind delete cluster --name vutty-test
```

**E2E notes.** `wails dev -browser` opens your default browser at the dev server, so the scripts make `open` do nothing for it (an extra tab on the unlock screen has a focused password field, and macOS then turns on secure input for every app, which stops input tools such as OpenKey). If you start `wails dev -browser` yourself, do the same or close that tab. `make e2e` starts `wails dev -browser` on a **free port** with a throw-away data directory, so it never collides with a `wails dev` you already have open, plus two demo SSH servers on 2201/2202 (stop `docker compose --profile dev` first: the containers use the same ports; `run.sh` checks). Never open the E2E server's URL yourself while it runs: its backend is shared, so a second tab disturbs the run.

## Docker

Vutty is a desktop app, so Docker is for CI and fixtures, not for running the GUI.

```bash
docker compose --profile dev up -d      # demo SSH servers :2201/:2202 and a Git remote :9418 (localhost only)
docker compose --profile ci build go-test   # Go tests inside a clean Linux container
docker compose --profile ci build web-test  # frontend typecheck + tests
docker compose --profile dev down       # add -v to forget the demo host keys
```

The demo servers keep a persistent host key in a volume, so restarts do not trigger "host key changed". The Git remote has no authentication: never expose it beyond localhost. Try sync with `git://localhost:9418/vutty-sync.git`.

## Deploy

```bash
scripts/deploy.sh dev  [--fast] [--fixtures] [--run] [--dry-run]
scripts/deploy.sh prod [--version X.Y.Z] [--e2e] [--tag] [--publish] [--yes] [--dry-run]
```

**dev** builds a debug app (devtools enabled) and installs it as **Vutty Dev**, with its own bundle id and data directory, and a `vutty-dev` launcher. It never touches production data. Paths are configured in `deploy/env.dev` and can be overridden from the shell.

**prod** is the release pipeline:

1. gates: `RELEASE_NOTES.md` has a `# Vutty X.Y.Z` heading, `--version` matches the `VERSION` file, the git tree is clean, you are on `main`, the tag (if it exists) points at `HEAD`;
2. tests: Go (`-race`) and frontend (optionally the E2E walkthrough with `--e2e`);
3. build with the version injected;
4. sign (`CODESIGN_IDENTITY`, hardened runtime) or ad-hoc sign with a warning;
5. notarize and staple when `NOTARY_PROFILE` is set;
6. package `dist/Vutty-<version>-macos-<arch>.zip` and `SHA256SUMS`.

Nothing leaves your machine by default. `--tag` creates a local annotated tag; `--publish` (which requires `--tag`) pushes the tag and creates a GitHub release, and asks you to type the version to confirm unless `--yes` is given. Use `--dry-run` to see every command first. Secrets are read from the environment only (see `deploy/env.prod`).

## Releasing checklist

1. Bump `VERSION`, write the `# Vutty X.Y.Z` section in `RELEASE_NOTES.md`.
2. `make notices` if a dependency changed (it rewrites `THIRD_PARTY_NOTICES.md`, which every download carries: inside the macOS app and next to it in the DMG, in the Windows installer and as a file of its own, in the Linux tar); commit the result.
3. `make test && make e2e && make inventory`; commit the refreshed screenshots and inventory.
4. Run the manual checklist in [TestPlan.md](TestPlan.md) on a real Mac.
5. `scripts/deploy.sh prod --e2e --tag --publish`.

## Releasing with GitHub Actions (macOS, Windows, Linux)

`.github/workflows/release.yml` builds the three platforms in parallel and attaches the files to a GitHub release. It is the normal way to publish; `scripts/deploy.sh prod` above stays for a signed and notarized macOS build.

1. Bump `VERSION` and write the `# Vutty X.Y.Z` section in `RELEASE_NOTES.md` (it becomes the release description). Commit and push to `main`.
2. Tag the same version and push the tag:
   ```
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```
3. About three minutes later the release has: `vutty-X.Y.Z-macos.dmg` and `.zip`, `vutty-X.Y.Z-windows-amd64.exe`, `vutty-X.Y.Z-linux-amd64.tar.gz` and `SHA256SUMS`.
   The workflow also attaches a copy of each file without the version in its name (`vutty-macos.dmg`, `vutty-macos.zip`, `vutty-windows-amd64.exe`, `vutty-linux-amd64.tar.gz`), which `SHA256SUMS` lists too. The download links of the README (`releases/latest/download/vutty-macos.dmg`) rely on them, so keep these names; the self-update looks its file up by the name with the version.

Notes:

- The tag must match `VERSION`; the build reads the version from the file. Do not reuse a tag.
- The release is created as a **pre-release**. The in-app *Check for updates* reads GitHub's `releases/latest`, which ignores pre-releases, so untick *Set as a pre-release* on the release page (or drop `--prerelease` in the workflow) for users to be offered it.
- If a release for the tag already exists (made by hand), the workflow adds the files to it (`--clobber`) instead of failing.
- Run it by hand from **Actions → release → Run workflow** to test the three builds: it only builds and keeps the files as artifacts; no release is made.
- `package-macos.sh` (or `make package`) makes the same `.dmg` and `.zip` on a Mac. Builds are not signed with a developer certificate, so macOS and Windows warn on first launch.
- Windows builds have no terminal on this computer yet (the pty library does not support Windows); SSH, SFTP and the rest work. Linux needs `libgtk-3-dev` and `libwebkit2gtk-4.0-dev` to build.

## Windows, bridge and screenshots

- **Second processes.** Wails v2 has one window per process, so an app window (`vutty --window <host id> <title>`, Duplicate in new window) is another process of the same binary (Settings is a page of the main window, no longer a process). It holds no data: `app/bridge.go` (loopback HTTP, random token in `settings-ipc.json`, mode 0600) serves `/appcall` (every use case service), `/events` (what the main process emits, kept only for terminal and SFTP sessions a window opened) and `/menu`. In an app window the frontend gets a `window.go.usecase` proxy (`installWindowBridge`), so the whole UI runs unchanged.
- **Stale app window.** An app window is a process of its own and runs the program (and the frontend embedded in it) it was started from. `make dev` builds `frontend/dist` first, because these other processes do not use the Vite dev server. A window left open by another version is refused by `/ping` (the version is compared) and quits.
- **Panes.** `Workspace` draws every pane of every tab in one flat, keyed list placed by `paneRects` (layout.ts); never nest panes again, or moving one would rebuild it and end its session.
- **Screenshots.** The E2E run numbers them, so adding a step shifts the numbers; `scripts/check-doc-links.sh` lists links that no longer point to a file (README and docs).
- **While the E2E run is going, do not edit files**: `wails dev` reloads and a reconnect overlay covers the page.

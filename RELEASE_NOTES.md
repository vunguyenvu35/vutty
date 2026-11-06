<p align="center"><img src="docs/brand/logo-mark-256.png" alt="Vutty" width="88"></p>

# Vutty 1.6.6

Release date: 2026-11-06 · Platform: macOS (arm64 build verified) · Status: beta. Read [Known issues](#known-issues) first.

## Unreleased

- **FTP and FTPS in the SFTP tab.** A host can have FTP (explicit TLS, implicit TLS or plain) next to SSH: it shows as "name (FTP)" in Select host and as a protocol choice. Browse, queue, resume, drag and drop and edit-and-save-back work as for SFTP; the password is in the vault; FTPS checks the certificate. Permissions and jump hosts do not work over FTP.
- **RDP.** A host can have Remote Desktop turned on; choosing RDP on the protocol screen starts your computer's own client (mstsc on Windows, Microsoft Remote Desktop on a Mac, FreeRDP or Remmina on Linux). Vutty does not draw the desktop and keeps no RDP password. Not yet tried on Windows or Linux.
- **Mosh.** Choose Mosh on the protocol screen: Vutty logs in over SSH, starts `mosh-server`, and runs the `mosh-client` of your computer (install `mosh` first; not on Windows, not through a jump host). The session key is passed in the environment and never logged. Needs UDP 60000–61000 open on the host.

## What's new in 1.6.6

- **Panel: the sections are in a list on the left, in three groups (#64).** Monitor (Overview, Processes, Logs, Incident), Run (Services, Docker, Kubernetes, Web, Schedule) and Manage (Ports & firewall, Security, Users, Maintenance), instead of one row of 13 tabs. What the Overview lists under *Needs attention* is marked on the section it belongs to (a failed service on Services, a certificate on Web, updates and the disk on Maintenance) and counted on its group, so it can be seen without opening the Overview. The list can show icons only (kept on this computer). Under 760 px it becomes a row.
- **Panel: Logs for an on-call (#63).** *Choose a range…* reads a log between two times you type (in the time of the server, whose zone is shown), **Previous boot** reads the boot before this one, a click on a line offers the 1, 5 or 30 minutes around it, **Context** keeps 3 lines around each match, and the range stays when you change the source. **Follow** is now a real stream (`journalctl -f`, `docker logs -f`, `tail -F`) with the lines sent in batches, not a re-read every 2 seconds; if it cannot start, it falls back to the re-read and says so.
- **Panel: an Incident tab (#63).** What the server wrote down about the last hour to 3 days or a range of your own: failed and restarted services, processes the kernel killed for memory, boots and shutdowns, error lines by minute (a bar opens the log of that time) and containers that started, stopped or restarted. It warns when the journal is kept in memory only (a reboot erases it) and offers, after asking, to keep it on disk.
- Not done on purpose: CPU and memory history while the app is closed. It needs either `sar` on the host or something installed there, and is left as the open question in [docs/PanelRoadmap.md](docs/PanelRoadmap.md).

Not yet seen against a real server: the time ranges, the previous boot, the Incident reads and the stream are covered by tests with sample output and by a shell check that the host command stops when the input ends; try them on a throwaway VPS first.

## What's new in 1.6.5

- **Panel: Users.** Add and delete users, manage their SSH keys, and a Reboot button that asks first (#48). The add-key form has room and a divider above it (#49).
- **Panel: Cron.** Add, change and delete the cron jobs of the login and of root (#50).
- **Panel: Ports & firewall.** Allow, deny and delete firewall rules, and turn ufw on or off (#52).
- **Panel: certificates, sites, sshd, Docker and system settings**, plus SFTP shortcuts and Fleet alerts (#59).

Not yet seen against a real server: try the new panel actions on a throwaway VPS first.

## What's new in 1.6.4

- **Kubernetes: Execute shell dialog.** A pod with several containers no longer squeezes its name into a few lines: the pod name and its container buttons stack (#46).
- **Kubernetes: ages keep moving.** The tabs reload every 3 s (was 5 s) and the Age column counts each second in between, so a table does not look stuck (#46).

## What's new in 1.6.3

- **Panel: Docker gets a shell, load, details, images and volumes.** *Shell* types `docker exec` (bash, else sh) into a terminal tab on the host. Each running container shows its CPU and memory. *Details* shows the image, command, restart policy, ports, networks and mounts, and the environment with its values hidden until asked. New **Images**, **Volumes** and **Networks** views: an image or volume is deleted only when nothing uses it, after asking; networks are read only. The actions of a row are one main button and a ⋮ menu.
- **Panel: Overview charts of 6 and 24 hours.** One point for each minute is kept on this computer for each host and drawn next to the live half hour. Nothing is read while the tab is closed, so those times are gaps in the line.
- **Panel: a Users tab (read only).** The accounts that can log in, who may use sudo, how many SSH keys each has (`?` when this login cannot read the file) and the logins open now.
- **Kubernetes:** a **Security** tab, *Copy kubectl*, port-forward for a cluster opened with a kubeconfig and *Debug container* (#32); **edit a workload on a host** (Edit YAML, Edit config, Add sidecar) (#33); a security review of the panel (login programs and their variables, names of files copied from a pod) (#37); the Shell button has the same width with and without its caret.
- **Zoom.** On a Mac the top bar, the room for the window buttons and the maximized Settings title keep their place at any zoom (the window buttons do not scale), the Settings popup is measured again when the zoom changes, and the zoom can be typed in Settings (80 to 160, with its limits).
- **Fix (Mac):** the top bar is as tall as the toolbar the window buttons sit in.
- **Fix (macOS): a crash when the screen locks or the Mac sleeps.** Locking the vault on a sleep or a screen lock told the page from the thread that macOS had posted the notification on, and Wails (up to 2.17.1) releases the string it queues for the page once too often, which on that thread kills the app in the run loop (SIGSEGV). The lock now runs on a goroutine and the notification waits for it, so the vault is still locked before the computer sleeps. It was found by reading the crash and reproducing the Wails pattern in a small program, and a test keeps the handler off that thread. Locking the screen with the app open did kill the running app before this change (reported on a Mac); that the fixed app survives the same lock is still to be seen.

Not yet seen against a real server: the Docker, history and Users parts are covered by tests with sample output; try them on a throwaway VPS first.

## What's new in 1.6.2

- **Files of a shell in a pod.** The Files section of the terminal sidebar now shows the files inside the pod when the terminal is a shell in a pod of a cluster opened with a kubeconfig: browse the folders, open a file with the system, copy files out into a folder of this computer. Read only (no upload, rename or delete yet), and a folder cannot be copied out yet. Nothing is installed in the pod: it needs `sh`, `stat` and `cat` there, so a container without a shell says so. Tried on real Alpine (busybox) and Debian (GNU) pods.
- **Fix:** the × of a filter box is centred in its circle.

## What's new in 1.6.1

- **Windows and Linux: no empty gap at the left of the tab bar.** The bar kept room for the red, yellow and green window buttons of macOS on every system; it is now left on a Mac only, so the first tab sits at the edge where the window has a frame of its own. Seen on Windows in a screenshot; not yet looked at on a real Windows or Linux machine.

## What's new in 1.6.0

- **Panel: a server dashboard in a tab.** On the protocol screen, **Panel** opens a dashboard of a Linux server instead of a terminal: Overview (CPU, memory, disk, network), Processes, systemd Services, Docker containers, Logs, and Ports & firewall (read only). Every change (end a process, start, restart or stop a service or container) asks first. It runs fixed commands over its own SSH connection and installs nothing. Verified against a real Ubuntu 24.04 for the overview, processes, ports and logs (the journal); the systemd and Docker parts are covered by tests with sample output only.

- **Panel grows into a VPS toolbox.** New tabs: **Web** (Let's Encrypt certificates with the time left, and the sites of nginx or Caddy with *Test configuration* and a *Reload* that tests first and reloads only if the test passes), **Maintenance** (pending updates with the security ones marked, disks, a cleanup that says how much it frees, and a folder-size scan), **Security** (failed SSH logins, fail2ban jails with *Unban*, the SSH settings) and **Schedule** (systemd timers with *Run now*, cron jobs, and a backup check: name a folder and see how old its newest file is). **Docker** groups containers by Compose project, with *Update* (pull, then up -d), Restart, Stop and Logs for the whole stack. **Logs** has *Follow* (reads again every 2 seconds). **Overview** keeps half an hour of CPU, memory, disk and network history while the tab is open, and warns about a certificate that runs out, security updates, a reboot and a stale backup. A new **Fleet** page lists every saved host (up or down, CPU, memory, disk, failed services), each over a short connection that never asks a question. Every change still asks first; docker volumes are never cleaned from the Panel. Covered by tests with sample output; not yet run against a real server.

- **Kubernetes in Panel.** A **Kubernetes** tab on a host runs `kubectl` there over the SSH connection the Panel already has, and installs nothing. A **cluster from a kubeconfig** (*Clusters*, with the kubeconfig kept in the vault) is reached by Vutty itself, with no `kubectl` and no server. Views: Overview, Pods, Workloads, Network, Config & storage, Resources, Apps, Nodes and Events, with a Shell in a pod, Logs, Scale, Restart, Roll back, Cordon and Drain. Every change asks first, is audited (never with its output) and is locked, with the reason, when `kubectl auth can-i` says the login may not do it; a production cluster keeps its red frame. A login that may only see some namespaces (a Rancher project) adds them by name. See [docs/KubernetesPanel.md](docs/KubernetesPanel.md).
- **Resources: any kind, CRDs included, read only.** Every kind the cluster serves is listed (discovery); pick one, read its objects with the columns a CRD asks for, and open one as YAML. Buttons for Namespaces, PersistentVolumes, StorageClasses and HorizontalPodAutoscalers. The Secrets of the core group are never shown here.
- **Apps: Helm releases, read only.** Releases are read from the Secrets Helm 3 keeps in the cluster, so nothing is installed. A release shows its chart, notes, manifest (the Secrets in it without their data) and its revisions; its **values** ask first and are audited, because they often hold passwords. *Copy rollback* puts the `helm rollback` command on the clipboard for a terminal: Vutty does not change releases. Chart repositories are not in the cluster and are not listed.
- **Logs of all the pods.** *Logs* on a Deployment, StatefulSet, DaemonSet, Job or Service reads every pod it selects (up to 20), merged by time, each line with its pod in a colour of its own.
- **Numbers on the tabs:** how many pods, workloads and network objects there are.
- **Edit a workload** (Edit YAML with a preview of what changes, Edit config, Add sidecar) for a cluster opened with a kubeconfig.
- **Fixes.** A screen that takes longer to load than its refresh interval (the Overview of a cluster behind Rancher) no longer stays on "Loading" for ever, and the loading ring keeps turning when *Reduce motion* is on.
- **Verified** against a real kind cluster (Kubernetes 1.37 with Helm 4) through both ways in, for Resources, Apps, the merged logs and the numbers (`TestLiveCluster`, see [docs/Development.md](docs/Development.md)). Not yet run against Rancher, a managed cloud cluster, or by clicking through the screens of the app; the rest of the tab is covered by tests with sample answers. Port-forward works for a host only, and editing a workload for a kubeconfig only.

## What's new in 1.5.1

- **The sidebar's Files section works for local terminals too.** A local terminal shows this computer's files (no SFTP connection, no queue). A button goes to the folder the terminal is in (the shell's OSC 7 when it reports one, else the shell's process: `lsof` on macOS, `/proc` on Linux, or the server's `/proc` over SSH). Right-click offers what the SFTP screen offers (open, open with, edit and save back, download, upload, copy path, rename, delete, new folder, permissions, select all), and dot files can be shown or hidden.
- **Go to folder** in that right-click menu types `cd` into the terminal and runs it: a folder goes to itself, a file to the folder it is in. Paths with quotes, spaces or `[brackets]` are quoted; a Windows path (drive letter or UNC) gets PowerShell's `cd -LiteralPath`. Not for `cmd.exe`.
- **The quick unlock key goes into a Keychain item guarded by Touch ID** where macOS allows it; the old item is the fallback.
- **Choose what to sync.** *Settings → Sync → What to sync* (a box you can fold) has a tick for each kind of data (Hosts and groups, Keys and passwords, Port forwards, Snippets) and for each group of preferences (Appearance, Terminal, Language, Keyboard shortcuts, Behaviour, Account name and email). All are ticked by default, so nothing changes until you untick something. An unticked item is neither sent nor taken: this machine keeps what it has and the repository keeps what the other machines put there. Port forwards need Hosts. The preferences travel in the same encrypted bundle as the data, each group with its own date (the newest change wins), and a new Mac that restores from the repository takes them. Zoom, folders, auto-lock, Touch ID and the other settings of one machine stay local. A copy of Vutty from before this drops the preferences when it syncs; the next newer copy puts them back. Not yet tried between two real Macs.
- **Fast drawing (WebGL)** for the terminal that has the focus: see *Known issues* 6.

## What's new in 1.5.0

- **Settings is a popup over the window, like VS Code's.** The gear, the navigation, the menu and Cmd/Ctrl+`,` open it; Esc, a click outside it and the × close it. It opens at a size taken from the window you are in (nearly all of it, at most 1280 × 880) and sits below the top bar, so it never covers the macOS window buttons. Drag its title bar to move it (it stays inside the window), drag its edges and corners to make it larger or smaller (it remembers the size as a share of the window; a double click on an edge restores the default), or Maximize it. The button before Maximize, *Open in a tab*, moves it into a Settings tab next to your terminals; while that tab exists, the gear and the shortcut show it.
- **Settings is no longer a window of its own.** It used to run as a second process with a hidden Dock icon, a helper app and a bridge to the main app; all of that is gone, so Settings opens at once and no longer needs a helper. Only *Duplicate in new window* still uses another process.
- **The host list of the terminal sidebar works like a file manager's tree.** One line per host (its name, then a muted `user@host` when that says something more), guide lines for each level, a folder icon and a count on every group, a dot on hosts with an open session, *Expand all* and *Collapse all*, and the arrow keys (up and down, right opens a group, left closes it or goes to its parent, Home and End). That is what DevOps asked for after MobaXterm.
- **SFTP copies keep the modification time of their source** (upload, replace and download), so a folder sync no longer copies a file again, or picks the wrong side, only because the copy was made later. Partial copies (`.vutty-part-…`) that nothing has written to for 7 days are removed from the folder of a transfer, once per folder and session.
- **Icons in the narrow navigation are centred.** In a narrow window, or with a large interface zoom, the invisible key hints still took room and pushed the icons to the left.
- Under the hood: the largest files were split by topic without changing what they do (`styles.css` into 10 files, the assistant, session and bridge code, the input-method helpers), a data race in a test of the AI assistant gateway was fixed, and the table of gaps in [docs/Assessment.md](docs/Assessment.md) was brought up to date (G1, G2, G11, G13, G14 and G17).

## What's new in 1.4.1

A release about downloading; the app itself is the same as 1.4.0.

- **Download links that never change.** Every release now also carries each file without the version in its name (`vutty-macos.dmg`, `vutty-macos.zip`, `vutty-windows-amd64.exe`, `vutty-linux-amd64.tar.gz`), listed in `SHA256SUMS` like the others, so `https://github.com/vunguyenvu35/vutty/releases/latest/download/vutty-macos.dmg` is always the newest version. The README uses them. The files with the version in their name are still there (the app that updates itself looks its file up by that name).
- A copy of 1.4.0 can update itself to this version from inside the app (macOS, Apple silicon): *Help → About → Check for updates → Install and restart*.

## What's new in 1.4.0

- **Deleting a group deletes what is in it.** Its hosts, its child groups and their hosts go with it (and the port forwards and Telnet passwords of those hosts). The confirmation says how many. Before, the hosts moved to the parent group.
- **The interface speaks four languages: English, Vietnamese (Tiếng Việt), Japanese (日本語) and Chinese (中文, simplified).** *Settings → General → Language*, or Automatic to follow the system; it applies at once, also to the menu bar of the window. About 1300 texts are translated. Not translated: what servers and the backend say (error messages from a connection, for example), names of products and protocols, dates and times (they follow the system), and the documentation. The translations were written by an AI model and have not been read by native speakers yet: please tell us what sounds wrong.
- **The first update made from inside the app.** A copy of 1.3.0 can now go to this version with *Help → About → Check for updates → Install and restart* (macOS, Apple silicon); the release's checksum list is signed with the project's key. This is the first real update made this way: if it does not work for you, install from the release page and tell us what the app said.
- **Output waits for the terminal** (flow control): the backend stops reading a session while more than 512 KB of its output is sent but not yet drawn, so that `yes` or `cat` of a huge file slows down instead of piling up in the window.

## What's new in 1.3.0

**Updates that install themselves (macOS, Apple silicon).** *Help → About → Check for updates → Install and restart* downloads the new version, checks that the release's checksum list is signed with the project's key and that the download matches it, replaces the app and restarts it. An update that fails either check is not installed. It does not need an Apple certificate: macOS only asks about files that a browser downloaded, and the signature, not Apple, is what makes the update trustworthy ([Release signing](docs/ReleaseSigning.md)). **This version has to be installed by hand once** (earlier versions do not carry the key); from the next release on, updates install from inside the app. Windows, Linux and Intel Macs still use the release page.

**Signing in and the vault**
- **Servers that ask for more than a password** (a verification code, a Duo choice, a one-time password): a *Sign in to <host>* box shows the server's own questions, over the terminal, in the SFTP pane, the sidebar, port forwards and *Test connection*. A saved password answers the password question by itself, and nothing you type is saved or logged.
- **Vault health check.** Keys and passwords that were sealed with another key (the leftover quick-unlock key of older versions) are marked *Can't be read* in Keychain, and a host that uses one says what to do instead of "message authentication failed". Enter it again with **Edit**.
- **Sync refuses to send** what was sealed with a key that is not the vault's, and a Mac with nothing in it can simply use the repository's vault (*Use the repository's vault*).
- **Edit a key, password or ssh-agent identity** in Keychain; the hosts that use it keep using it.
- **Lock when the computer sleeps** (on by default): the vault locks as a Mac goes to sleep and when its screen is locked; elsewhere it locks when the computer wakes up.

**Everyday work**
- **Run on hosts**: one command on several hosts at once, each on its own connection, with the exit code and output of every host (the first 64 KB), a timeout, *Cancel* and *Copy all output*. A confirmation is asked when a production host is among them.
- **Snippet blanks**: `{{name}}` and `{{name|default}}` in a script are asked for when it runs; `{{host}}`, `{{hostname}}` and `{{user}}` are filled in by *Run on hosts*.
- **Forward my keys to this host** (`ssh -A`): your running ssh-agent, or the host's saved key when there is none; off by default, and read from and written to ssh config.
- **Production hosts** are marked (a red frame and a PROD badge) and, with *Be careful on production hosts* (on by default), Vutty asks before text with a line break is pasted there or a snippet is run.
- **`ssh://` links** open a saved host after a yes, or the New host form filled in: a link never connects by itself.
- **Session logs** (off by default): one text file per session, line by line with the time, readable only by you.
- **SFTP**: choose *This computer* on the right side to copy between two folders of your own machine.
- **Import hosts from a cloud or an inventory**: a listing you made with `aws ec2 describe-instances`, `gcloud compute instances list`, `ansible-inventory --list` (or an INI inventory) or a CSV. Vutty never contacts a cloud and never sees its credentials.
- The **Connect** button of the host search connects only to a saved host typed in full or to a complete address; a half-typed search just filters.

**Look and feel.** Settings was redesigned (grouped menu, page titles, grouped rows) and opens at once with the values it showed last time, on an opaque background; Import & export has three tabs (Import, Export, Backup); check boxes have one design everywhere.

## What's new in 1.2.1

- **Sync now works between two machines that made their own vaults.** Each vault has its own random key, and a plain sync stopped with *encrypted with a different vault key* even when both machines used the same master password. **Settings → Sync** now shows a box for this: enter the master password of the vault that made the repository and choose **Join this vault and sync**. This machine keeps its hosts, groups, snippets and saved keys, merges them in and, from then on, uses that vault's master password. A copy of what it had is saved first in `backups/before-joining-…`; a wrong password changes nothing.
- **Replace a repository that nobody can open.** If the bundle in the repository was not locked with the key of its own header (it cannot be opened even with the right password), Join cannot help. The same box has *The password does not work…*: type `REPLACE` and the repository then holds this machine's data. The two files that were there are copied to `backups/replaced-remote-…` first, and a repository that this vault can read is never replaced.
- **Quick unlock checks its key against the vault.** The key kept in the OS keychain was checked only against the first saved secret, so a vault with no secret yet accepted any key left by an older vault, and then sealed the sync bundle with that wrong key. The header now carries a key check (older vaults get one at the next unlock with the password). A key that does not belong to the vault is refused and removed, and the master password is asked for.
- **The Join / Replace box stays after a failed join**, with a message that says what is wrong, so the way out is not taken away.
- Sync and the Settings window: the Settings window used an old helper after a rebuild or an update of the app (it showed an old version of everything); it now runs only the program that is running, and a window of another version is refused.
- **New host and New group start in the group you are in.** Open a group on the Hosts page and press *New host*: the *Group* field is already filled in with it (you can still change it). The same goes for *New group*, which is created inside the open group. Editing an existing host never moves it.

## What's new in 1.2.0

- **VuTerm is now Vutty.** The VuTerm name was used for a few days only, and Vutty is shorter and says more (a *tty* is a terminal). The logo mark is unchanged, the wordmark now reads Vutty. Your data comes with you from VuTerm and from VVTerm alike: on first start the data folder (`vuterm` or `vvterm`) is renamed to `vutty`, the database file is renamed too, saved view settings are copied to the new names, `$VUTERM_DATA_DIR` and `$VVTERM_DATA_DIR` still work next to the new `$VUTTY_DATA_DIR`, and Quick unlock (Touch ID) and `.vvbak` / `.vubak` backups keep working. The app is now `vutty.app`, and the update check and the links point to `github.com/vunguyenvu35/vutty`.

- **VVTerm is now VuTerm.** Another project already used the name VVTerm, so this one is renamed. The logo mark is unchanged. Your data comes with you: on first start the data folder `vvterm` is renamed to `vuterm`, the database file is renamed too, saved view settings are copied to the new names, Quick unlock keeps working, and backups, sync bundles and `~/.ssh/config` files exported by VVTerm are still read. The old `VVTERM_DATA_DIR` setting still works (the new one is `VUTERM_DATA_DIR`). The app is now `vuterm.app` / `vuterm-<version>-…`, and the GitHub repository is `vunguyenvu35/vuterm` (the old address redirects). Backups you save get the extension `.vubak`; `.vvbak` files still open.

- **Command suggestions and History are kept for each host.** The list of commands you typed was shared by every terminal, so a `cd Data/projects/...` typed on your Mac was offered at a server where that folder does not exist. Each host (a saved host, or *local*, a serial port, a Telnet host) now has its own list, used by the suggestions and shown in the sidebar's History tab, and *Clear history* clears only that host's. Commands typed before this update are not carried over (they cannot be told apart by host); snippets are still offered everywhere, as they are yours on purpose.
### SFTP is complete

- **A transfer queue.** Uploads and downloads run in the background, two at a time, and are listed in a **Transfers** panel with a bar for each one and **Pause**, **Resume**, **Cancel**, **Retry** and **Clear finished**. You can keep browsing, or start more transfers, while they run. The batch still asks **once** before replacing files that already exist.
- **Resume.** A transfer that is paused, fails or is cut by a lost connection continues from the bytes already copied instead of starting over, for uploads and downloads. The partial copy is named after the size and time of the source, so it is only continued when the file has not changed; the last 64 KB are written again in case the last chunk was cut short. **Cancel** deletes the partial file.
- **Sync folders.** Compare a local folder with a remote one (either direction), see what is new or changed, then copy it through the queue. Nothing is ever deleted at the target; the files only it has are listed.
- **Edit and save back.** Right-click a remote file → **Edit and save back**: it is opened on your computer and uploaded again each time you save, keeping the permissions of the file it replaces. If the server's copy changed meanwhile, you are asked before it is replaced.
- **Files in the terminal sidebar.** A **Files (SFTP)** section beside every SSH terminal: browse, upload, download, with the same queue.
- Replacing a file over SFTP now keeps its permissions (a `0600` key stays `0600`).
- **For developers:** `RemoteFS.Upload` and `Download` take a context; `RemoteFS.Stat` is new; new `SftpService` calls `Enqueue`, `Queue`, `Pause`, `Resume`, `Cancel`, `Retry`, `ClearFinished`, `Conflicts`, `SyncPlan`, `EditStart`, `EditPush`, `EditStop` and events `sftp:queue:<sid>`, `sftp:edit:<sid>` (see Architecture notes in `internal/usecase/queue.go`). `testssh` can serve the real file system for tests that need permissions.

- **Panes can be dragged by their header.** The bar with the host name that shows over a pane of a split is now a handle: drop it on a side of another pane to move the pane there (also into another tab), **on the tab bar to make it a tab of its own**, or on another terminal tab to join it. The pane keeps its session, nothing reconnects. The "Move pane to its own tab" button is still there.
- **It is now clear which pane has the keyboard.** In a tab with several panes the focused pane has a frame in the accent colour and a dot by its name on the pane bar, and the other panes are dimmed (a little less under the pointer, as an invitation to click). A tab with one pane looks as before.
- **A tidier bar on each pane.** The bar that shows over a pane of a split has a handle (six dots) to drag the pane by, the name in bold with a small tag for what it is, quieter buttons (the close button turns red under the pointer) and no tooltip over the name.
- **For developers:** `make e2e` and `e2e/update-shots.sh` no longer open your default browser. `wails dev -browser` does that by itself, and a tab on the unlock screen (a focused password field) makes macOS turn on secure input for every app, which stops input tools such as OpenKey. If you start `wails dev -browser` yourself, close that tab (see Development.md).

## What's new in 1.1.2

- **The Settings window answers at once when it opens.** Until now it showed a blank "Loading…" page until the main process had answered, so a busy main process left the menu missing or unclickable for seconds. The window and its menu now appear immediately and work while the saved settings are on their way; the pages (and the Theme switch, which would otherwise save placeholder values) wait for them. Measured in the system web view with a main process made 4 s slow: the menu appeared after 25 ms instead of 4 s, and every click was answered in under 15 ms.
- **Less loaded on first open.** The Settings window no longer loads the terminal app's code (the page it needs went from 210 KB to 67 KB compressed), and the AI assistant, Sync and Support pages are loaded when first opened. Focusing the window again re-reads the data at most every 3 seconds.
- **The AI tools are looked up when first needed**, not at start-up: starting each CLI takes seconds of CPU, which used to compete with a Settings window opened just after the app launched.
- **Command suggestions cost next to nothing while a program writes a lot of output**, and nothing is looked up for a pasted line of hundreds of characters.

## What's new in 1.1.1

A patch release: fixes for typing and for the macOS input tools, and two new things in the terminal and in Settings.

### New: command suggestions in the terminal

- **A list of matching commands appears under the cursor while you type.** Each row says where it comes from: a command you ran in VVTerm, the shell history of this computer (local terminal: `~/.zsh_history`, `~/.bash_history`), one of your one-line Snippets (also found by its name) or, if you turn it on, the shell history of an SSH server.
- **Nothing is taken unless you choose it.** ↓ ↑ choose a row, then **Enter** or **Tab** put it on the line (it is never run for you); **Esc** closes the list. Until a row is chosen, Enter, Tab and the arrows are the shell's, as always.
- **⌘⇧G** (Ctrl+Shift+G elsewhere) turns suggestions off and on; **⌘⇧J** (Ctrl+Shift+J) opens the list by hand, also on an empty line. Both are in the **View** menu and can be changed in Settings → Shortcuts. **Settings → Terminal → Suggest commands** switches it off for good.
- **Safe by design.** No suggestions at a password prompt, in vim or htop, or while the shell is changing the line; a line that looks like a password, token or key is never suggested; a Snippet of several lines is never suggested (its first lines would run at once). The shell history of an SSH server is **off by default**, read once per terminal, kept in memory only, and goes through the secret filter on the way in.
- **The history keeps the command that ran**, as the screen shows it, instead of the keys that were pressed: a line completed with Tab or with your shell's own suggestion is saved whole, and a command with long output (`ls -la`) is no longer lost.

### New: Settings → Storage

- **Clear cache** removes the files VVTerm can make again and forgets what it found out about your AI tools (they are looked up again). It says what it removed.
- **Reset settings** puts appearance, terminal, general options, shortcuts and auto-lock back to their defaults, after a confirmation. Your profile, sync target, hosts, keys and vault are never touched.

### Fixed

- **A password field no longer leaves macOS "secure input" switched on.** While it is on, input tools such as OpenKey and GoTiengViet stop working in every app. Clicking an empty part of the unlock card (or of a panel with a password field) and then unlocking used to leave it on until some text field was focused; VVTerm now releases it. Measured with the system web view: a focused password field turns it on, a click on empty space keeps it on, focusing a text field turns it off.
- **Vietnamese typed in the terminal on macOS**: words replaced by the input method (`insertReplacementText`) were lost.
- **Settings → AI assistant no longer re-reads the whole page every 0.6 s** while it looks for your AI tools, which could make the page unresponsive; it also keeps what you are typing, and no longer makes the main window refresh for it. A failing load can no longer repeat itself on every notification.
- **Settings window ran a stale helper** after a rebuild or an app update.
- Development only: `make dev` hides the line macOS' Vietnamese input method prints for every key typed.

## Also in 1.1.1: AI terminal assistant

Ask an AI about what is on your terminal, without leaving VVTerm and without handing it your secrets. VVTerm runs the official AI command-line tool you installed and signed in to, and **no API key is stored**. The use of each tool is governed by its provider's terms; VVTerm is not affiliated with Anthropic, OpenAI or Google.

### Get started

1. **Settings → AI assistant**: switch on a tool VVTerm found (Claude Code, Codex CLI, or a command of your own; the Antigravity and Gemini CLIs are listed but cannot be switched on yet, see Limits). It stays off until you do, after a short isolation test.
2. Open a terminal, then the ✨ icon in the sidebar. Pick the provider and model from the chip under the message box, and ask.
3. Drop a log or a screenshot on the panel, or paste one, to add it to your question.

### What you get

- **A chat beside every terminal.** It reads the screen (or only the text you selected), explains errors, and suggests commands. Answers show code as blocks with a Copy button; an empty chat offers a few starting questions. The sidebar can be dragged wider or narrower (double-click the edge to reset).
- **It looks at your machine for you.** Commands that only read (`which`, `pgrep`, `ls`, `df`, `brew list`, `systemctl status`, `git status`… also chained with `;`, `&&`, `||` or a pipe) run **without asking**, and the assistant reads their output before it answers. This is on by default (**Settings → AI assistant → Run read-only commands by itself**) and never applies to production hosts.
- **Anything that could change something asks first, in the chat.** A command that edits, installs, restarts or deletes shows **Run…** and a confirmation in its own card; a risky one needs you to type `run`. **Insert** puts a command on the prompt without pressing Enter. Risk is judged by VVTerm, never by the AI. Nothing opens as a popup.
- **Files and screenshots.** Text files (up to 5, 100 KB each) are added to the question and redacted like the screen. Pictures (up to 3, 5 MB each) work with Claude Code and Codex CLI.
- **Several providers, real model lists.** Models are read from each provider itself (nothing is hard-coded) and chosen from a menu with full descriptions. A provider that cannot list its models uses its default one.
- **MCP servers.** Add MCP servers (a command or an HTTP address), test them, and choose a mode per tool: *Allow*, *Ask each time* or *Off*. New tools of a server stay off until you review them. Import servers from Claude Code or Gemini CLI. Currently used through Claude Code.
- **Per-host switch.** On a host's form: *Off*, *Ask first (in the chat)* or *On*. Hosts are **On** by default and **Off** on production hosts.
- **Typing Vietnamese (and other input methods)** in the terminal now shows the text you are composing at the cursor, highlighted, until you commit it.
- **Vietnamese and other accented text typed in a terminal** is now sent to the shell as whole letters. macOS input methods can deliver a letter in pieces (the `ệ` of *việt* as `e` + dot + circumflex); `zsh` cannot edit those and showed them as `<0087>`.

### Your secrets stay yours

- Before anything is sent, keys, tokens, passwords, URLs with credentials, `NAME=value` secrets and **everything saved in your vault** are replaced by `*******`. The same is done to every answer before you see it, and to commands before they can run. You can add your own patterns; you cannot turn redaction off.
- **What will be sent** shows the exact redacted text, inside the chat, before or after you ask. Every use is written to the activity log (Logs → Assistant).
- A command with a hidden value cannot be run from the panel. A secret in a tool's arguments blocks the call; tool results are redacted.
- **Pictures are the exception**: a secret inside a picture cannot be found, so VVTerm asks you to confirm each time (in the chat) and says so plainly.
- The AI tool runs in an empty temporary folder with a cleaned environment and no tools of its own. A provider can only be switched on after a self-test in which it is asked to run a command and to search the web, and must fail at both. Codex CLI is started with its shell tool, its apps connector and its web search turned off.
- Redaction is best effort. A password written in an ordinary sentence cannot always be recognised.

### Also new: sync through a cloud folder

**Settings → Sync → Cloud folder**: pick a folder inside Google Drive, iCloud Drive, Dropbox or OneDrive. Only the two encrypted files are written there, with no sign-in and no token. Extra "conflict copies" made by the cloud client are merged per record (newest edit wins) and removed. Git sync still works as before.

### Also new: find things in every list

- **Snippets, Port Forwarding, Known Hosts and Keychain** now have what the host list has: a search box, filter chips (state and type of a forward, key type of a known host, key / password / ssh-agent in the Keychain), a **Sort** menu (name, host, type, status, port, recently changed…) and a **Grid / List** switch. The **host list** gets a Sort menu too (name, production first, recently connected), the **Logs** page sorts by newest, oldest, host or problems first, and each **SFTP pane** sorts by name or size or date (folders always first). The sort and the view are remembered for each page. Names sort the way people read them (case and accents ignored, `web-2` before `web-10`).

### Also fixed in this release

- **The vault no longer auto-locks while you are working in a terminal**: typing and output now count as activity.
- **Locking the vault closes the Settings window.** The Settings window also opens in front of the main window (macOS 14+), without a white flash, and its AI page appears at once.
- **A host list that cannot be reached** from the Settings window now shows a message you can act on instead of an internal service name.

### Limits you should know about

- **Antigravity CLI cannot be switched on yet.** Its web search tool keeps working whatever deny rule is given (several were tried), so text from your terminal could leave in a search query, and the isolation self-test correctly fails. It will be usable when Antigravity offers a way to turn that tool off.
- Gemini CLI shares `~/.gemini` with Antigravity, which sets a sign-in the Gemini CLI does not support. Sign in again with `gemini` (Login with Google) to use it.
- The Gemini CLI adapter could not be tested against a signed-in account; its isolation is checked by the self-test when you switch it on.
- MCP tools work through Claude Code only. Pictures work with Claude Code and Codex CLI only.
- The assistant is not run on Windows or Linux yet, and chat history is kept in memory only (a new chat starts empty after a restart).
- If you saved the AI settings before this release, your saved value of *Run read-only commands by itself* is kept; switch it on in Settings if you want the new default.

Design, verification notes and the live checks that were run: [docs/AIAssistant.md](docs/AIAssistant.md). How to use it: [user guide](docs/UserGuide.md#22-ai-assistant).

## Highlights

- **New host, step by step**: a form docked next to the list (no popup) that follows the natural flow, with **Test connection** (checks address, credentials and jump host without saving anything) and **Connect** (save + open a terminal).
- **Settings as its own macOS window**: a real window with its own title bar, movable outside the main window, with macOS full screen, holding terminal themes (26), light / dark / auto appearance, font, cursor, scrollback, security and sync, plus help links and the version (⌘/Ctrl + ,). It closes when you quit VVTerm.
- **Forgot your master password?** The unlock screen now offers a clearly explained **reset** that keeps your hosts and erases only the secrets.
- **SFTP**: a file browser in its own tab, through jump hosts, with upload, download, new folder, rename, delete and a confirmation before anything is replaced.
- **More in Settings**: new *General* (interface zoom, reduce motion, confirm before closing) and *Account* (local profile, ready for sign-in later) sections, **shortcuts you can change**, and a window you can maximise, roll up and resize.
- **Snippets**: save commands you run often and type them into the focused terminal with one click. They sync, back up and export like everything else.
- **New interface**: a *Vaults* tab with the terminals as tabs, left navigation, one search box that also connects (`ssh user@host`), group and host cards, custom dropdowns.
- **New brand**: logo and wordmark on one row, macOS and Windows icons, favicon, social preview ([brand kit](docs/brand/README.md)).
- **Docker** for CI and development fixtures, and a **dev / prod deploy script**.
- **Terminal sidebar** (hosts, snippets with Run / Paste, command history, themes and font), **drag a tab onto a pane to split** without reconnecting, drag tabs to order them, take a pane back out into a tab, and **Duplicate in new window** (the whole app in a second window).
- **Clearer connecting**: *Choose protocol*, a connecting screen whose line moves with the real connection (error, Retry, Show logs), connected / lost badges on tabs, a **New Tab** page with recent connections.
- **This computer, Serial and Telnet** (own login, encoding), Known Hosts and Logs pages, output highlighting, copy on select and right-click paste, tag filter, drag hosts into groups, SFTP right-click menu with permissions.

## What's new in 1.1.0

### Windows, tabs and panes
- **Tabs**: the + opens a New Tab (search hosts or tabs, Local Terminal, recent connections). Tabs shrink like a browser's, show the server-type icon and a spinner / green / red badge, can be dragged to reorder, renamed, duplicated and split from a right-click menu.
- **Drag a tab onto a pane** to split it left / right / top / bottom; **Move pane to its own tab** undoes it. All panes of all tabs are drawn in one flat list, so moving a pane never rebuilds it and its session is not interrupted.
- **Duplicate in new window** opens the whole app in a second window (another process) that shares the vault, the hosts and the sessions' owner through a local bridge (loopback, random token in a 0600 file, see [ADR-0006](docs/CleanArchitecture.md)). Sessions it opened end with it.
- A native menu bar (File, Edit, View, Window, Help) that stays the same in the Settings window; the browser's own right-click menu (Look Up, Translate, Inspect Element) is turned off.

### Connecting
- *Choose protocol* (SSH, Mosh shown but unsupported, Telnet when enabled) on by default (**Settings → Terminal → Ask which protocol to use**); a connecting screen with a line that eases toward the real stage (connecting, authenticated, ready); on failure the line stops, turns red and shows the reason with Retry, Show logs and Close.
- The backend reports the stages (**term:progress**) and says *ready* as soon as the shell is open: the activity log, server-type detection and automatic forwards no longer delay it.

### Terminal
- Sidebar: Hosts, Snippets (one line; click for the whole script with Run and Paste), History (only echoed lines: nothing typed at a password prompt), Themes and font. Terminal on this computer, Serial ports, Telnet (own user name, password in the vault, UTF-8 / ISO-8859-1), per-host theme with picture cards, output highlighting, copy on select, configurable right-click and copy / paste keys, broadcast input and maximise for panes. Default terminal text is now green.
- Page shortcuts ⌘ 1 … 6 and ⌘ N / ⌘ Shift T; the key of an action shows at the right of its navigation item when you point at it.

### Hosts, files and pages
- Tag filter, **drag a host onto a group** (or onto *All hosts* to take it out), Grid / List view, *All hosts* lists every host including those in groups, a New host menu (New group, Import).
- SFTP: two panes, several files and folders at once, drag and drop, one *Replace?* question per batch, a right-click menu (Open, Open with…, Copy to target directory, Rename, Delete, Refresh, New Folder, **Edit Permissions**); remote files open from a private temporary folder, and files that run programs (.command, .app, .sh, .exe…) are never opened from here.
- **Known Hosts** (forget a trusted key) and **Logs** (connections, transfers, forwards, host keys, unlocks; never a secret), Keychain with *New Key* (private, public, certificate, drag a key file in), the Port Forwarding wizard.
- The database gets columns added later on every start (schema v4 + idempotent additions), so a database that already had the newest version is never left behind (the cause of a *no such column* start-up error).

### Known limits of this part
- Mosh, cloud provider integrations (AWS, DigitalOcean, Azure) and multiplayer are not built.
- Telnet is not encrypted. Keys stored *hashed* in `known_hosts` are listed but cannot be forgotten from the app.
- The new window and the Settings window are separate processes: the new window has its own Dock icon, the Settings window is hidden from the Dock (macOS then shows no menu bar of its own for it). A change made in one window is picked up by the other when it next refreshes.
- Typing Vietnamese with a system-wide input method (GoTiengViet, OpenKey…) can fail to compose in the terminal: those tools treat unknown apps differently; add VVTerm to their app list.
- Native windows, the menu bar and the Dock icon cannot be screenshotted by the end-to-end run and were checked by hand only where noted.

### Add a host
- The New host / New group / New snippet forms are **docked beside the list** and show a draft card in the grid while you type. They close when you move to another screen instead of staying open.
- Flow: address → general (label, group, environment colour, tags) → SSH port → credentials (username and password, or a key identity; eye button to reveal) → options (jump host, keepalive).
- A typed password is stored in the vault as an identity; the host only references it.
- **Test connection** runs through the jump chain without saving a host or an identity, confirms unknown host keys inline, never shows the password, and reports why a test failed (authentication, unreachable, key not trusted).
- **Connect** saves, then opens the terminal. **Save** only saves.
- Every control has the same height and style; the native macOS dropdown was replaced by a **custom dropdown** (hints, check mark, keyboard: arrows, Enter, Esc, type-ahead; opens upward when there is no room).

### SFTP
- Folder button on a host card opens an SFTP tab on a separate SSH connection (jump chain supported, host key confirmation like a terminal). Browse, breadcrumbs, path box, Up / Home / Refresh.
- Upload, download, new folder, rename, delete (recursive, after confirmation). Existing files are never replaced silently on either side. Transfers use a temporary file and are renamed when finished; progress is shown.
- Connection drops show a banner with Reconnect. Closing the tab asks first. Snippets still go to the last terminal, never to an SFTP tab.
- Limits: single files only, no queue or resume, no drag and drop.

### Settings window
- **A second native window** (a second VVTerm process, see [ADR-0006](docs/CleanArchitecture.md)): the standard red / yellow / green buttons, resize, full screen. It opens from the navigation, the gear or the shortcut, is raised instead of opened twice, and closes when the main app quits. Where no native window exists (browser mode, the E2E run) the same settings appear in an in-app floating window (drag, roll up, maximise) (Esc closes an open dropdown first). Opens from the navigation, the gear in the title bar, or ⌘/Ctrl + , (also inside a terminal).
- Left menu: General, Account, Terminal, Security, Sync, Shortcuts, then Theme (Auto / Light / Dark), Documentation, Request a feature, Report a bug, Release notes, Support on Ko-fi, and the Version.
- **Terminal theme picker**: 26 themes in a two-column list with live swatches, dark/light filter, *Match app theme*, and a live preview with your font, size and cursor.
- Font, size stepper, cursor style, blinking, scrollback. Auto-lock, keychain unlock and master-password change under Security.
- **General**: interface zoom 80 %–160 %, reduce motion, confirm before closing a tab. **Account**: display name and email (local only), plan, disabled *Sign in*.
- **Shortcuts**: click a key chip and press a new combination. Duplicates and plain keys are refused with an explanation; reset one or all. `Ctrl+Tab`-style defaults work on every platform (Mod means ⌘ on a Mac and Ctrl elsewhere).
- Old `settings.json` files keep working: new fields get defaults.

### Lost master password
- *Forgot password?* on the unlock screen explains that the password cannot be recovered and what a reset does.
- **Reset vault** (type `RESET`): erases the vault and every key/password identity, keeps hosts, groups, snippets, port forwards and ssh-agent identities, clears the Keychain entry, and turns the screen into *Create vault*. It works while locked. An encrypted backup can be restored afterwards.

### Snippets
- Create, edit, delete snippets (label, optional description, multi-line script, validated: 64 KiB limit, no NUL bytes).
- **Run** types the script and presses Enter in the focused pane of the last terminal; **Paste** types it without Enter. A *Snippets* menu in the title bar does the same inside a terminal.
- Included in Git sync (merged per record), encrypted backups and the plain JSON export. The database upgrades itself from v1 to v2.

### Interface and brand
- Title bar with a **Vaults** home tab, one tab per terminal, `+`, settings gear, snippet picker and split buttons; drag the bar to move the window.
- Left navigation: Hosts, Keychain, Port Forwarding, Snippets, Import / Export, Settings, Lock vault. It shrinks to icons in a narrow window and the New host panel floats instead of squeezing the list.
- Hosts page: *Find a host or ssh user@hostname…* with **Connect**; an unknown target opens *New host* pre-filled. Group cards open with breadcrumbs.
- Logo and wordmark share one row; all emoji and text glyphs used as icons were replaced by one SVG icon set.

### Engineering
- `docker-compose.yml`: demo SSH servers (persistent host keys), a throw-away Git remote, and CI runners. Go tests also pass inside a Linux container.
- `scripts/deploy.sh dev|prod` with release gates, signing/notarization hooks, zip + checksums and an explicit confirmation before anything is published. `VERSION` is the single source of the version number.
- `e2e/run.sh` picks a free port, so it never collides with a `wails dev` you already have open.
- Go 1.26 is declared consistently (`go.mod`, Docker, CI, README). `docs/Development.md` for contributors; the README is written for people trying the app.

### Fixes
- The window could not be dragged by the title bar (wrong CSS mechanism for Wails).
- A maximised Settings window used to cover the macOS window buttons, showing two sets of dots; it now leaves the title bar free.
- The sidebar *Theme* dropdown showed a heavy box on hover; it now highlights softly.
- Holding a zoom shortcut could lose steps (each press used stale settings); saves are now applied immediately and in order.
- The *Vaults* tab stretched to 220 px (a CSS class collided with the page container).
- The wordmark's two V letters touched or drifted apart; the navigation had an odd gap in the middle.
- The page could scroll horizontally for a moment when a panel slid in.
- Navigation buttons had no accessible name when only icons were shown.
- Switching Keychain tabs while a save was still running pulled you back to the first tab.
- Typing `user@host` did not match saved hosts.
- Dock icon: the built app contains the new logo; macOS may show the old icon until its icon cache is refreshed (restart the Dock, or move the app).

## Quality gates

| Gate | Result |
| --- | --- |
| `go vet ./...` | clean |
| `go test -race ./...` | **296 passing** (test functions and sub-tests), 0 failing, 20 packages; also green in a Linux container |
| Go statement coverage | **80.6 %** (`internal/…` and `app`) |
| Frontend `tsc --noEmit` + Vitest | clean, **575 passing** in 35 files |
| End-to-end walkthrough (real backend, SQLite, vault, two SSH servers) | **passed**: 20 steps, 60 screenshots in `docs/screenshots` |
| Architecture rule (`internal/archtest`) | 5 tests, verified to fail on a deliberate violation |
| `scripts/deploy.sh` | dev install, every prod gate and a dry run exercised |
| Docker dev fixtures | SSH host keys survive a restart; Git remote accepts a push |

The complete list of test cases is [docs/TestInventory.md](docs/TestInventory.md); the plan and the manual checklist are in [docs/TestPlan.md](docs/TestPlan.md); the honest evaluation is [docs/Assessment.md](docs/Assessment.md).

## Known issues

1. **Keychain unlock is biometric-gated by the app, and by macOS only in a signed build.** Vutty now stores the vault key in a Keychain item with a Touch ID access control (`.biometryCurrentSet`) and moves an older item into it at the next unlock. macOS refuses to create such an item without the `keychain-access-groups` entitlement (OSStatus -34018, which a test binary and an ad hoc signed app both get), so the published builds fall back to the older item: Vutty asks for Touch ID, then reads an ordinary item, and anyone who can read your login keychain can unlock the vault. The guarded item was **not** exercised on a real Touch ID in this environment (no Developer ID is available here).
2. **Locking when the Mac sleeps** relies on the notifications macOS sends. The one for a *sleep* was exercised by a test that posts it by hand; the one for a *locked screen* needs the running app and was not exercised. On Windows and Linux the vault locks when the computer wakes up, and a sleep of a few seconds is not noticed.
3. **Settings is a popup of the main window** (since 1.5.0); it was a second process before. *Duplicate in new window* still runs the whole app in a second process, through a local, token-protected bridge to the main app, so it needs the main app running. The Settings popup (moving, resizing, its distance from the macOS window buttons, *Open in a tab*) was tested in a browser against the real backend and with component tests, **not in the packaged native app**. The screenshots of Settings in `docs/screenshots` are from the older window and are not regenerated yet; please check the popup on your Mac.
4. The packaged app was built and its icon, About box and bundle were checked, but it is **not notarized**: on first launch right-click → Open. The *release* pipeline can sign and notarize when you provide a Developer ID.
5. The native macOS window, file dialogs and Keychain prompts were not exercised in this environment (the UI is verified in a browser against the real backend). Run the manual checklist in [docs/TestPlan.md](docs/TestPlan.md).
6. **Terminal performance on heavy output** was measured in the real macOS WKWebView ([docs/Assessment.md](docs/Assessment.md) G4, `tools/termperf/run.sh`). Text output (plain, log lines, colours, Vietnamese/CJK) at the most the backend sends (about 3.7 MB/s) keeps 55–60 frames per second. A screen that is redrawn in place with every cell coloured (a heavier htop) drops to about 20 frames per second with the default renderer; the WebGL renderer keeps 60 and is now used for the terminal that has the focus (*Settings → Terminal → Fast drawing*, on by default; it falls back to the default renderer by itself). It was measured with the benchmark page, not yet looked at in the packaged app: if text looks wrong (fonts, selection), turn it off and tell us.
7. Sync was tested against local Git remotes only. Edits made during the few seconds of a sync can be overwritten. After a vault reset the sync repository still holds the old vault: use a new repository.
8. Last-write-wins depends on reasonably correct clocks.
9. `ssh_config`: `ProxyCommand`, `Match`, token expansion are reported as warnings and skipped.
10. No CLI (`vvt`). Account sign-in is a placeholder.
11. Windows and Linux builds are made by the release workflow but are much less tested than macOS. Updates install by themselves on macOS with Apple silicon only.
12. Use cases still depend directly on some infrastructure packages (documented debt, capped by a test).
13. Replacing a saved host password creates a new identity; the old one stays in Keychain until you delete it.
14. **New in 1.3.0 and not yet exercised on real hardware:** installing an update by itself (every step is covered by tests, including the real swap script; the first whole update of the packaged app is the one from 1.3.0 to 1.4.0), the `ssh://` registration in the macOS bundle, a verification-code prompt against a real bastion, and a hardware (FIDO2) key through ssh-agent. Run the checklist in [docs/TestPlan.md](docs/TestPlan.md) and tell us what you find.
15. A port forward that starts with the app cannot answer a verification code: it waits three minutes and fails. Start it from the Port Forwarding page. *Run on hosts* never asks anything, so a host with an untrusted key or that wants a code fails there: open it once in a terminal tab first.
16. Session logs are not rotated or deleted, and hold everything that was shown on screen. No tmux integration, no X11 forwarding, no Mosh.
17. **The Kubernetes tab** was run against a real kind cluster for Resources, Apps, the merged logs and the numbers, and is covered by tests with sample answers for the rest; it was not run against Rancher or a managed cloud cluster, and its screens were not clicked through in the app on a real cluster. Port-forward needs a host with `kubectl`; editing a workload needs a cluster opened with a kubeconfig. Apps does not read Helm 2, releases kept in ConfigMaps or SQL, or chart repositories.

## Changes from the original design

| Design | Now | Why |
| --- | --- | --- |
| JSON files, no SQLite in the MVP | SQLite (`vvterm.db`, schema v2) | product requirement ([ADR-0001](docs/CleanArchitecture.md)) |
| Snippets out of scope | Snippets included | requested |
| No SFTP, fixed shortcuts | SFTP browser, configurable shortcuts, General and Account settings | requested |
| `kevinburke/ssh_config` | own parser | needs per-file group names ([ADR-0003](docs/CleanArchitecture.md)) |
| Services in `app/` | `internal/usecase`, Wails glue in `app/` | clean architecture ([ADR-0002](docs/CleanArchitecture.md)) |
| Svelte or React | React + TypeScript | open question resolved |
| Touch ID | keychain unlock without biometric enforcement | needs a native bridge |
| "Lost password: nothing to do" | reset vault (keeps hosts, erases secrets) | requested |

## Upgrading

Open the new build: the database migrates itself (v1 → v2) on first start. Back up first with **Import / Export → Encrypted backup** if the data matters. Data lives in `~/Library/Application Support/vvterm/` (override with `VVTERM_DATA_DIR`). **If you forget the master password the vault cannot be recovered** (you can reset it and keep your hosts).

## How to verify this release

```bash
make test        # all unit, integration and component tests
make e2e         # walkthrough against the real backend; refreshes docs/screenshots
make build       # build/bin/vutty.app
scripts/deploy.sh prod --dry-run   # the release pipeline without side effects
```

---

# VVTerm 1.0.0

First release (2026-10-28): encrypted vault (Argon2id + AES-256-GCM), hosts with groups/tags/environments, jump host chains, tabs and split panes, strict host key checking (changed keys are blocked with no override), local/remote/SOCKS5 forwarding, `~/.ssh/config` import/export, plain JSON and encrypted backups, Git sync with per-record merge, SQLite storage and a clean-architecture codebase with an enforced dependency rule.

Defects found and fixed while building 1.0.0 (each has a regression test): config-file injection through newline characters in hostnames; a sync commit on every run caused by random encryption nonces; early output and host key prompts lost before the UI subscribed; a white screen after importing because Go sent `null` for empty lists; untrusted KDF parameters that could crash or exhaust memory.

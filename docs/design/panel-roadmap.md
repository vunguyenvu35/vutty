# Panel roadmap

Status: items 1–11 are built (see the notes in [RELEASE_NOTES.md](../../RELEASE_NOTES.md)); 12 is covered by Snippets; 13 is on hold, to be watched. Ideas for the next steps of **Panel**, the server dashboard that opens in a tab ([User guide](../user-guide/08-panel.md)). They are aimed at solo developers who run one or a few VPSs by hand.

These are hypotheses, not measured demand. Before building, confirm them with a few VPS users or by counting which Panel tabs are opened most.

## Principles to keep

- **Nothing installed on the server.** Everything goes over the existing SSH connection.
- **Fixed commands only.** No free-form command line. A user-defined action (see Runbooks) is the one exception and is always shown and confirmed before it runs.
- **Every change asks first.** Read-only views stay read-only.

## Who it is for

- Solo developers, freelancers and indie hackers who manage a VPS (one app, Docker Compose, a reverse proxy) and are not full-time sysadmins. The main audience.
- Developers who handle incidents on staging or as on-call and want logs and service state without recalling commands.
- Agency staff who work on client servers where installing monitoring is not allowed.
- People new to Linux, who learn service, port and process concepts with a confirmation step as a safety net.

Professional sysadmins and SREs already have monitoring (Grafana, Datadog) and use Panel only for a quick look.

## Priority 1: the incidents VPS owners hit most

| # | Feature | What it does | Mode |
|---|---------|--------------|------|
| 1 | Certificates | Expiry of Let's Encrypt certificates (`certbot certificates` or `openssl`), a warning under 14 days | read only |
| 2 | Disk and resource history | CPU, memory and disk sampled while the tab is open, as a short chart; a warning when a disk is nearly full | read only |
| 3 | Nginx / Caddy | Sites and upstreams, `nginx -t`, reload only after a passing test, error log | confirm on reload |
| 4 | Docker Compose by project | Group containers by stack (`docker compose ls/ps`); pull, up -d, restart and logs for a whole stack | confirm on change |
| 5 | Live logs | *Follow* re-reads the log every 2 seconds (not a true `-f` stream), with a level filter and search | read only |

Suggested order: 1, 2, 3, 4. Expired certificates and full disks are the common silent outages, and Nginx and Compose are the common stack.

## Priority 2

| # | Feature | What it does | Mode |
|---|---------|--------------|------|
| 6 | Safe cleanup | Size by folder (`du`), unused Docker images and volumes, journald size; clean after showing the space that will be freed | confirm |
| 7 | System updates | Number of pending packages, security updates, whether a reboot is required | read only |
| 8 | Fail2ban and logins | Banned IPs, failed SSH logins (`lastb`), unban; basic checks (root login, password auth, unexpected open ports) | confirm on unban |
| 9 | Cron and timers | Cron jobs and systemd timers with last run and result; run one now | confirm on run |
| 10 | Backup check | The user names a path or a command; Panel shows how long ago the last backup was | read only |

## Priority 3: experience

| # | Feature | What it does |
|---|---------|--------------|
| 11 | Fleet overview | One screen for all saved hosts, one row each (up or down, load, disk, failed services). Most useful for people with several VPSs |
| 12 | Runbooks | Not built: Snippets already cover saved commands |
| 13 | AI assistant (on hold, observing) | Ask "why did this service crash?" from Panel with the relevant log in context, subject to the redaction rules in [AIAssistant.md](ai-assistant.md) |

## Open questions

- **Looking back (built):** the Logs tab reads a time range or the previous boot, and the Incident tab reads what the journal and Docker kept, so a night can be reconstructed the next day without anything installed. What is still missing is CPU, memory and disk history for a time the app was closed: either read `sar` where the host has it, or an opt-in collector on the host.
- **Alerts need something running.** History and alerts while the app is closed require a process on the server (which breaks "nothing installed") or only work while Vutty is open on the desktop. Decide which before building 2 beyond a live chart.
- **Linux only.** Panel assumes Linux with systemd and, for Docker, the Docker CLI. Other init systems and distros are not covered.
- **Runbooks vs fixed commands.** Runbooks (12) are the one place where the user supplies the command. Decide how they are stored, whether they sync, and how they are shown in the confirmation.
- **Privacy of logs.** Items 5 and 13 move log text into the UI and possibly to an AI provider; apply the same redaction as the assistant.

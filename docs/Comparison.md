# Vutty compared with Termius

This page is for someone choosing an SSH client. Vutty's column comes from this repository (code, tests and the release notes). Termius's column comes from its own pages, **read on 5 November 2026** (linked at the end). Where those pages did not say, the cell says "not stated". Nothing here comes from memory.

## At a glance

Vutty is a free, open source SSH client that keeps everything on your computer, and it goes beyond SSH: the same app can open a dashboard of a Linux server (Docker, systemd, web, security, schedules) and manage Kubernetes clusters. Termius is a polished SSH client for every platform, including phones, with cloud sync and team features behind paid plans.

If you need **mobile apps, a hosted cloud sync, or shared team vaults**, Termius has them and Vutty does not. If you want **one free app for your terminals, your files, your servers and your clusters**, with nothing to sign up for, that is Vutty.

## The terminal and the connection

| | Vutty | Termius |
| --- | --- | --- |
| Systems | macOS, Windows, Linux | macOS, Windows, Linux, iOS, Android |
| SSH, SFTP | Yes | Yes (Starter, the free plan, lists "SSH and SFTP") |
| Telnet, serial, a shell on this computer | Yes (on Windows the shell on this computer is not available yet) | Listed on the pricing table ("SSH, SFTP, Telnet, Mosh, Local terminal", and Serial); the plan of each row is not stated |
| Mosh | No | Listed on the pricing table |
| Jump hosts | Yes, chains of them | Listed ("Jump host / Host chain") |
| Port forwarding | Local, remote, SOCKS5 | Listed; "Port Forwarding" is in Starter |
| Agent forwarding | Yes, per host | Listed |
| SOCKS and HTTP proxy | SOCKS5 forwarding; no HTTP proxy | Listed ("SOCKS & HTTP proxy") |
| Keys | Generate, import, OpenSSH user certificates imported with the key; no FIDO2 keys | Lists SSH certificates and FIDO2 |
| Unlock with biometrics | Touch ID on a Mac (a guarded Keychain item needs a signed build) | Listed ("TouchID, FaceID, Biometric keys, PIN-lock") |
| Tabs, split panes, new windows | Yes, sessions keep running when a tab is dragged | "Workspaces" are listed; not described |
| Autocomplete | *Suggest commands* from your history, the shell history of the host and your snippets (no AI) | "AI-powered autocomplete" in Starter |
| Snippets | Yes, with blanks that are asked for; run on several hosts at once with the result and exit code of each, a timeout and Cancel | Snippets, "Snippet multi-execution", "Startup snippets" listed; "Snippets automation" in Pro |
| Session logs | A text file per session, off, production hosts only, or all; 50 MB each; kept on your computer | "Session logs" in Pro; shared logs and retention policies in the team plans |
| AI | Optional assistant through the official CLI you already use (Claude Code, Codex, Gemini); the screen is sent with every secret replaced, it only suggests commands | AI autocomplete; no other AI feature on the pages read |
| Languages | English, Vietnamese, Japanese, Chinese | not stated |

## Your data and your cost

| | Vutty | Termius |
| --- | --- | --- |
| Price | Free, no tiers | Starter free, "available for commercial usage"; Pro $10 a month; Team $20 and Business $30 per seat a month (all "when paid annually"); Enterprise by quote |
| Source code | Open (MIT) | not stated on the pages read |
| Account | None. No telemetry | The pages do not say whether the free plan needs one ("Free to try. No credit card required") |
| Where hosts and keys live | An encrypted vault on your computer (AES-256-GCM, key from your master password) | A local vault in Starter; a personal cloud vault in Pro |
| Sync between computers | Your own private Git repository or cloud folder, encrypted, free | "Sync across mobile and desktop" in Pro, through their cloud |
| Backups, import, export | Encrypted backups, `~/.ssh/config` in and out, hosts from a listing made with `aws`, `gcloud` or `ansible-inventory` (or CSV); Vutty never contacts a cloud | Lists integrations with "AWS, DigitalOcean, Azure", an "API bridge" and Ansible |
| Changed server key | The connection is blocked with no override | not stated |
| Production hosts | A red frame, a PROD badge, and a question before a paste with a line break or a snippet | not stated |

## Teams and companies

Termius has real-time collaboration, a team vault (Team), several team vaults with fine access control (Business), SAML single sign-on (an add-on on Business, included in Enterprise), a SOC 2 Type II report, an SLA and priority support. **Vutty has none of these**: it is made for one person, or a few people who share a Git repository on their own.

## Where Vutty goes further

The pages of Termius that were read do not mention Docker, Kubernetes or server dashboards. (The pricing table was cut off at the bottom when it was read, so this is not a complete check.) In Vutty these are part of the app:

- **Panel: a dashboard of a Linux server, over the SSH login you already have, installing nothing.** Overview with half an hour of CPU, memory, disk and network history, Processes, systemd Services, Docker containers and Compose stacks, Logs with Follow, Ports and firewall, Web (certificates and the sites of nginx or Caddy), Maintenance (updates, disks, a cleanup that says what it frees), Security (failed logins, fail2ban), Schedule (timers, cron, a backup check) and **Fleet**, which lists every saved host at once. Every change asks first.
- **Kubernetes**, from a host that has `kubectl` or from a **kubeconfig with no server at all**: Overview, Pods, Workloads, Network, Config and storage (Secrets by name only), Resources (any kind, CRDs included), Apps (Helm releases), Nodes and Events; a shell in a pod, logs of all the pods of a workload merged by time, Scale, Restart, Roll back, Drain, edit a workload with a preview; the files inside a pod (read only); buttons locked, with the reason, when `kubectl auth can-i` says the login may not. See [KubernetesPanel.md](KubernetesPanel.md).
- **An audit log** of what was changed (never the output), **SFTP with a queue that resumes**, folder sync that never deletes, and edit-and-save-back.

## Where Vutty is behind

- **No mobile apps** (Termius has iOS and Android).
- **No hosted sync and no team features**: sync needs your own Git repository or cloud folder, and there are no shared vaults or single sign-on.
- **No Mosh, no FIDO2 keys, no HTTP proxy**, and X11 forwarding is not done yet.
- **The Windows and Linux builds are new** and less tested than macOS, and builds are not yet signed or notarized (see [Installing on macOS](InstallMac.md)).
- **What was and was not run for real:** the Kubernetes tab was run against a real kind cluster for its newest parts (resources, Helm, merged logs, files in a pod); the rest is covered by tests with sample answers, and none of it has been run against Rancher or a cloud-managed cluster yet. See the known issues in the [release notes](../RELEASE_NOTES.md).

## Tabby and WindTerm

Checked on 2 November 2026 (Termius left out: see above).

| | Vutty | Tabby | WindTerm |
| --- | --- | --- | --- |
| Price | Free | Free | Free for commercial and non-commercial use |
| Source code | Open (MIT) | Open (MIT) | Partly open: released source is Apache-2.0, "the source will be gradually opened" |
| Systems | macOS, Windows, Linux | Windows, macOS, Linux | Windows, macOS, Linux |
| SSH and SFTP | Yes | SSH yes; SFTP not listed in the README (a third-party plugin exists) | Yes (SSH, integrated SFTP and SCP client) |
| Port forwarding | Local, remote, SOCKS5 | Yes (with X11 forwarding) | Local, remote, dynamic |
| Serial and Telnet | Yes | Yes | Yes |
| X11 forwarding | Not yet | Yes | Yes |
| tmux integration | No | not stated | Yes |
| Encrypted store for secrets | Yes | Yes ("encrypted container for SSH secrets") | not stated |
| Sync between computers | Your own Git repository or cloud folder; encrypted | Third-party plugin only (Gist or Gitee) | not stated |

## Sources

- Termius: <https://termius.com/pricing> (plans, and the table of features by plan, whose per-plan marks could not be read), <https://termius.com/enterprise>, <https://termius.com> (read 5 November 2026; `/features` does not exist)
- Tabby: <https://github.com/Eugeny/tabby>
- WindTerm: <https://github.com/kingToolbox/WindTerm>
- Vutty: this repository, [RELEASE_NOTES.md](../RELEASE_NOTES.md), [KubernetesPanel.md](KubernetesPanel.md), [PanelRoadmap.md](PanelRoadmap.md)

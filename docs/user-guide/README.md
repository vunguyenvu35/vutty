<p align="center"><img src="../assets/brand/logo-mark-256.png" alt="Vutty" width="88"></p>

# Vutty user guide

A step-by-step guide to every screen of Vutty, from creating your vault to connecting, transferring files, running a server and getting help. The screenshots show the app as it looks today.

> New here? Start with [Getting started](01-getting-started.md): five steps from install to your first terminal.

## Chapters

| # | Chapter | What it covers |
| --- | --- | --- |
| 1 | [Getting started](01-getting-started.md) | Install, create the vault, the main window, your first connection |
| 2 | [Keychain](02-keychain.md) | SSH keys, passwords, ssh-agent and hardware keys |
| 3 | [Hosts, groups and import](03-hosts.md) | Add and test a host, groups, search, quick connect, import from ssh config, cloud and Ansible, export |
| 4 | [Connecting](04-connecting.md) | Protocols (SSH, Mosh, Telnet, FTP, RDP, VNC), sign-in codes, production hosts, `ssh://` links |
| 5 | [The terminal](05-terminal.md) | Tabs, split panes, sidebar, snippets, Run on hosts, command suggestions |
| 6 | [Files: SFTP and FTP](06-files.md) | Browser, transfer queue, sync folders, edit a remote file, FTP and FTPS |
| 7 | [Port forwarding](07-port-forwarding.md) | Local, remote and dynamic forwards |
| 8 | [Panel](08-panel.md) | A server dashboard: monitor, logs, incidents, Docker, web, firewall, users, updates, Fleet |
| 9 | [Kubernetes](09-kubernetes.md) | Clusters through a host or a kubeconfig; look, shell, edit, drain |
| 10 | [AI assistant](10-ai-assistant.md) | Ask about a terminal, with secrets removed first; MCP servers |
| 11 | [Settings and security](11-settings-and-security.md) | Every Settings page, auto-lock, Touch ID, updates, a forgotten master password |
| 12 | [Sync and backup](12-sync-and-backup.md) | Git or cloud-folder sync, what to sync, encrypted backup |
| 13 | [Troubleshooting and reference](13-troubleshooting.md) | Common messages, Known Hosts, Logs, where your data lives |
| 14 | [Keyboard shortcuts](14-shortcuts.md) | Every default key |

## I want to…

| Task | Go to |
| --- | --- |
| Connect to a server for the first time | [Getting started](01-getting-started.md#your-first-connection-in-five-steps) |
| Log in with a key instead of a password | [Keychain](02-keychain.md) |
| Bring my hosts from `~/.ssh/config`, AWS or Ansible | [Import](03-hosts.md#import-and-export) |
| Keep a session alive on a bad network | [Mosh](04-connecting.md#mosh) |
| Open a remote desktop | [VNC](04-connecting.md#vnc) or [RDP](04-connecting.md#rdp) |
| Run one command on many servers | [Run on hosts](05-terminal.md#run-on-hosts) |
| Upload or sync files | [Files](06-files.md) |
| See what happened on a server last night | [Incident](08-panel.md#incident) |
| Renew a certificate or install updates on several servers | [Fleet](08-panel.md#fleet-every-host-on-one-page) |
| Manage a Kubernetes cluster from a kubeconfig | [Kubernetes](09-kubernetes.md#a-cluster-from-a-kubeconfig) |
| Ask an AI about an error on the screen | [AI assistant](10-ai-assistant.md) |
| Use my hosts on a second Mac | [Sync](12-sync-and-backup.md) |
| Recover after forgetting the master password | [Forgot your master password](11-settings-and-security.md#forgot-your-master-password) |

## Conventions

- **Bold** names a button or a menu entry as it appears on screen.
- ⌘ on a Mac is Ctrl on Windows and Linux.
- Paths such as `~/Library/Application Support/vutty/` are the macOS ones; [Where your data lives](13-troubleshooting.md#where-your-data-lives) has the rest.
- Features marked "not yet tried" in a chapter are covered by tests but have not been run against a real server of that kind: try them on a throwaway host first.

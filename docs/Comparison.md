# Vutty compared with Termius, Tabby and WindTerm

A short table, limited to what could be checked. Vutty's column comes from this repository; the others come from each project's own pages, read on **2 November 2026** (linked below). Where a page did not say, the cell says **not stated**, not "no". Products change: check the linked pages before you decide.

| | Vutty | Termius | Tabby | WindTerm |
| --- | --- | --- | --- | --- |
| Price | Free | Free *Starter* plan; paid plans from $10 / month (billed yearly) | Free | Free for commercial and non-commercial use |
| Source code | Open (MIT) | not stated on the pages read | Open (MIT) | Partly open: released source is Apache-2.0, "the source will be gradually opened" |
| Systems | macOS, Windows, Linux | macOS, Windows, Linux, iOS, Android | Windows, macOS, Linux | Windows, macOS, Linux |
| SSH and SFTP | Yes | Yes, in the free plan | SSH yes; SFTP not listed in the README (a third-party plugin exists) | Yes (SSH, integrated SFTP and SCP client) |
| Port forwarding | Local, remote, SOCKS5 | Yes, in the free plan | Yes (with X11 forwarding) | Local, remote, dynamic |
| Serial and Telnet | Yes | Listed on the pricing page; plan not stated | Yes | Yes |
| X11 forwarding | Coming soon | not stated | Yes | Yes |
| tmux integration | No | not stated | not stated | Yes |
| Mosh | No (shown as unsupported) | Listed on the pricing page; plan not stated | not stated | not stated |
| Encrypted store for secrets | Yes (AES-256-GCM, key from your master password) | Local vault in the free plan | Yes ("encrypted container for SSH secrets") | not stated |
| Sync between computers | Your own Git repository or cloud folder; encrypted | Personal cloud vault and sync are in the paid *Pro* plan | Third-party plugin only (Gist or Gitee) | not stated |
| Account needed | No | Sign-up pages are linked from the pricing page; whether the free plan needs one is not stated | not stated | not stated |

## Where Vutty is behind

- No tmux integration or Mosh yet (WindTerm has tmux). X11 forwarding is coming soon (Tabby and WindTerm have it).
- No mobile apps (Termius has them).
- The Windows and Linux builds are new and not signed; macOS is the most tested. See [Installing on macOS](InstallMac.md) for the macOS warning.

## Where Vutty is different

- **Sync you control, without paying for it:** a private Git repository or a cloud folder you own. Only encrypted files leave your computer.
- **SFTP with a resumable queue**, folder sync that never deletes, and edit-and-save-back.
- **An AI assistant that only sees redacted screens** and only suggests commands (optional, off until you turn it on).

## Sources

- Termius: <https://termius.com/pricing>, <https://termius.com/download>
- Tabby: <https://github.com/Eugeny/tabby>
- WindTerm: <https://github.com/kingToolbox/WindTerm>

# Release signing and self-update

Vutty can install a newer version of itself (**Help → About → Check for updates → Install and restart**). It does not need an Apple Developer certificate for that: what makes an update safe is a signature made with a key that only the maintainer holds. This page is for the person who publishes releases.

## What the app checks

1. It reads the latest release from GitHub and only goes on if its version is higher than the running one.
2. It downloads `SHA256SUMS` (the checksum list of the release) and `SHA256SUMS.sig` (its signature) and checks the signature against the **public key built into the app** (`internal/updater/pubkey.go`). Without a valid signature nothing else is downloaded.
3. It downloads `vutty-<version>-macos.zip`, hashing it as it arrives, and compares the SHA-256 with the line the signed list gives for that name. The version in that name must be the version of the release.
4. It unpacks the zip with `ditto`, checks that the result is this application (same bundle identifier, a program that can run, a `codesign --verify` that passes), puts it next to the running copy and, after Vutty quits, swaps it in and opens it. The old copy is kept as `vutty.app.previous` until the new one has run for 30 seconds.

Everything is fetched over HTTPS from GitHub only (a redirect to another host stops the update), the downloads have size limits, and nothing in the running application is touched before every check has passed.

Why this works without Apple: Gatekeeper's check applies to files that carry the *quarantine* flag, which browsers add to what they download. A file that the app fetches itself does not have it. Only the very first install, which comes from a browser, needs the *Open Anyway* step in [Installing on macOS](InstallMac.md).

## Setting it up once

```sh
go run ./tools/updatesign keygen ~/.config/vutty/update-signing.key
```

It writes the private key (mode 0600, never overwrites a key) and prints the public key. Put the public key in `internal/updater/pubkey.go` and commit that file; **never commit the private key**, and keep a backup of it (a password manager is a good place): see *If the key is lost* below.

For releases built by GitHub Actions, add the content of the key file as a repository secret named `UPDATE_SIGNING_KEY` (Settings → Secrets and variables → Actions). The `release` workflow then signs `SHA256SUMS` itself and checks the signature before it publishes. Without the secret the release is still published, but unsigned: installed copies then offer the release page instead of *Install and restart*.

## For each release

Push the tag as usual; the workflow builds, signs and publishes. To sign a release that is already published (one made by hand, or before the secret was added):

```sh
make sign-release TAG=v1.3.0        # downloads SHA256SUMS, signs it, checks it, uploads SHA256SUMS.sig
```

To check a signature by hand: `go run ./tools/updatesign verify <public key> SHA256SUMS` (the `.sig` file must be next to it).

## What this protects against, and what it does not

- A **tampered download**, a **compromised mirror or network**, or **somebody who gains write access to the GitHub release**: they cannot produce a valid signature, so the app refuses.
- It does **not** protect against **theft of the private key**: whoever has it can sign an update that installs on every copy. Keep it off shared machines; if it leaks, treat it as lost.
- It cannot stop **a rollback to an older release** that was really signed, other than by never installing a version that is not higher than the running one, and by binding the version to the signed file name.
- The first install, and every copy made before the key was added to the app (1.2.1 and older), has to be installed by hand from the release page once.
- Self-update is available on **macOS with Apple silicon** only. Windows and Linux copies are told to download from the release page.
- A copy that is not in a folder the user can write to (running from the disk image, or from the temporary place macOS uses for a freshly downloaded app) says so and offers the release page.

## If the key is lost, or has leaked

Installed copies trust only the key they carry, so a new key cannot be rolled out by an update. Make a new key, put its public half in `internal/updater/pubkey.go`, release a version built with it, and tell users to install that one by hand from the release page. From then on updates are signed with the new key.

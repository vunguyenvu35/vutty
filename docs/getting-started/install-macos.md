# Installing on macOS: opening a beta that is not notarized

You only have to do this **once**, for the first install. After that, newer versions install from inside Vutty (**Help → About → Check for updates → Install and restart**): the app downloads the update itself, checks its signature and checksum, and restarts, and macOS does not ask again (see [Release signing](../development/release-signing.md)). It needs Vutty to be in your Applications folder, not running from the disk image.

Vutty betas are not signed with an Apple Developer ID and not notarized yet, so macOS **Gatekeeper** blocks the first launch with a message such as *"vutty" can't be opened because Apple cannot check it for malicious software.* Nothing is wrong with the download; Apple simply has not seen this build. You only do this once.

## 1. Check your download (recommended)

Each release lists `SHA256SUMS`. In Terminal, in the folder you downloaded to:

```sh
shasum -a 256 vutty-<version>-macos.dmg
```

The result must match the line for that file in `SHA256SUMS` on the [Releases](https://github.com/vunguyenvu35/vutty/releases/latest) page.

## 2. Install

Open the DMG and drag **vutty.app** to **Applications** (or unzip the `.zip` and move the app there).

## 3. Open it once

**Option A: System Settings (works on every recent macOS)**

1. Open **vutty** from Applications. macOS blocks it; click **Done** (or **OK**).
2. Open **System Settings → Privacy & Security** and scroll down to **Security**.
3. Next to *"vutty" was blocked…* click **Open Anyway**, then confirm with your password or Touch ID.
4. Open vutty again and choose **Open**.

**Option B: right-click (older macOS only)**

Right-click **vutty.app → Open → Open**. Recent macOS versions (Sequoia and later) removed this shortcut; if no **Open** button appears, use Option A.

**Option C: Terminal**

This removes the "downloaded from the internet" mark from the app:

```sh
xattr -dr com.apple.quarantine /Applications/vutty.app
```

After any of these, the app opens normally from then on, including after you replace it with a newer version of the same kind.

## Windows

SmartScreen may warn on the `.exe`: choose **More info → Run anyway**.

## Why this is needed, and when it will go away

An Apple Developer ID (paid) is needed to sign and notarize. The release pipeline already has the hooks for it (see the [Development guide](../development/development.md)); until a certificate is added, betas show this warning. Only open Vutty files you downloaded from the official [Releases](https://github.com/vunguyenvu35/vutty/releases) page and checked against `SHA256SUMS`.

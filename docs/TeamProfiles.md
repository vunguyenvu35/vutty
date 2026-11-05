# Profiles: Personal and Team

Status: **design (revision 6), nothing built yet.** It is not decided to build M2 onward; see [the assessment](#is-it-worth-building-assessment-for-thinking-over).

Decisions taken:

- The typical case: a small DevOps team (2 or 3 **admins**) gives access to about **20 developers**, who are **members who can only connect**. They cannot edit hosts or change the team.
- The **admin creates the private Git repository**. Git is the shared database and **everything in it is encrypted on each client**.
- The repository holds a **shared host list** (per space) and, for each member, **a file with that person's own logins** that only that person (and the admins) can open. **Nobody shares an account or a password.**
- **Each member has their own account on each server.** The admin issues it through Panel in one action, and **removes it in one action** that cuts access on the servers first (§5).
- A login can be a **password** or a **key**. Key login is encouraged, not required: password-only servers work, because the password is the member's own.
- **Members have read-only access** to the repository on the Git host; only admins can write. This is a sharing model from the admin: members do not write.
- Kubernetes credentials per person are left for later (§5).

## Goal

Let a small team share hosts, groups, snippets and port forwards through **their own private Git repository**, with no server of ours, where **each person gets their own way in** and the admin can add and remove people from one place. A person has one **Personal** profile and any number of **Team** profiles.

What this gives a team: a shared host directory, with each member seeing only the spaces they were given, and their own account and credential on each server, created and removed from Panel. What it does not give: SOC 2, an SLA, SAML on our side. See [Limitations](#limitations-to-tell-the-user) and [Non-goals](#non-goals).

## Rules that keep the beta safe

- **Personal does not change.** Same folder, same `bundle.enc`, same vault. No migration.
- Everything new applies to Team profiles only and sits behind *Settings → Team profiles (experimental)*.
- Each step ships on its own and says in the release notes where it was tried.
- A team repository records its format and the **oldest client allowed to write** to it (§3), so an old beta cannot damage a repository that a newer one already upgraded.

## 1. A profile is a data folder and a process (result of the M0 spike)

What the code does today:

- `app.Build(dir)` makes one database, one vault and one `Core` from one folder; the Wails bindings (`a.Host`, `a.Vault`, …) are fixed pointers handed to `wails.Run`. `Core` also owns timers, sessions, the sleep lock, the emitter and the bridge.
- A second window is already **a second process** that talks to the first through a loopback bridge whose connection file (`settings-ipc.json`) lives **in the data folder**. `DataDir()` already honours `$VUTTY_DATA_DIR`.
- Wails is not configured with `SingleInstanceLock`.

Swapping the `Core` under live bindings would be a large and risky change, while a profile as its own process reuses what exists:

```
<config>/vutty/                 Personal (unchanged)
<config>/vutty/profiles.json    [{id, name, kind: personal|team, dir}]
<config>/vutty/profiles/<id>/   one Team profile: data/, sync/, its own vault, bridge file, keychain account
```

![Personal and Team profiles as two processes on one machine](diagrams/01-profile-processes.svg)

*Figure 1. Two processes, no shared memory or vault. Only the Team process talks to the team repository.*

- *Open profile…* starts `vutty --profile <id>`, which sets the data folder and runs the normal `Build`. If that profile's process already answers on its bridge, the existing window is shown instead (new bridge action `show`).
- A **lock file** per profile folder keeps two processes off the same SQLite file. `profiles.json` is written under the same kind of lock.
- Only the Personal process does self-update and handles `ssh://` links; Team processes skip both.
- Costs we accept: two windows (Personal and a Team), no single sidebar, auto-lock and sleep-lock per process, no drag of a host between profiles. In return no vault of one profile is ever readable from the process of another. Several **spaces of one team share one window**.

### Moving data between profiles

The processes do not share memory, so copying hosts between Personal and a Team profile goes through what already exists: **export the selected hosts** and **import** them in the other profile. A *Copy to another profile…* button wraps this. Nothing is moved automatically. A member cannot copy *into* the team: the team is read-only for them.

### Leaving a team

*Leave team* deletes the local profile folder and its keychain entry. It does not change the repository or the servers. The admin removes the person (§5).

## 2. Roles and the trust model

| | Admin | Member |
| --- | --- | --- |
| Repository on the Git host | write | **read only** |
| Opens which host lists | all (an admin is always a recipient) | only the spaces they were added to |
| Opens which login files | all (the admin issued them) | **only their own** |
| Edits hosts, spaces, members | yes | no |
| Has an account on the servers | yes, their own | yes, their own, made by an admin |

- **Write control is the Git host's.** A member's client never pushes, and a push would be refused anyway.
- **Read control is cryptographic.** A member who can read the repository still cannot open a space or a login file that is not theirs.
- **Authenticity is the admins' signature** (§3). A write permission given by mistake, a leaked write token or a compromised Git host account cannot slip in a forged host list or put an old one back.
- A team has two or three admins. An admin who loses their key is replaced by another admin.
- An admin knows the logins they issued, because they set them. A member who generates their own key and sends only the public key gives the admin nothing secret.

## 3. Repository format

Personal keeps `bundle.enc`. A Team repository has:

```
team.json                format, min_client, team id, version,
                         admins [{id, public key}],
                         members [{id, name, public key}],
                         spaces [{id, recipients: [member ids]}],
                         SHA-256 of every file below, signature (by an admin)
spaces/<id>.enc          the host list of one space, age-encrypted to its recipients
members/<id>.enc         one person's own logins, age-encrypted to that person and the admins
```

![The repository files and who can open them](diagrams/02-repository-files.svg)

*Figure 2. A space file is encrypted to the members of that space; a member file is encrypted to that one member and the admins.*

- **A space** is the unit of sharing: for example `prod`, `staging`, `client-acme`. Its file holds the name, groups, hosts, snippets, port forwards, clusters (address and certificate authority only), the **host keys the admin has verified**, and the list of accounts the admin created on its servers (§5). It holds **no login**.
- **A member file** holds, for each server that person may use: the account name, and the credential: a **password**, a **private key the admin generated**, or a note that the person uses **their own key** (nothing secret, the admin only installed the public key). It also holds when it was issued and when it should be rotated.
- **Encryption** uses [`age`](https://github.com/FiloSottile/age) with the recipients' **SSH public keys** (`ssh-ed25519`; `ssh-rsa` also works for imported keys). `age` puts one entry per recipient in a file's header and encrypts the data with a fresh key each time, so there is no separate key to store: **re-encrypting a file to a new recipient list is the rotation.**
- Each member has a **repository key** (an ed25519 key) used **only** to open these files. It is separate from the credential they log in to servers with.
- **Names are inside the files**, not in `team.json`, so a leaked repository does not show client or project names.
- **A member's client** reads `team.json`, verifies it (below), decrypts the host lists of its spaces and **its own login file**, and replaces its read-only local copy. It never writes to the repository and never touches a file it is not a recipient of.
- **An admin's client** does the same, and on a change merges, re-encrypts the changed files, signs and pushes.
- A member file changes only when that person's logins change, so a sync rewrites little.
- The branch is configurable (default `main`). A protected branch that refuses direct pushes does not work with this design; the setup screen says so.

![Data flow of one sync](diagrams/03-sync-data-flow.svg)

*Figure 3. One sync. A member only reads; an admin also merges, re-encrypts, signs and pushes. A file the client has no key for stays hidden and untouched.*

### Admin signature: who really wrote this

`age` keeps a file secret and detects damage, but it does **not** say who made it: the recipients' public keys are public, so anyone who can write to the repository can make a valid file for the same recipients. Without a signature, a leaked write token, a write permission given by mistake or a compromised Git host account could replace a space with a host list that points at a server of the attacker's, or put an older commit back to bring a removed person back.

So the team is **signed**:

- `team.json` is a manifest. It lists the admins' public keys, the members, the recipients of each space, the **SHA-256 of every file** and a `version` that goes up on every change. An admin signs it (Ed25519, with their SSH key).
- **A member's client checks before it decrypts anything:** the signature is by a pinned admin key, each file matches its recorded hash, and `version` is not lower than the highest one this machine has seen. Otherwise it keeps what it had, shows a red warning and opens nothing new.
- **Pinning.** When a person joins, the app shows the fingerprints of the team's admin keys and asks them to compare with an admin (by voice or chat), as for an SSH host key. After that the client trusts only those keys.
- **Changing the admins.** A new admin is added by a manifest signed by an existing admin, and an admin is removed the same way. A client follows the chain from the keys it pinned.
- **Rollback.** An old signed manifest has a lower `version`, so putting an older commit back is refused.
- The signature covers the whole repository state, so a change to a file and the matching manifest are one commit.

What it protects against: a forged or replaced file, a forged member or recipient list, and a rollback. What it does not: an attacker who holds an **admin's key** (that admin is removed, §5), and an attacker who deletes or corrupts files (the client detects it and keeps its last good copy; Git restores the files).

### Format and client versions

`team.json` carries `format` and `min_client`. A client older than `min_client` opens the team **read-only** and says why; a client never writes a format newer than it understands. Most people only read, so a member on an older version is rarely affected.

### Revisions and conflicts (between admins)

Members never write, so conflicts can only happen between the two or three admins. A member's sync is a replace, so it needs no merge and no tombstones.

`Meta` gains `Rev` (a hybrid logical clock: wall time, counter, admin label) and `UpdatedBy`. `UpdatedAt` stays for compatibility and display. Each admin machine keeps, per record, the `rev` it last synced (`base_rev`). On a sync:

- only the remote side moved since `base_rev` → take it;
- only the local side moved → push it;
- **both moved** → keep the one with the higher `Rev`, and put the other in a **Conflicts** list (who, when, what) that can be restored with one click. Nothing is overwritten silently;
- a tombstone beats an edit only when its `Rev` is higher. Tombstones are kept: with two or three writers they stay small;
- a machine that has just cloned has no `base_rev`: the first sync takes the remote as its base.

A `Rev` whose wall time is more than a day ahead of this machine's clock is **refused** and reported.

### Where the keys live on a machine

The Team profile has its own **local vault** (the existing `vault` package, unlocked with a password the person sets for their own machine, also with Touch ID or Windows Hello). It holds the person's repository key. After a sync it also holds that person's decrypted logins, so Vutty can connect without showing them in the interface.

## 4. Members: joining

![Adding a member](diagrams/04-add-a-member.svg)

*Figure 4. The member sends a public key; in Panel the admin creates the account on each server and writes the member's own login file; the member fetches, decrypts and logs in with their own account.*

1. The admin gives the person **read access** to the repository on the Git host (the first gate; SSO and the team's own rules apply there).
2. In Vutty the person chooses *Join a team* with the repository URL. The app makes a repository key (or imports one) and shows its **public key and fingerprint**.
3. They send the public key to an admin over any channel. It is not a secret.
4. In Panel the admin chooses **Add member**, picks the spaces and approves after comparing the fingerprint with the person. Panel then, for each server of those spaces:
   - **creates the person's account** (their own, named for them), with the permissions the admin chose;
   - **sets the credential**: generates a password or a key, or installs the person's own public key if they prefer to use theirs;
   - records the account in the space.
5. The app writes **`members/<id>.enc`** with the person's logins, adds the person to the recipients of their spaces, updates `team.json`, signs and pushes.
6. The member syncs, finds their spaces and their login file, and connects. They never asked anyone for a password and nobody else can read theirs.

*Shortcut, an option for M3:* the admin adds a member by their **Git host username** and Vutty reads their public SSH keys from the host (GitHub and GitLab publish them at `/<user>.keys`). There is no key to send and no fingerprint to compare, and the Git host has already confirmed who the person is. Only `ssh-ed25519` and `ssh-rsa` keys can be used, and the developer must make the matching private key available to Vutty in their Team profile.

## 5. Servers: accounts, logins and removal

### Which account on the server

A login gives a person a way in, but it also says **who they become** on the server. That is the *Unix account* (also called the login user or the account name): the name in `ssh alice@server`. Each account has its own home folder, files, groups and permissions (for example whether it may use `sudo`).

**Every member has their own account on each server.** There is no shared account in a Team profile, so:

- the server knows who did what (history, logs and `sudo` records are per person);
- what a person can do is what **their** account is allowed, which the admin sets when making it;
- removing one person touches only their account.

### What a login can be

| Credential | What the admin does | What is delivered | Notes |
| --- | --- | --- | --- |
| **Password** | Panel sets a generated password on the person's account | The password, in the person's login file | Works on any server. Still a password: it can be copied or phished and guessed over the network |
| **A key the admin generates** | Panel makes a key pair, installs the public key | The private key, in the person's login file | The admin has seen the private key |
| **The person's own key** (recommended) | Panel installs the person's public key | Nothing secret | The admin never sees a secret. Needs key login on the server, and lets the server turn password login off |

The member never types or copies a password: Vutty reads it from the login file and connects. They can still read their own login in the app if the admin allows it.

### Add and remove from Panel

**Adding** is the one action in §4. **Removing is one action too, and it acts at once:**

![Removing a member](diagrams/05-remove-a-member.svg)

*Figure 5. Removing a person in one action. Access is cut on the servers first; Git follows. A server that cannot be reached stays on a pending list.*

1. **On each of the person's servers:** end their sessions, remove their cron jobs, remove them from `sudo` and groups, **delete the account** (the admin can choose to lock it and keep its files instead).
2. **Verify:** the account is gone and no session is left.
3. **In Git:** delete `members/<id>.enc`, remove the person from the recipients, re-encrypt, sign and push.
4. **On the Git host:** revoke their read access.
5. **Report:** which servers were cleared, and which could not be reached.

Because the account no longer exists, **a password or key from the person's old login file is useless**, even though it stays in the Git history. That is what keeps credentials in the repository acceptable.

- **A server that cannot be reached** keeps access until it is done. It goes on a **pending list** that Panel keeps, shows to the admin and retries; it is never reported as done.
- **Panel only knows what it made.** A key or a cron job the person added to another account, or an account made outside Panel, can be missed. A **check** lists what still belongs to the person on each server, and the admin sees it after removal.
- **Secrets the person saw on the server** (a database password in a `.env`) are not changed by this. The removal report reminds the admin to change them.

### Rotation

A password has a rotate-by date in the login file. Panel can **rotate** a person's password on all their servers in one action: set a new one, write the new login file, push. An old value in the Git history stops working at once.

### What is out of scope

- **Servers with central accounts** (LDAP, FreeIPA, SSSD): Panel should not create local accounts there. It detects this where it can and leaves those servers to the directory.
- **Kubernetes.** A cluster in a space holds its address and certificate authority only. Each member uses **their own credential** for it, bound on their machine; issuing it depends on the cluster and is left for later.
- **SSH certificates** are a later option: an admin signs a short-lived certificate for each member, servers trust the team's certificate authority once, and removal is the end of the lifetime. A certificate's **key ID** is written in the server's login log. For about 20 developers direct accounts are enough; certificates become the main path beyond roughly 50 people, and then **who signs them** needs an answer (an admin signing in batches, or a signer that is always on).
- **Host keys.** The admin verifies each server's host key once and stores it in the space. A member's client uses those and refuses a mismatch, so a new member never has to trust a host on first use.

## 6. If the repository leaks

Everything in the repository is encrypted. What a reader sees:

| What leaks | What it means |
| --- | --- |
| `team.json`: member names, **public** keys, which member is a recipient of which space | The shape of the team. Public keys are public |
| File sizes, commit times, commit authors, the number of member files | Metadata only. A generic commit author can be set to hide names |
| **History** | Old ciphertext stays in Git. A person who was removed can still open the files from when they were a recipient: the host list, and **their own old logins, which no longer work** because the account is gone |

What a reader does **not** get: any login, any space or host name, and **no password to guess**. There is nothing derived from a password in the repository, so there is nothing to attack offline.

The weak point is a **member's private repository key** stolen from their machine: the thief opens that member's host lists and their own logins, and, while those logins still work, can use them. Treat it as a removal: remove the person (all their logins die), and re-add them with a new key. Keep `snippets` free of secrets: a snippet is plain text inside the space.

## Limitations to tell the user

These belong on the *New team* screen and in the user guide, in plain words:

- **Members can only connect.** They cannot add hosts or change anything. Their own hosts go in their Personal profile.
- **The admin sets up each person's account on each server**, from Panel. The admin knows the logins they issue (unless the person uses their own key).
- **A password is still a password.** Key login is better, and lets the server turn password login off.
- **Removal is only as complete as Panel's reach.** A server that cannot be reached keeps the access until it is cleared, and anything made outside Panel can be missed.
- **Removing someone does not erase what they saw** (the host list up to that day, secrets on the servers).
- **Write protection is the Git host's, and the admin signature backs it up.** Someone with write access cannot forge a host list or a member list: clients refuse anything not signed by an admin. They could still delete or damage files; clients keep their last good copy and Git restores the files.
- **No recovery of a private key.** A new key replaces a lost one.
- **Not real time.** Members see a change after the next sync.
- **No SSO, no SLA, no compliance reports** of our own. Access to the repository is the Git host's.

## Is it worth building? (assessment, for thinking over)

Written after the design was reviewed. It is an opinion, not a measurement: nothing here comes from user research.

### For

- **Fits the positioning.** Local-first, the team's own Git, no account, free. A hosted competitor cannot copy this without changing how it earns money.
- **The typical case matches the design.** A small DevOps team gives about 20 developers access to servers: two or three writers and many readers. Developers already have an account on the Git host, so read access costs nothing.
- **A real pain: giving and taking back access on many servers.** Today a team may issue passwords through a password management page. Creating an account on each server by hand, delivering the login and clearing it all when someone leaves is slow and easy to get wrong. One action in Panel for each is concrete value, and Panel already manages users, cron and SSH keys on a server.
- **Reuses what exists:** Panel, Git sync, the vault, the process-per-window pattern.
- **Reversible:** behind an experimental flag, a repository format of its own, Personal untouched.

### Against

- **No evidence of demand.** [PanelRoadmap.md](PanelRoadmap.md) already says its ideas are hypotheses, and this one is no different. One answer so far, from the author's own team.
- **A different audience.** The main users today are solo developers, freelancers and small agencies.
- **The competition is not only Termius.** Teams already use password managers that share an item with one person, a bastion or SSO (Teleport, Boundary), Tailscale SSH, AWS SSM, an SSH certificate authority, Ansible, or a shared `ssh_config` in Git. They tend to ask for SSO, central audit and SOC 2, which this design lists as non-goals.
- **Per-person passwords are already close.** Where each developer already has their own password and account, removing one person is already possible without touching the others. What stays: a key is never sent or typed and cannot be phished or guessed; the server holds no secret to leak or crack; password login can be switched off; and, above all, **the account and login setup across many servers is automated** instead of done by hand. If that manual work is not a real pain for a team, the feature gives it little.
- **A large cost for one maintainer.** M2 (encryption, signature, sync, merge) and M4 (Panel creating accounts and credentials on many distributions, removal that must be complete, unreachable servers) are each big, on top of four languages of interface, tests, documentation and an outside review of the cryptography.
- **A reputation risk.** A bug in the team part reads as "an SSH tool leaked a company's infrastructure", which is heavier than a bug in the personal app.

### Scorecard

| | Assessment |
| --- | --- |
| Fit with the strategy | High |
| Difference from competitors | Medium: the automation of accounts and logins, not who holds a secret |
| Evidence of demand | One data point (the author's own team hands out passwords through a password management page); nothing from outside yet |
| Cost | High |
| Risk | Medium to high (security) |
| Reversibility | Medium |

### What is worth doing even without a team

- **M1 (profiles):** Work and Personal apart is useful to one person.
- **Account, login and offboarding in Panel (the Panel part of M4):** "make an account for this person on these servers", "give them a password or install their key", and above all **"remove this person from every server at once: end sessions, remove cron and sudo, delete the account, check, list what is pending"** helps **one admin of a few servers** too, with no team, no repository format and no new cryptography. It is the part with the most standalone value. In a Team profile the same actions simply also deliver the login through the repository.
- **M2 and M3 (encryption, signature, members)** only matter once a team uses them. They are the expensive and risky part.

### Recommendation

1. **Build M1 now.**
2. **Build the Panel account, login and offboarding actions as a feature of its own**, shippable without Team.
3. **Do not commit to M2 and M3 yet.** First find about three real teams willing to try, and give them a light version: the Panel actions, with the login copied to the person in the way they use today. See what they miss. Measure how long adding and removing one developer takes today across the servers; that is what the feature has to beat.
4. **Go on to M2 and M3 only if** several teams say plainly that they will use it and the pain they name is the one this design solves (setting up and clearing access on many servers, a shared host list). If they ask for SSO, central audit or SOC 2, **stop**.

## Open questions (to decide)

| # | Question | Notes |
| --- | --- | --- |
| 1 | Is the demand real? Which three teams would try it? | Decides whether M2 starts at all |
| 2 | Should the Panel account, login and offboarding actions ship on their own first? | Recommended: yes |
| 3 | A **recovery recipient** (an extra key kept offline, always a recipient of every file)? | Without it, losing every admin key leaves the team unable to change anything. At least two admins are required either way |
| 4 | The repository key is a software key. A person who only has a hardware key (`-sk`) cannot open the files | They would keep a separate software key for the repository. Acceptable? |
| 5 | Password rotation: how often, and is it forced? | Rotate-by date in the login file; Panel rotates in one action |
| 6 | Per-person Kubernetes credentials | Issuing them depends on the cluster. Left out of v1 |
| 7 | Members need an account on the Git host. For outside contributors that may cost money | A shared read-only token is acceptable here (it only reads ciphertext) |
| 8 | Which distributions does Panel support when it makes accounts, and what about root-only servers and central accounts (LDAP, SSSD)? | A support matrix is needed before M4. Panel needs a privileged login on each server to start with: who provides it? |
| 9 | What to call it: "Team" suggests more than it offers (no SSO, no audit). "Shared hosts for small teams"? | The limits are on the *New team* screen either way |
| 10 | How do DevOps teams give developers SSH access today? | **Answered by one team: passwords through a password management page, one per developer.** Ask the pilot teams too |
| 11 | What do developers need to reach: dev and staging machines, production, Kubernetes? | Decides the permissions the admin sets on each account |
| 12 | Add members by Git host username (the shortcut in §4)? | Saves the key exchange for 20 people |
| 13 | At what size do SSH certificates become the main path, and who signs them? | About 20 developers does not need them; beyond roughly 50 it probably does |
| 14 | May a member read their own password in the app, or only use it? | Reading helps with tools outside Vutty; not showing it is safer |

## Milestones

| | What | Done when |
| --- | --- | --- |
| **M0** | Spike (done): process per profile, see §1 | this document |
| **M1** | `profiles.json`, *Open profile…*, create, rename, delete, per-profile lock, `--profile`, `show` action, Team processes skip self-update and `ssh://`, *Copy to another profile…* | two profiles run side by side; Personal behaves as before; the existing tests pass. Useful alone: Work and Personal apart |
| **M2** | Team core: signed `team.json`, `age` with SSH recipients, space files and member files, **pinned admin keys, file hashes and `version` check**, **member pull-only sync**, admin merge with `Rev`, `base_rev`, clock guard, Conflicts list, `format` and `min_client` | a member opens only their files; a forged, replaced or older state is refused; two admins converge with no silent loss; a member's client never writes |
| **M3** | Members: repository key (generate or import), join, add to spaces, optional add by Git username | a member joins and opens their spaces |
| **M4** | Panel: **add a member** (account per server, credential: password, generated key or their own key, login file) and **remove a member at once** (sessions, cron, sudo, account, verify, Git, pending list), rotation, the check of what is left | a member connects with their own account; removal clears every recorded server and lists any it could not reach |
| **Pilot** | One or two real teams use it for a few weeks | what hurts is known before more is built |
| **M5** | SSH certificates (short-lived, one trusted authority per server) | removal by expiry, no per-server work |
| **Later** | Per-person Kubernetes credentials; a per-record file layout if a space grows large; per-space write control | on demand |

M1 helps people with one profile too. The Panel part of M4 can ship **without** M2 and M3 (see the assessment). M2 changes the format of Team repositories only. M2 onward is labelled experimental until reviewed.

## Tests

- `age` round trip with an `ssh-ed25519` identity and several recipients; a file opens for a recipient and **not** for a removed one (re-encrypted file); an `ssh-rsa` key also works.
- **A member opens only their own login file**: a test with two members, each tries the other's file.
- A client that is not a recipient never shows the file and never rewrites it (byte-for-byte check).
- A member's client **never pushes**; a push by a member is refused by the Git host (a test with a read-only remote).
- **Signature:** a manifest signed by an unpinned key, a file that does not match its hash, a replaced file, a manifest with a lower `version` and an unsigned change of members or recipients are each refused, and the client keeps its last good copy.
- `syncgit`: real-`git` scenarios (bare repositories) with two admins: simultaneous edits, delete against edit, rejected push, a long-offline admin, a fresh clone taking the remote as base.
- A property test for the admin merge: the result converges whatever the order of syncs.
- **Panel add and remove, against real servers** (containers): the account is created with the credential, the person can log in; after removal the account and sessions are gone, cron and sudo entries are removed, and an unreachable server stays on the pending list and is retried. Both actions are safe to run twice.
- After removal, the old password or key from the Git history **no longer logs in**.
- Versions: an older client opens a newer repository read-only.
- `archtest`: no package reads another profile's folder.
- Strings for every new screen in all four languages.
- **A person with cryptography experience reviews M2 and M3**, and someone reviews the Panel account and removal code for what it deletes. The design uses `age` as it is and no hand-made construction.

## Risks

| Risk | Handling |
| --- | --- |
| Someone with write access forges or rolls back the team | Admin signature, pinned admin keys, file hashes and a `version` check (M2) |
| An admin's key is stolen | The admin is removed by another admin; at least two admins; keep the key with a passphrase or on hardware |
| A member's repository key is stolen | Remove the person (their logins die with the account) and re-add them with a new key |
| Panel deletes the wrong account or too much | It only touches accounts it recorded; a confirmation lists what will be removed; the option to lock and keep files |
| Removal is incomplete (an unreachable server, something made outside Panel) | A pending list that is kept and retried, and a check of what is left |
| Credentials in the repository | Encrypted to one person and the admins; dead after removal; rotation in one action |
| Passwords stay passwords | Key login encouraged; the server can turn password login off |
| A bug in the admin merge loses data | Property test and two-admin tests from M2; members never write, so the blast radius is two or three machines |
| The repository grows (whole-file rewrite, ciphertext does not delta) | Only admins write, which is few commits; member files change rarely; measure in M2; clients can fetch shallow |
| Panel needs privileged access on every server | Say so in the setup; the admin provides it, with their own login |
| Mixed app versions | `format` and `min_client`; older clients read only |
| More surface in a beta | Experimental flag; Personal untouched; a pilot before M5 |

## Non-goals

- SSO, SAML, SCIM and an SLA of our own: access to the repository is controlled by the Git host (for example GitHub SAML SSO).
- Members who edit, or who add hosts to the team.
- A shared account or a shared password for several people.
- Replacing a password manager for other secrets (API keys, service accounts).
- Servers with central accounts (LDAP, SSSD) in v1.
- Per-record permissions inside a space.
- A server of ours, a hosted sync or real-time collaboration.
- Proving who connected to a server (that is the server's log).
- Recovery of a lost private key.

## Alternatives considered

![The three designs compared](diagrams/06-options.svg)

*Figure 6. Option 3 is chosen: a shared host list, and a file with each person's own logins that only they can open.*

| | Way | Read control | Cost |
| --- | --- | --- | --- |
| **1** | One repository, **a password per space** (revision 4) | Encryption | Passwords to hand out and rotate; every holder can write; an admin slot that a non-admin cannot keep |
| **2** | One repository, **SSH-key recipients, no credentials in the repository** (revision 5) | Encryption per person | Every server must accept key login; the setup is by hand |
| **3** | **Shared host list and per-person login files** (this document) | Encryption per person, for lists and for logins | Credentials sit in the repository (encrypted, and dead after removal); Panel must create and remove accounts |

Termius works in a similar way to option 2: a key per vault wrapped to each member's public key, with its cloud checking who may write. Here the Git host takes the place of that cloud. Termius also lets the owner keep credentials in the shared vault or in each member's personal vault; option 3 is the second, with the admin delivering them.

## Changes from revision 5

| Change | Why |
| --- | --- |
| **Per-person login files** (`members/<id>.enc`) hold each person's account and credential | The admin issues the account and delivers the login through Git, replacing a password management page. Only that person (and the admins) can open it |
| **Passwords are allowed**; key login is encouraged, not required | A password is the member's own, so a server that only takes passwords works. The condition "every server must accept key login" is dropped |
| **No shared account**; every member has their own account on each server | Audit and least privilege per person; removal touches one account. Replaces the shared `login` setting |
| **Removal is one Panel action that acts at once**: servers first, Git second, verify, pending list | Cutting access happens on the server. It also makes the old login in the Git history useless |
| Panel creates accounts and credentials; a rotation action | The automation is the real value |
| The repository key is separate from the login credential | The question "one key for two purposes" goes away |
| "The repository holds no credential" is dropped | It now does, encrypted to one person; the leak section says what that means |
| Assessment and open questions updated | The gain is mostly automation, since per-person passwords are already close; the Panel actions are the standalone feature |

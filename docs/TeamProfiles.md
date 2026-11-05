# Profiles: Personal and Team

Status: **design (revision 5), nothing built yet.**

Decisions taken:

- The typical case: a small DevOps team (2 or 3 **admins**) gives access to about **20 developers**, who are **members who can only connect**: they cannot view credentials, edit hosts or change the team. This is the size the design is made for (about 25 people in all).
- The **admin creates the private Git repository**. Git is the shared database and **everything in it is encrypted on each client**.
- **No shared passwords at all.** Every person has their own SSH key. A member's key opens the spaces they were given and logs in to the servers; nobody hands anyone a secret.
- **Members have read-only access** to the repository on the Git host; only admins can write. The Git host enforces it. This is a sharing model from the admin: members do not write.
- Servers accept **key login**. A server that only takes a shared password is not supported in a Team profile, because it cannot be "connect only".
- Keys are installed on servers through Panel first; **SSH certificates** come later (M5).
- Cluster credentials are per person, like server keys (§5).
- **The repository stays encrypted** (decided). Host information is not a credential, but a leaked repository would otherwise be a map of the infrastructure, and encryption is what splits read access per space inside one repository.
- Whether the whole feature is worth building is assessed below, with a recommended order. **It is not yet decided to build M2 onward.**

## Goal

Let a team share hosts, groups, snippets, port forwards and clusters through **their own private Git repository**, with no server of ours, and keep a person's own vault separate from the team's. A person has one **Personal** profile and any number of **Team** profiles.

What this gives a team: a shared host directory where each member sees only the spaces they were given, and connects with a key that is theirs, on the Git host the team already pays for. What it does not give: SOC 2, an SLA, SAML on our side. See [Limitations](#limitations-to-tell-the-user) and [Non-goals](#non-goals).

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

The processes do not share memory, so copying hosts between Personal and a Team profile goes through what already exists: **export the selected hosts** (plain JSON for hosts without secrets, or an encrypted file with its own password for hosts with secrets) and **import** them in the other profile. A *Copy to another profile…* button wraps this. Nothing is moved automatically. A member cannot copy *into* the team: the team is read-only for them.

### Leaving a team

*Leave team* deletes the local profile folder and its keychain entry. It does not change the repository. The admin removes the person (§4).

## 2. Roles and the trust model

| | Admin | Member |
| --- | --- | --- |
| Repository on the Git host | write | **read only** |
| Opens which spaces | all (an admin is always a recipient) | only the spaces they were added to |
| Sees credentials | there are none to see (§5) | none |
| Edits hosts, spaces, members | yes | no |
| Connects to hosts | yes, with their own key | yes, with their own key |

- **Write control is the Git host's.** A member's client does not try to push, and a push would be refused anyway. This is what makes the team connect-only without signing every record.
- **Read control is cryptographic.** A member who can read the repository still cannot open a space they were not added to.
- A team has two or three admins. An admin who loses their key is replaced by another admin, who re-encrypts the spaces.
- Trust sits with the admins. The Git host decides who can write, and the **admin signature** (§3) lets every client check that what it read really came from an admin. A write permission given by mistake, a leaked write token or a compromised Git host account cannot slip in a forged host list or put an old one back.

## 3. Repository format

Personal keeps `bundle.enc`. A Team repository has:

```
team.json                format, min_client, team id, version,
                         admins [{id, SSH public key}],
                         members [{id, name, SSH public key}],
                         spaces [{id, recipients: [member ids], file SHA-256}],
                         signature (by an admin)
spaces/<id>.enc          one file per space, age-encrypted to the SSH public keys of its recipients
```

![Each space file is encrypted to the keys of its recipients](diagrams/02-keys-and-recipients.svg)

*Figure 2. A space file is encrypted to the SSH public keys of its recipients. Each person opens it with their own private key.*

- **A space** is the unit of sharing: for example `prod`, `staging`, `client-acme`. Its file holds the name, groups, hosts, snippets, port forwards, clusters (address and certificate authority, no credentials), the **host keys the admin has verified** and the list of keys installed on its servers (§4). It holds **no credential**.
- **Encryption** uses [`age`](https://github.com/FiloSottile/age) with the recipients' **SSH public keys** (`ssh-ed25519`; `ssh-rsa` also works for imported keys). `age` puts one entry per recipient in the file's header and encrypts the data with a fresh key each time, so there is no separate key to store or rotate: **re-encrypting the file to a new recipient list is the rotation.**
- **Space names are inside the file**, not in `team.json`, so a leaked repository does not show client or project names. `team.json` lists member names and public keys; the public keys are public.
- **A member's client** reads `team.json`, finds the spaces it is a recipient of, decrypts them with the member's key and replaces its read-only local copy. It never writes to the repository and never touches a space it is not a recipient of.
- **An admin's client** does the same, and on a change merges, re-encrypts the changed spaces to the current recipients and pushes.
- The branch is configurable (default `main`). A protected branch that refuses direct pushes does not work with this design; the setup screen says so.
- **There is no password or hint anywhere in the repository.**

![Data flow of one sync](diagrams/03-sync-data-flow.svg)

*Figure 3. One sync. A member only reads; an admin also merges, re-encrypts and pushes. A space the client has no key for stays hidden and untouched.*

### Admin signature: who really wrote this

`age` keeps a file secret and detects damage, but it does **not** say who made it: the recipients' public keys are public, so anyone who can write to the repository can make a valid file for the same recipients. Without a signature, a leaked write token, a write permission given by mistake or a compromised Git host account could replace a space with a host list that points at a server of the attacker's, or put an older commit back to bring a removed person back.

So the team is **signed**:

- `team.json` is a manifest. It lists the admins' public keys, the members, the recipients of each space, the **SHA-256 of every `spaces/<id>.enc`**, and a `version` that goes up on every change. An admin signs it (Ed25519, with their SSH key).
- **A member's client checks before it decrypts anything:** the signature is by a pinned admin key, each space file matches its recorded hash, and `version` is not lower than the highest one this machine has seen. Otherwise it keeps what it had, shows a red warning and opens nothing new.
- **Pinning.** When a person joins, the app shows the fingerprints of the team's admin keys and asks them to compare with an admin (by voice or chat), as for an SSH host key. After that the client trusts only those keys.
- **Changing the admins.** A new admin is added by a manifest signed by an existing admin, and an admin is removed the same way. A client follows the chain from the keys it pinned.
- **Rollback.** An old signed manifest has a lower `version`, so putting an older commit back is refused.
- Any admin can sign; the signature covers the whole repository state, so a change to a space and the matching manifest are one commit.

What it protects against: a forged or replaced space, a forged member or recipient list, and a rollback. What it does not: an attacker who holds an **admin's key** (that admin is removed, §4), and an attacker who deletes or corrupts files (the client detects it and keeps its last good copy; Git restores the files).

### Format and client versions

`team.json` carries `format` and `min_client`. A client older than `min_client` opens the team **read-only** and says why; a client never writes a format newer than it understands. Most people only read, so a member on an older version is rarely affected.

### Revisions and conflicts (between admins)

Members never write, so conflicts can only happen between the two or three admins. A member's sync is a replace, so it needs no merge and no tombstones.

`Meta` gains `Rev` (a hybrid logical clock: wall time, counter, admin label) and `UpdatedBy`. `UpdatedAt` stays for compatibility and display. Each admin machine keeps, per record, the `rev` it last synced (`base_rev`). On a sync:

- only the remote side moved since `base_rev` → take it;
- only the local side moved → push it;
- **both moved** → keep the one with the higher `Rev`, and put the other in a **Conflicts** list (who, when, what) that can be restored with one click. Nothing is overwritten silently;
- a tombstone beats an edit only when its `Rev` is higher. Tombstones are kept: with two or three writers they stay small.
- a machine that has just cloned has no `base_rev`: the first sync takes the remote as its base.

A `Rev` whose wall time is more than a day ahead of this machine's clock is **refused** and reported, so one computer with a wrong clock cannot make its edits win forever.

### Where the keys live on a machine

The Team profile has its own **local vault** (the existing `vault` package, unlocked with a password the person sets for their own machine, also with Touch ID or Windows Hello). It holds the person's SSH private key for this team. The same key is used to open the spaces and to log in to servers; `age` supports using an SSH key this way. A person can generate the key in Vutty or import one they already have.

## 4. Members: joining and removing

### Joining

![Joining a team](diagrams/04-join-a-team.svg)

*Figure 4. The member sends a public key, the admin adds it to the spaces and installs it on the servers, and the member fetches, decrypts and connects with their own key.*

1. The admin gives the person **read access** to the repository on the Git host (the first gate; SSO and the team's own rules apply there).
2. In Vutty the person chooses *Join a team* with the repository URL. The app makes a key pair (or imports one), keeps the private key in the local vault and shows the **public key and its fingerprint**.
3. They send the public key to an admin over any channel. It is not a secret.
4. The admin compares the fingerprint with the person (by voice or chat), picks the spaces and approves. The app adds the member to `team.json`, re-encrypts those spaces to the new recipient list and pushes.
5. **Panel installs the public key** on the servers of those spaces (`authorized_keys`, using the key management Panel already has) and records which servers have it.
6. The member syncs, finds their spaces, and connects. Nobody typed or received a password.

*Shortcut, an option for M3:* the admin adds a member by their **Git host username** and Vutty reads their public SSH keys from the host (GitHub and GitLab publish them at `/<user>.keys`). There is no key to send and no fingerprint to compare, and the Git host has already confirmed who the person is. Only `ssh-ed25519` and `ssh-rsa` keys can be used (`age` cannot encrypt to hardware `-sk` keys), and the developer must make the matching private key available to Vutty in their Team profile.

### Removing a member

![Removing a member](diagrams/05-remove-a-member.svg)

*Figure 5. Removing someone: re-encrypt without them, remove their key from the servers, revoke their read access. Nobody else has to do anything.*

1. The admin removes the member from the recipients of their spaces. The app **re-encrypts** those files without them and pushes.
2. **Panel removes the person's public key** from every server recorded for those spaces, in one run, and lists any server it could not reach.
3. The admin revokes the person's read access on the Git host.
4. Nobody else enters or receives anything.

What this cannot take back: old commits still open with the person's key, so they keep the host list they saw, and their machine keeps its local copy. Because no credential was ever shared, there is nothing else to change on the servers once their key is gone.

### Lost machine or key

There is no recovery of a private key. The person makes a new key and sends the public key; an admin swaps it (add the new key, remove the old one, on the spaces and the servers).

## 5. Server access: one key per person

A member connects with **their own key**; no server credential is stored in the repository.

- **Key install through Panel (first).** The admin installs a member's public key on the servers of a space, and removes it later. Panel already manages SSH keys of users on a server, so this is one more action over the existing connection, and the app keeps the list of servers per member for removal.
- **SSH certificates (later).** An admin signs a short-lived certificate for each member (`ssh-keygen -s`), the servers trust the team's certificate authority once (`TrustedUserCAKeys`), and certificates are public files that can sit in the repository. Removal is the end of the lifetime, so nothing has to reach every server. This scales better than installing keys on many servers. A certificate's **key ID** is written in the server's login log, so even a shared account shows who logged in. For about 20 developers installing keys directly is enough; certificates become the main path when the number of people or servers makes per-server work too heavy (roughly beyond 50 people), and then **who signs the certificates** needs an answer (an admin signing in batches, or a signer that is always on).
- **Servers that only take a shared password** cannot be made connect-only: a client that can log in holds the credential. They are not supported in a Team profile; the setup says so.
- **Kubernetes.** A cluster in a space holds its address and certificate authority only. Each member uses **their own credential** for it (a client certificate or token issued to that person, bound on their machine). The earlier idea of a shared kubeconfig is dropped, because a shared token is a credential a member could read.
- **Host keys.** The admin verifies each server's host key once and stores it in the space. A member's client uses those and refuses a mismatch, so a new member never has to trust a host on first use.

### Which account on the server

A key gives a person a way in, but it does not say **who they become** on the server. That is the *Unix account* (also called the login user or the account name): the name in `ssh deploy@server`. Each account on a server has its own home folder, files, groups and permissions (for example whether it may use `sudo`). A person's key is listed in that account's `~/.ssh/authorized_keys`, and a key listed there logs in **as that account**.

So "everyone has their own key" is not the same as "everyone has their own access". Two ways to set it up:

| | One shared account (for example `deploy`) | One account per person (`alice`, `bao`, …) |
| --- | --- | --- |
| Keys | Everyone's key is in `deploy`'s `authorized_keys` | Alice's key is only in `alice`'s |
| What a member can do | Everything `deploy` can: read its files and `.env`, use its `sudo` | Only what that person's account is allowed |
| Who did it | The server sees `deploy` for everyone; the log does not say who | The server sees `alice`: history, logs and `sudo` records are per person |
| Removing someone | Remove their key from the file | Remove the key and, if wanted, the account |
| Work for the admin | Low: one file per server | Higher: one account per person per server, which Panel can make (it already adds and deletes users) |

What the team vault never holds is a **credential**, but a person who can log in as `deploy` can still read whatever `deploy` can read on that server: the application's configuration, database passwords. "Connect only" means the member cannot take anything out of the *team's vault*; it does not shrink what the server account allows.

The design therefore works like this:

- A host in a space has a `login` setting: a fixed name (`deploy`) or **per person** (the account named after the member).
- **Per person is the default for hosts marked prod.** Panel creates the account, adds the key, and removes both when the person is removed. The app records the pair (member, server, account) in the space, so removal knows what to clear.
- A **shared account is allowed** where the team chooses it, with a warning in the host form: *"Everyone who opens this host logs in as «deploy» and sees everything this account sees. The server cannot tell them apart."*
- Panel does not create accounts with `sudo` or root rights on its own; a person's permissions are set by the admin when the account is made.
- With SSH certificates (M5) the same split is made by **principals** in the certificate (which account a certificate may log in as) and can also limit the commands.

## 6. If the repository leaks

Everything in the repository is encrypted. What a reader sees:

| What leaks | What it means |
| --- | --- |
| `team.json`: member names, **public** SSH keys, which member is a recipient of which space | The shape of the team. Public keys are public |
| File sizes, commit times, commit authors | Metadata only. A generic commit author can be set to hide names |
| **History** | Old ciphertext stays in Git. A person who was removed can still open the files from the time they were a recipient (the host list) with their own key |

What a reader does **not** get: any credential (none is stored), any space name, any host name, and **no password to guess**. There is no key derived from a password anywhere in the repository, so there is nothing to attack offline.

If a **member's private key** is stolen from their machine, that person is removed and re-added with a new key (§4); the thief gets the spaces that person could open and, until the key is removed, their server logins. Keep `snippets` free of secrets: a snippet is plain text inside the space.

## Limitations to tell the user

These belong on the *New team* screen and in the user guide, in plain words:

- **Members can only connect.** They cannot add hosts or change anything. Their own hosts go in their Personal profile.
- **It needs key login on the servers.** A server that only takes a shared password is not supported.
- **The admin does the onboarding**: add the key to the spaces, install it on the servers. Certificates reduce this later.
- **Write protection is the Git host's, and the admin signature backs it up.** A member who is given write access by mistake cannot forge a host list or a member list: clients refuse anything not signed by an admin. They could still delete or damage files; clients keep their last good copy and Git restores the files.
- **A key is not the same as limited access.** A member logs in as a Unix account on the server and can do what that account can do. Use one account per person where it matters (§5).
- **Removing someone does not erase what they saw** (the host list, up to the day they were removed). It does take away their access.
- **No recovery of a private key.** A new key replaces a lost one.
- **Not real time.** Members see a change after the next sync.
- **No SSO, no SLA, no compliance reports** of our own. Access to the repository is the Git host's.

## Is it worth building? (assessment, for thinking over)

Written after the design was reviewed. It is an opinion, not a measurement: nothing here comes from user research.

### For

- **Fits the positioning.** Local-first, the team's own Git, no account, free. A hosted competitor cannot copy this without changing how it earns money (Termius Team is a paid cloud service).
- **A real pain: offboarding.** Removing a person from many servers is tedious and easy to get wrong. One place that removes their key, their account and their read access is concrete value, and Panel already manages users and SSH keys on a server.
- **Reuses what exists:** Panel, Git sync, the vault, the process-per-window pattern.
- **Reversible:** behind an experimental flag, a repository format of its own, Personal untouched.
- **The typical case matches the design.** A small DevOps team gives about 20 developers access to servers. That is two or three writers and many readers, which is the admin and member split here. Developers already have an account on the Git host, so read access costs nothing, and at this size installing keys directly is workable without certificates.

### Against

- **No evidence of demand.** [PanelRoadmap.md](PanelRoadmap.md) already says its ideas are hypotheses, and this one is no different.
- **A different audience.** The main users today are solo developers, freelancers and small agencies. A team of 15 to 20 people has other expectations.
- **The competition is not only Termius.** Teams with infrastructure often already use a bastion or SSO (Teleport, Boundary), Tailscale SSH, AWS SSM, an SSH certificate authority, or a shared `ssh_config` in Git with a password manager. They tend to ask for SSO, central audit and SOC 2, which this design lists as non-goals.
- **A large cost for one maintainer.** M2 (encryption, signature, sync, merge between admins) and M4 (Panel creating accounts and installing keys on many distributions, unreachable servers) are each big, on top of four languages of interface, tests, documentation and an outside review of the cryptography.
- **A reputation risk.** A bug in the team part reads as "an SSH tool leaked a company's infrastructure", which is heavier than a bug in the personal app.
- **Per-person passwords are already close.** Where each developer already has their own password (and their own account on the server), removing one person is already possible without touching the others, so the security gain of per-person keys is smaller than against a shared password. What stays is: a key is never sent or typed and cannot be phished or guessed over the network; the server holds no secret to leak or crack; password login can be switched off; and, above all, **the account and key setup across many servers is automated** instead of done by hand. If that manual work is not a real pain for a team, the feature gives it little.

### Scorecard

| | Assessment |
| --- | --- |
| Fit with the strategy | High |
| Difference from competitors | Medium to high for offboarding, low for the shared vault itself |
| Evidence of demand | One data point (the author's own team hands out passwords through a password management page); nothing from outside yet |
| Cost | High |
| Risk | Medium to high (security) |
| Reversibility | Medium |

### What is worth doing even without a team

- **M1 (profiles):** Work and Personal apart is useful to one person.
- **Key and account management on many servers (the Panel part of M4):** "add this key to these five servers", "remove that one", "make an account for this person" helps **one admin of a few servers** too, with no team, no repository format and no new cryptography. It is the part with the most standalone value.
- **M2 and M3 (encryption, signature, members)** only matter once a team uses them. They are the expensive and risky part.

### Recommendation

1. **Build M1 now.**
2. **Build the Panel key and account management as a feature of its own**, shippable without Team.
3. **Do not commit to M2 and M3 yet.** First find about three real teams willing to try, and give them a light version: sharing hosts through a private Git repository as today (the existing sync) plus the Panel key management. See what they miss.
4. **Go on to M2 and M3 only if** several teams say plainly that they will use it and the pain they name is the one this design solves (offboarding, a shared host list). If they ask for SSO, central audit or SOC 2, **stop**: this design does not meet those and should not try to.

## Open questions (to decide)

| # | Question | Notes |
| --- | --- | --- |
| 1 | Is the demand real? Which three teams would try it? | Decides whether M2 starts at all |
| 2 | Should the Panel key and account management ship on its own first? | Recommended: yes |
| 3 | A **recovery recipient** (an extra key kept offline, always a recipient of every space)? | Without it, losing every admin key leaves the team unable to change anything: a member cannot add a new admin because they cannot write. At least two admins are required either way |
| 4 | Hardware keys (`ed25519-sk`) cannot decrypt with `age`, which needs the private key. Acceptable? | A team that requires hardware keys would not fit |
| 5 | One SSH key for decrypting and for logging in, or two? | One is simpler; two is the usual advice. A reviewer should decide |
| 6 | Per-person Kubernetes credentials | Issuing them depends on the cluster. Left out of v1 |
| 7 | Members need an account on the Git host. For outside contributors that may cost money | A shared read-only token is acceptable here (it only reads ciphertext) |
| 8 | Per-person Unix accounts by default for prod hosts: which distributions does Panel support, and what about root-only servers? | A support matrix is needed before M4 |
| 9 | What to call it: "Team" suggests more than it offers (no SSO, no audit). "Shared hosts for small teams"? | The limits are on the *New team* screen either way |
| 10 | How do DevOps teams give developers SSH access today (one shared key, a shared password, a bastion, keys added by hand)? | The answer shows what the feature replaces and how big the pain is. **One answer so far (the author's own team): passwords are issued through a password management web page.** That is one data point from one team, so ask the pilot teams too. The passwords are issued **per developer**, so the gain over today's practice is mainly the automation of accounts and keys, not a change of who holds a secret (see the assessment). Measure how long adding and removing one developer takes today across the servers: that is what the feature has to beat |
| 11 | What do developers need to reach: dev and staging machines, production, Kubernetes? | Decides the default `login` (shared on dev, per person on prod) and whether Kubernetes cannot wait |
| 12 | Add members by Git host username (the shortcut in §4)? | Saves the key exchange for 20 people; needs the private key to be available to Vutty |
| 13 | At what size do SSH certificates become the main path, and who signs them? | About 20 developers does not need them; beyond roughly 50 it probably does |

## Milestones

| | What | Done when |
| --- | --- | --- |
| **M0** | Spike (done): process per profile, see §1 | this document |
| **M1** | `profiles.json`, *Open profile…*, create, rename, delete, per-profile lock, `--profile`, `show` action, Team processes skip self-update and `ssh://`, *Copy to another profile…* | two profiles run side by side; Personal behaves as before; the existing tests pass. Useful alone: Work and Personal apart |
| **M2** | Team core: `team.json`, `age` with SSH recipients, space files, **admin signature, pinned admin keys, file hashes and `version` check**, **member pull-only sync**, admin merge with `Rev`, `base_rev`, clock guard, Conflicts list, `format` and `min_client` | a member opens only their spaces; a forged, replaced or older repository state is refused; two admins converge with no silent loss; a member's client never writes |
| **M3** | Members: key generate or import, join, add to spaces, remove (re-encrypt), lost key | a removed member cannot open the new files; others do nothing |
| **M4** | Servers: Panel installs and removes a member's key and records where; **a `login` per host (shared account, or one account per person made by Panel)**; admin-verified host keys in the space | a member connects with their own key and their own account; removal clears every recorded server and account |
| **Pilot** | One or two real teams use it for a few weeks | what hurts is known before more is built |
| **M5** | SSH certificates (short-lived, one trusted authority per server) | removal by expiry, no per-server work |
| **Later** | Per-person Kubernetes credentials (issuing them depends on the cluster); a per-record file layout if a space grows large; per-space write control; a shared credential with an explicit warning for legacy servers | on demand |

M1 helps people with one profile too. M2 changes the format of Team repositories only; moving Personal to the same format is a separate decision for later. M2 onward is labelled experimental until reviewed.

## Tests

- `age` round trip with an `ssh-ed25519` identity and several recipients; a file opens for a recipient and **not** for a removed one (re-encrypted file); an `ssh-rsa` key also works.
- A client that is not a recipient never shows the space and never rewrites its file (byte-for-byte check).
- A member's client **never pushes**; a push by a member is refused by the Git host (a test with a read-only remote).
- **Signature:** a manifest signed by an unpinned key, a space file that does not match its hash, a replaced space, a manifest with a lower `version` and an unsigned change of members or recipients are each refused, and the client keeps its last good copy. A new admin signed by an existing admin is accepted; one signed by nobody is not.
- **Accounts:** Panel creates an account per person, adds the key, records (member, server, account), and removal clears all of it; an unreachable server is reported and kept in the list.
- `syncgit`: real-`git` scenarios (bare repositories) with two admins: simultaneous edits, delete against edit, rejected push, a long-offline admin, a fresh clone taking the remote as base.
- A property test for the admin merge: the result converges whatever the order of syncs.
- Versions: an older client opens a newer repository read-only.
- Panel: the key install is idempotent, removal clears every recorded server, an unreachable server is reported and not forgotten.
- Host keys: a mismatch with the admin-verified key refuses the connection.
- `archtest`: no package reads another profile's folder.
- Strings for every new screen in all four languages.
- **A person with cryptography experience reviews M2 and M3** before they are called stable. The design uses `age` as it is and no hand-made construction.

## Risks

| Risk | Handling |
| --- | --- |
| The same SSH key opens spaces and logs in to servers | `age` supports it. A separate key per purpose is the alternative if a reviewer prefers it |
| Someone with write access forges or rolls back the team | Admin signature, pinned admin keys, file hashes and a `version` check (M2) |
| An admin's key is stolen | The admin is removed by another admin; at least two admins; keep the key with a passphrase or on hardware |
| Members share one Unix account on a server | Per-person accounts by default for prod hosts; a warning where a shared account is chosen (§5) |
| Onboarding many servers is slow | Panel runs it in one pass; certificates (M5) remove the per-server work |
| A server that only takes a shared password | Not supported in a Team profile; said up front |
| A bug in the admin merge loses data | Property test and two-admin tests from M2; members never write, so the blast radius is two or three machines |
| The repository grows (whole-file rewrite, ciphertext does not delta) | Only admins write, which is few commits; measure in M2; clients can fetch shallow; a per-record layout if a space grows large |
| A lost or stolen key | Replace the key (§4); a stolen key is treated as a removal |
| Mixed app versions | `format` and `min_client`; older clients read only |
| More surface in a beta | Experimental flag; Personal untouched; a pilot before M5 |

## Non-goals

- SSO, SAML, SCIM and an SLA of our own: access to the repository is controlled by the Git host (for example GitHub SAML SSO).
- Members who edit, or who add hosts to the team.
- Shared credentials for servers that only take a password.
- Per-record permissions inside a space.
- A server of ours, a hosted sync or real-time collaboration.
- Proving who connected to a server (that is the server's log).
- Recovery of a lost private key.

## Alternatives considered

![The three designs compared](diagrams/06-options.svg)

*Figure 6. Option 3 is chosen: no shared password at all, members read-only, and a key per person.*

| | Way | Read control | Write control | Cost |
| --- | --- | --- | --- | --- |
| **1** | One repository per space, a password each | Git host and encryption | Git host | One profile and one window per repository; shared passwords |
| **2** | One repository, a password per space (revision 4) | Encryption | None | Passwords to hand out and rotate; every holder can write; an admin slot that a non-admin cannot keep |
| **3** | **One repository, SSH-key recipients, members read-only** (this document) | Encryption per person | Git host | Needs key login on servers; the admin onboards each person |

Termius works in a similar way to option 3: a key per vault wrapped to each member's public key, with its cloud checking who may write. Here the Git host takes the place of that cloud, and the member's SSH key is both the decryption key and the login key.

## Changes from revision 4

| Change | Why |
| --- | --- |
| **No passwords**: spaces are age-encrypted to members' SSH public keys | No shared secret to hand out or rotate, no offline guessing, no admin password to protect |
| **Members read-only, admins write** (Git host permissions) | The team connects without being able to view or edit; no per-record signatures needed |
| Removal is *re-encrypt, remove the key from servers, revoke read access* | Nobody else has to enter or receive anything |
| Credentials per person is the default; no shared credential | A shared credential on a member's machine can always be extracted |
| Shared kubeconfig dropped | It is a shared credential |
| Admin-verified host keys in the space | A member never has to trust a server on first use |
| Conflicts only between admins; members replace their copy | Two or three writers instead of twenty; no tombstone growth for members |
| Server onboarding (Panel key install, later certificates) added as M4 and M5 | It is what makes "connect with no shared password" real |
| **Admin signature** on `team.json` with file hashes and `version` (moved from *Later* to M2) | `age` does not prove who made a file, so anyone who could write could forge a host list or roll the team back |
| **Which Unix account** a member logs in as is explained and made a setting per host; per person by default for prod | A key is not limited access: a shared account lets every member do whatever that account can |

## Findings of the review of revision 4, and where they went

| Finding | In this revision |
| --- | --- |
| A non-admin changing a space password drops the admin's slot | There are no slots: an admin is always in the recipient list and members cannot write |
| The repository grows because every commit rewrites a whole encrypted file | Only admins write; measure in M2; shallow fetch; a per-record layout is a *Later* item |
| Tombstones and a fresh clone's `base_rev` were undefined | Tombstones are kept (few writers); a fresh clone takes the remote as base; members replace |
| The admin password is a juicy offline target | There is no password |
| Offboarding means handing out new passwords | It does not |
| Trying a password against every slot is slow | No passwords |

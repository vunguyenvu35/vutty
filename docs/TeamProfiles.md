# Profiles: Personal and Team

Status: **design (revision 5), nothing built yet.**

Decisions taken:

- Teams of 15 to 20 people. A few **admins** manage the team; everyone else is a **member who can only connect**: they cannot view credentials, edit hosts or change the team.
- The **admin creates the private Git repository**. Git is the shared database and **everything in it is encrypted on each client**.
- **No shared passwords at all.** Every person has their own SSH key. A member's key opens the spaces they were given and logs in to the servers; nobody hands anyone a secret.
- **Members have read-only access** to the repository on the Git host; only admins can write. The Git host enforces it. This is a sharing model from the admin: members do not write.
- Servers accept **key login**. A server that only takes a shared password is not supported in a Team profile, because it cannot be "connect only".
- Keys are installed on servers through Panel first; **SSH certificates** come later (M5).
- Cluster credentials are per person, like server keys (§5).

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
- Trust sits with the admins and with the Git host's permissions. If a member were given write access by mistake they could add themselves to a space they can already read, or change a host they can read; the *Later* list has an admin-signed `team.json` for that case.

## 3. Repository format

Personal keeps `bundle.enc`. A Team repository has:

```
team.json                format, min_client, team id, version,
                         members [{id, name, SSH public key}],
                         spaces [{id, recipients: [member ids]}]
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
- **SSH certificates (later).** An admin signs a short-lived certificate for each member (`ssh-keygen -s`), the servers trust the team's certificate authority once (`TrustedUserCAKeys`), and certificates are public files that can sit in the repository. Removal is the end of the lifetime, so nothing has to reach every server. This scales better than installing keys on many servers.
- **Servers that only take a shared password** cannot be made connect-only: a client that can log in holds the credential. They are not supported in a Team profile; the setup says so.
- **Kubernetes.** A cluster in a space holds its address and certificate authority only. Each member uses **their own credential** for it (a client certificate or token issued to that person, bound on their machine). The earlier idea of a shared kubeconfig is dropped, because a shared token is a credential a member could read.
- **Host keys.** The admin verifies each server's host key once and stores it in the space. A member's client uses those and refuses a mismatch, so a new member never has to trust a host on first use.

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
- **Write protection is the Git host's.** If a member is given write access by mistake the team's membership is no longer protected.
- **Removing someone does not erase what they saw** (the host list, up to the day they were removed). It does take away their access.
- **No recovery of a private key.** A new key replaces a lost one.
- **Not real time.** Members see a change after the next sync.
- **No SSO, no SLA, no compliance reports** of our own. Access to the repository is the Git host's.

## Milestones

| | What | Done when |
| --- | --- | --- |
| **M0** | Spike (done): process per profile, see §1 | this document |
| **M1** | `profiles.json`, *Open profile…*, create, rename, delete, per-profile lock, `--profile`, `show` action, Team processes skip self-update and `ssh://`, *Copy to another profile…* | two profiles run side by side; Personal behaves as before; the existing tests pass. Useful alone: Work and Personal apart |
| **M2** | Team core: `team.json`, `age` with SSH recipients, space files, **member pull-only sync**, admin merge with `Rev`, `base_rev`, clock guard, Conflicts list, `format` and `min_client` | a member opens only their spaces; two admins converge with no silent loss; a member's client never writes |
| **M3** | Members: key generate or import, join, add to spaces, remove (re-encrypt), lost key | a removed member cannot open the new files; others do nothing |
| **M4** | Servers: Panel installs and removes a member's key and records where; admin-verified host keys in the space; per-person Kubernetes credentials | a member connects with their own key; removal clears every recorded server |
| **Pilot** | One or two real teams use it for a few weeks | what hurts is known before more is built |
| **M5** | SSH certificates (short-lived, one trusted authority per server) | removal by expiry, no per-server work |
| **Later** | Admin-signed `team.json`; a per-record file layout if a space grows large; per-space write control; a shared credential with an explicit warning for legacy servers | on demand |

M1 helps people with one profile too. M2 changes the format of Team repositories only; moving Personal to the same format is a separate decision for later. M2 onward is labelled experimental until reviewed.

## Tests

- `age` round trip with an `ssh-ed25519` identity and several recipients; a file opens for a recipient and **not** for a removed one (re-encrypted file); an `ssh-rsa` key also works.
- A client that is not a recipient never shows the space and never rewrites its file (byte-for-byte check).
- A member's client **never pushes**; a push by a member is refused by the Git host (a test with a read-only remote).
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
| A member given write access by mistake | Stated in the limits; an admin-signed `team.json` is on the *Later* list |
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

## Findings of the review of revision 4, and where they went

| Finding | In this revision |
| --- | --- |
| A non-admin changing a space password drops the admin's slot | There are no slots: an admin is always in the recipient list and members cannot write |
| The repository grows because every commit rewrites a whole encrypted file | Only admins write; measure in M2; shallow fetch; a per-record layout is a *Later* item |
| Tombstones and a fresh clone's `base_rev` were undefined | Tombstones are kept (few writers); a fresh clone takes the remote as base; members replace |
| The admin password is a juicy offline target | There is no password |
| Offboarding means handing out new passwords | It does not |
| Trying a password against every slot is slow | No passwords |

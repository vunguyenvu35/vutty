# Profiles: Personal and Team

Status: **design (revision 4), nothing built yet.**

Decisions taken:

- Teams of 15 to 20 people, who trust each other.
- The **admin creates the private Git repository**; Git is the shared database; **everything in it is encrypted on each client**.
- A member sees **only the spaces the admin gives them** (a space has its own password, §2 and §3). To remove someone from a space the admin changes that space's password.
- The team's kubeconfigs are shared with the whole space they belong to.

Proposed, waiting for confirmation: **spaces inside one repository, one password per space** (the choice among the three ways in [Alternatives](#alternatives-considered)).

## Goal

Let a team share hosts, groups, snippets, port forwards, clusters and shared credentials through **their own private Git repository**, with no server of ours, and keep a person's own vault separate from the team's. A person has one **Personal** profile and any number of **Team** profiles.

What this gives a team: shared hosts and credentials with a part for each group of people, on the Git host the team already pays for. What it does not give: SOC 2, an SLA, SAML on our side, or write permissions per space. See [Limitations](#limitations-to-tell-the-user) and [Non-goals](#non-goals).

## Rules that keep the beta safe

- **Personal does not change.** Same folder, same `bundle.enc`, same vault. No migration.
- Everything new applies to Team profiles only and sits behind *Settings → Team profiles (experimental)*.
- Each step ships on its own and says in the release notes where it was tried.
- A team repository records its format and the **oldest client allowed to write** to it (§2), so an old beta cannot damage a repository that a newer one already upgraded.
- A client **never rewrites a space it cannot open**. Its file is left exactly as it is.

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

- *Open profile…* starts `vutty --profile <id>`, which sets the data folder and runs the normal `Build`. If that profile's process already answers on its bridge, the existing window is shown instead (new bridge action `show`).
- A **lock file** per profile folder keeps two processes off the same SQLite file.
- Only the Personal process does self-update and handles `ssh://` links; Team processes skip both.
- Costs we accept: two windows (Personal and a Team), no single sidebar, auto-lock and sleep-lock per process, no drag of a host between profiles. In return no vault of one profile is ever readable from the process of another. Several **spaces of one team share one window** (§2).


![Personal and Team profiles as two processes on one machine](diagrams/01-profile-processes.svg)

*Figure 1. Two processes, no shared memory or vault. Only the Team process talks to the team repository.*

### Moving data between profiles

The processes do not share memory, so copying hosts between Personal and a Team profile goes through what already exists: **export the selected hosts** (plain JSON for hosts without secrets, or an encrypted file with its own password for hosts with secrets) and **import** them in the other profile. A *Copy to another profile…* button wraps this. Nothing is moved automatically.

### Leaving a team

*Leave team* deletes the local profile folder and its keychain entry. It does not change the repository. The admin removes the person's access and changes the password of each space they could open (§3).

## 2. Spaces and the repository format

A **space** is the unit of sharing: for example `prod`, `staging`, `client-acme`. A space has its own key, its own password, and holds the hosts, groups, snippets, forwards, clusters, identities and secrets that belong to it.

Personal keeps `bundle.enc`. A Team repository has:

```
team.json            format, min_client, team id, version, and for each space:
                     id, epoch, slots [{type, KDF parameters, wrapped space key}], key check
spaces/<id>.enc      the space's data, sealed (name, groups, hosts, forwards, snippets, clusters, identities, secrets)
audit/<space>/<member>/<yyyy-mm>.enc    append-only change log, metadata only (milestone M5)
```

- **Space names are sealed inside the space**, not in `team.json`, so a leaked repository does not show client or project names. The file names are random ids.
- **Slots.** Each space has two kinds of slot, both wrapping the same space key with Argon2id and AES-256-GCM as `internal/vault` does today:
  - `password`: the space's password, given to its members;
  - `admin`: the **admin password**, which opens every space. The admin remembers one password. The slot list carries a `type` so another kind of slot can be added later without a format change.
- **Opening.** A member enters the password of a space. The app tries it against the slots of the spaces it cannot open yet (a few seconds at most, since each try is an Argon2id run) and opens the one that matches. Spaces without a password simply do not appear.
- **Rules inside a space.** A host belongs to exactly one space. Its jump hosts and its credentials must be in the **same space**, so a member of that space is never sent to something they cannot read. A group belongs to one space.
- Records are sealed with the space key (AES-256-GCM, record id and epoch as associated data). Inside the file the client still merges record by record (fetch, decrypt, merge, commit on top of `origin`, push, retry on rejection, as `syncgit` does now).
- A file per space also keeps a sync small: only the spaces that changed are rewritten and pushed.
- Preferences are **not** synced in a Team profile. `Settings` is per profile.
- The branch is configurable (default `main`). A protected branch that refuses direct pushes does not work with this design; the setup screen says so.
- **There is no password hint in `team.json`.** The Personal vault header keeps its hint; a team header does not.


![Passwords open slots in team.json, slots give the space key, the key seals the space file](diagrams/02-keys-and-slots.svg)

*Figure 2. How a password reaches a sealed space file. The admin slot opens every space.*

### Format and client versions

`team.json` carries `format` and `min_client`. A client older than `min_client` opens the team **read-only** and says why; a client never writes a format newer than it understands. This matters for 15 to 20 people on a beta, who will not all update on the same day.

### Revisions and conflicts

![Data flow of one sync](diagrams/03-sync-data-flow.svg)

*Figure 3. One sync. A space the client has no key for is left byte for byte as it is.*


`Meta` gains `Rev` (a hybrid logical clock: wall time, counter, member label) and `UpdatedBy`. `UpdatedAt` stays for compatibility and display. `UpdatedBy` is the name the member set in their profile; it is a label, not proof, and Git's author on the commit says the same.

Each machine keeps, per record, the `rev` it last synced (`base_rev`). On a sync:

- only the remote side moved since `base_rev` → take it;
- only the local side moved → push it;
- **both moved** → keep the one with the higher `Rev`, and put the other in a **Conflicts** list (who, when, what) that can be restored with one click. Nothing is overwritten silently;
- a tombstone beats an edit only when its `Rev` is higher, as today.

A `Rev` whose wall time is more than a day ahead of this machine's clock is **refused** and reported, so one computer with a wrong clock cannot make its edits win forever.

This removes the clock-skew and merge-window gaps (G5 and the clock risk in [Architecture.md](Architecture.md#7-importexport-and-sync)) for Team profiles; the same code can later serve Personal.

## 3. Passwords, members and removal

There is **one password per space**, one admin password, and **no list of members inside Vutty**. The Git host decides who can read the repository; the passwords decide who can open what.

### Creating a team (the admin)

1. The admin makes a private repository on their Git host and gives the other people access there.
2. In Vutty: *New team profile* with the repository URL. The app generates the **admin password** (random, at least 80 bits) and shows it once with a copy button, and tells the admin where to keep it: it cannot be recovered.
3. *New space*: a name, and the app generates the **space password** the same way and shows it once. The admin gives it to the people who should open that space, over a channel the admin trusts.

A password the admin types instead is checked for **strength**, not only length (16 characters at least, and a check against common patterns and words). Generated passwords are the default.

### Joining (a member)

![Sequence: create the team, give out a space password, join and open a space](diagrams/04-join-and-open-space.svg)

*Figure 4. Creating a team, giving out a space password (outside Git), and joining.*


1. The admin gives them access to the repository on the Git host.
2. In Vutty: *Join a team* with the repository URL, then *Open a space* with the password of each space they were given. The team profile itself unlocks with a password the member sets for **their own machine** (also with Touch ID or Windows Hello once set); the space passwords are kept in the local vault.

### Removing someone from a space (the admin)

![Before and after changing a space password](diagrams/05-remove-from-space.svg)

*Figure 5. Removing someone from one space: new key, new password, one commit. What it cannot take back.*


1. If the person must lose the whole repository, remove them on the Git host. If they stay in the team but leave one space, nothing changes there.
2. In Vutty: *Change space password*. It makes a **new space key and a new password**, seals that space again, bumps its `epoch` and the `version`, and pushes **one commit**. Other spaces are not touched.
3. The app lists **what that person could read** in that space (hosts and secrets, per host) as a checklist: *change these credentials on the servers*. It cannot do that for the admin, and the page says so.
4. The admin gives the new password to the people who stay. On their next sync a member sees *"The password of «prod» was changed. Enter the new one."*, types it, and their pending edits (made under the old key) are sealed again with the new one.

Changing a password does not undo the past: the person already read the old data, their machine keeps its local copy, and old commits stay in Git history. The page says this plainly.

### Who may change a password

Anyone who knows a space's password and can write to the repository can change it. The team trusts those people, and the page says so. If someone changes it by mistake or against the team's wishes, the admin can `git revert` the commit (or re-open the space through the admin slot); the change shows in `git log` and in the audit files (M5). A client refuses a `team.json` whose `version` is **lower** than one it has already seen, so an old header cannot be put back to bring back an old password.

### Forgotten passwords

A member who lost a space password asks the admin. If the admin password is also gone, no space can be recovered: there is no recovery, by design, as for a vault.

## 4. If the repository leaks

Everything in the repository is encrypted on the clients: the data of every space is sealed with AES-256-GCM, and the space keys are wrapped by Argon2id from the passwords. What is **not** hidden:

| What leaks | What it means |
| --- | --- |
| The slots in `team.json` (salt, Argon2id parameters, wrapped key) | Someone can try passwords **offline**, with no limit on tries. A generated password of at least 80 bits makes this pointless; a weak password chosen by a person may be found even with Argon2id (64 MiB, 3 passes). This is why generated passwords are the default and a typed one is checked for strength |
| **Metadata**: how many spaces, how big each file is, when each commit was made, the commit authors, the file names (random ids) | The shape of the team shows, not what is in it. A generic commit author can be set in the profile to hide names |
| **History** | Old ciphertext stays in Git. If a password was changed later, someone who has the repository **and the old password** can still read what was in it at that time |

What to do if the repository (or the Git host account) leaked:

1. Treat it as a new repository: change the password of every space and the admin password.
2. Change the credentials stored in the spaces on the servers, because the old history can still be opened with the old passwords.
3. Review who has access to the repository on the Git host.

A leak of the repository **and** a password is a leak of that space, as for any encryption.

## 5. Credentials per person, and shared ones

A host in a Team profile picks one of:

- **Shared credential**: sealed with the space key; every member of that space can use it. Shared kubeconfigs are the same (decided): they sit in the space and every member of it can use them. The UI says so on the cluster, and a per-person kubeconfig stays on the roadmap.
- **Per member**: the host stores only a **slot name** (for example `deploy@prod`). Each member binds their own key or password to that name **on their machine only**; it is not synced and not in the repository. A member who has not bound it yet sees "choose your credential" at connect time. This is also what makes removal cheaper: a credential that was never shared does not need changing on the servers.

No second vault is ever opened inside the Team process.

## 6. Protection against a changed host

A hostile or compromised member can point a shared host at a server of their own to catch passwords. Mitigations that exist in this design (none is a guarantee):

- When a **different member** changes `hostname`, `port`, jump host or credential of a host, the next connection from another machine shows *"Alice changed the address of this host on 2 Dec"* and asks before connecting.
- `known_hosts` entries can be shared as records, so a new member starts with fingerprints that were already verified.
- `UpdatedBy`, the commit author and the audit files say who did it.

The first version **trusts everyone who has a space's password and write access**, and the page says so.

## Limitations to tell the user

These belong on the *New team* screen and in the user guide, in plain words:

- **For a small team that trusts each other.** Everyone who opens a space has the same rights in it: read, change, delete, change its password.
- **Read access is separated by space; write access is not.** Anyone who can write to the repository can delete or damage the file of a space they cannot open. It is detected and Git restores it, but it is not prevented. For hard separation, use a separate repository per team.
- **Removing someone does not take back what they saw.** Remove them on the Git host if needed, change the space password, and change the credentials on the servers.
- **A password can be passed on** by whoever has it.
- **No recovery.** If the admin password and a space password are both lost, that space is lost.
- **Not real time.** Changes appear after a sync; two edits of the same record end in the Conflicts list.
- **Keep one repository per trust boundary.** The shape of the team (how many spaces, how large) can be seen by everyone who can read the repository.
- No SSO, no SLA, no compliance reports of our own.

## Milestones

| | What | Done when |
| --- | --- | --- |
| **M0** | Spike (done): process per profile, see §1 | this document |
| **M1** | `profiles.json`, *Open profile…*, create, rename, delete, per-profile lock, `--profile`, `show` action, Team processes skip self-update and `ssh://`, *Copy to another profile…* | two profiles run side by side; Personal behaves as before; the existing tests pass. Useful alone: Work and Personal apart |
| **M2** | Team sync core: `Rev`, `UpdatedBy`, `base_rev`, clock guard, Conflicts list, `format` and `min_client`, one sealed file per space (one default space at first) | three-client scenarios converge with no silent loss |
| **M3** | Spaces: `team.json`, New team, New space, Open a space, admin slot, **Change space password** with rotation and the checklist, "password changed" prompt, `epoch` and `version` checks, never write a space that cannot be opened | a removed member cannot read new commits of that space; the others re-enter one password and lose nothing; an old header is refused; other spaces are untouched |
| **Pilot** | One or two real teams use it for a few weeks | what hurts is known before more is built |
| **M4** | Per-member credential slots, "changed by" warning on a host | two people use the same host with their own keys |
| **M5** | Audit files, shared `known_hosts` | the change log shows author and time |
| **Later** | One file per record; a per-person slot type (public key) if a team needs to revoke one person without handing out a new password; per-space write control | on demand |

M1 helps people with one profile too. M2 changes the sync format of Team repositories only; moving Personal to the same format is a separate decision for later. M3 onward is labelled experimental until reviewed.

## Tests

- `syncgit`: extend the real-`git` scenarios (bare repositories) to **three clients**: simultaneous edits, delete against edit, rejected push, a client that was offline for a long time, a client on an old epoch syncing after a password changed.
- A client that cannot open a space leaves that file **byte for byte** unchanged across a sync (a test with a space the client has no password for).
- A property test for the merge: the result converges whatever the order of syncs.
- Crypto: a wrong password, a tampered header or file, an old header put back (refused by `version`), a password change, a removed member unable to open the new epoch, an `admin` slot that opens every space, a `Rev` far in the future, no hint in `team.json`.
- Strength check of typed passwords.
- Versions: an older client opens a newer repository read-only.
- `archtest`: no package reads another profile's folder.
- Strings for every new screen in all four languages.
- **A person with cryptography experience reviews the password change and the slots (M3) before it is called stable.** It reuses the vault's existing code, which keeps the review small.

## Risks

| Risk | Handling |
| --- | --- |
| A password is guessed offline from a leaked repository | Generated by default, at least 80 bits; strength check for typed ones; §4 says what to do after a leak |
| Removal looks like revocation | Say plainly what it does not do; the checklist is part of the flow |
| A member changes a password against the team's wishes | Visible in `git log`; the admin reverts or re-opens through the admin slot; the page says that password holders are trusted |
| A member damages a space they cannot open | Detected by the seal, restored with Git; the limitation is stated; separate repositories for hard separation |
| Nobody remembers the passwords | No recovery by design; the setup screen says where to keep them |
| Mixed app versions across the team | `format` and `min_client` in the header; older clients read only |
| A bug loses data on a sync | Three-client tests and the property test from M2 on; a space that cannot be opened is never rewritten |
| The team repository grows | One commit only when something changed; one file per space; measure at 20 members before M3 |
| Whole-space secrets (shared kubeconfig) | Shown on the cluster; per-person kubeconfig on the roadmap |
| More surface in a beta | Experimental flag; Personal untouched; a pilot before M4 |

## Non-goals

- SSO, SAML, SCIM and an SLA of our own: access to the repository is controlled by the Git host (for example GitHub SAML SSO).
- Roles and a member list inside Vutty; a different password or key per person (a possible later slot type).
- Write permissions per space, or per record (a member of a space can do everything in it).
- A server, a hosted sync or real-time collaboration.
- Proving who changed a record (a label and Git's author, not a signature).
- Recovery of lost passwords.
- Hiding the shape of the team from people who can read the repository.

## Alternatives considered

![The three ways to separate access](diagrams/06-options.svg)

*Figure 6. Option 1 uses one repository per space, option 2 (chosen) one repository with a password per space, option 3 a key per person.*


| | Way | Read control | Write control | Cost |
| --- | --- | --- | --- | --- |
| **1** | **One repository per space**, one password each | Git host and encryption | Git host | Lowest; no new cryptography. Costs one profile and one window per repository, because a profile is a process (§1). Best when the spaces must not know about each other |
| **2** | **Spaces in one repository, one password each** (this document) | Encryption | None | Medium; one window for all the spaces of a team |
| **3** | A key pair per person and a signed access list | Encryption, per person | Signatures per record | Highest; revokes one person without a new password for the others. Was revision 2 of this document |

Option 2 is chosen as the middle road. The slot `type` and the per-space files leave the way open to option 3 later.

## Changes from revision 3

| Change | Why |
| --- | --- |
| **Spaces**, one password each, plus an admin password | A member sees only what the admin gives them, with no key pairs |
| One sealed file per space (replaces "one file per record") | The unit of access is the space; a sync rewrites only what changed |
| Space names sealed; a client never rewrites a space it cannot open | A leak does not show client names; a member without a password cannot damage a space by syncing |
| Jump hosts and credentials must be in the same space | A member is never sent through something they cannot read |
| New section: if the repository leaks | Says what is hidden, what is not, and what to do |
| New section: limitations to tell the user | Plain words for the *New team* screen |
| No hint in `team.json`; strength check for typed passwords | The hint is public in a repository; weak passwords are the weak point of offline guessing |
| Milestones reordered: M2 is the sync core, M3 the spaces, then a pilot; one file per record moved to "Later" | Smaller steps, learn from real teams before building more |
| Alternatives section | The three options and why option 2 |

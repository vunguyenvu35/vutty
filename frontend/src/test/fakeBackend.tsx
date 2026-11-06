import {ReactNode} from 'react'
import {BackendContext, Backend} from '../lib/backend'
import {ToastProvider} from '../lib/ui/Toast'
import {DEFAULT_SETTINGS} from '../lib/settings/defaults'
import {k8sApi, makeK8s, type K8sFake} from './fakeK8s'
import type {ServerUsers, UserKey, DockerObjects, ExecRun, ServerBackup, ServerCerts, ServerCleanup, ServerContainers, ServerFleetHost, ServerFolder, ServerSchedule, ServerSecurity, ServerUpdates, ServerWeb, ServerWebResult, ServerOverview, ServerPorts, ServerProcess, ServerUnit, SSHLink, VaultHealth, ActivityEntry, KnownHost, QueueJob, RemoteEntry, SyncDiff, TransferResult, ConnectionTest, ForwardStatus, Group, Host, Identity, PortForward, Settings, Snippet, VaultStatus,
  CacheReport, AiCommandCheck, AiModelList, AiPreview, AiProvider, AiReply, AiSessionState, AiSettings, McpImportItem, McpServer, McpTool, Cluster, ClusterPreview} from '../lib/types'

export interface Fake extends Backend {
  state: {
    vault: VaultStatus
    password: string
    groups: Group[]
    hosts: Host[]
    identities: Identity[]
    forwards: PortForward[]
    snippets: Snippet[]
    running: Record<string, string>
    settings: Settings
    /** the newest release GitHub reports (a test sets it higher than the running version to get an update) */
    latest?: string
    /** remote file system for SFTP: path -> entry ("/" is the root) */
    remote: Record<string, {dir: boolean; size: number}>
    /** this computer's files for the Local pane: path -> entry */
    local: Record<string, {dir: boolean; size: number}>
    /** what the shell history of the server holds */
    hostHistory: string[]
    /** what Clear cache reports */
    cache: CacheReport
    /** serial devices the Serial panel finds */
    serialPorts: string[]
    /** the system clipboard */
    clipboard: string
    knownHosts: KnownHost[]
    /** true: queued transfers stay running (40 %) until a test finishes them with finishJob */
    holdQueue: boolean
    /** what session.cwd answers: the folder a local shell is in */
    shellCwd: string
    /** the activity log, newest first */
    logs: ActivityEntry[]
    testResult: ConnectionTest
    /** what the vault health check finds (default: nothing wrong) */
    health?: VaultHealth
    /** what Install and restart is told (default: this copy cannot update itself) */
    selfUpdate?: {supported: boolean; reason?: string}
    /** the reason installUpdate fails with, when a test wants it to */
    installError?: string
    /** an ssh:// link that arrived before the screen looked (taking it empties this) */
    pendingSSHLink?: SSHLink
    /** runs of a command on several hosts: a test moves them along by emitting `exec:update:<id>` */
    execRuns: Record<string, ExecRun>
    calls: Array<[string, ...unknown[]]>
    /** the Server section: what the server answers; an action that is in failAction fails with that message */
    server: {
      overview: ServerOverview
      processes: ServerProcess[]
      units: ServerUnit[]
      docker: ServerContainers
      dockerObjects: DockerObjects
      logs: string
      ports: ServerPorts
      certs: ServerCerts
      web: ServerWeb
      webResult: ServerWebResult
      updates: ServerUpdates
      cleanup: ServerCleanup
      folders: Record<string, ServerFolder[]>
      security: ServerSecurity
      users: ServerUsers
      userKeys: Record<string, UserKey[]>
      schedule: ServerSchedule
      backup: ServerBackup
      /** what fleetHost answers per host id: an error text means the host did not answer */
      fleetErrors: Record<string, string>
      failAction: string
      openError: string
    }
    /** the Kubernetes tab: the cluster the fake answers for (see fakeK8s.ts) */
    k8s: K8sFake
    /** the clusters reached with a kubeconfig; `preview` is what any kubeconfig the person picks holds, `previewError` makes the preview fail */
    clusters: Cluster[]
    preview: ClusterPreview
    previewError: string
    /** the AI assistant */
    ai: {
      providers: AiProvider[]
      models: Record<string, AiModelList>
      settings: AiSettings
      info: AiSessionState
      preview: AiPreview
      /** provider id -> the reason enabling it fails */
      enableError: Record<string, string>
      reply: AiReply
      /** the ids of the questions asked so far */
      asked: string[]
      mcp: McpServer[]
      tools: Record<string, McpTool[]>
      importable: McpImportItem[]
      /** while set, models() waits for it (a provider that is slow to answer) */
      modelsGate: Promise<void> | null
      /** how many providers() calls still answer that the tools are being looked for (state '') */
      checking: number
      /** what checkCommand answers for a command (default: low risk, everything allowed) */
      check: (cmd: string) => AiCommandCheck
    }
  }
  emit(event: string, data?: unknown): void
  /** Sets a queued job to a state (default done) and sends the snapshot, like the Go side does when a transfer ends. */
  finishJob(sid: string, jobId: string, patch?: Partial<QueueJob>): void
  /** the jobs of a session's queue */
  jobs(sid: string): QueueJob[]
}

const GB = 1024 ** 3
let n = 0

type Tree = Record<string, {dir: boolean; size: number}>
/** Copies files and folders between two in-memory trees, skipping files that exist unless overwrite. */
function copyTree(from: Tree, to: Tree, sources: string[], destDir: string, overwrite: boolean, done: (name: string) => void): TransferResult {
  const res: TransferResult = {files: 0, dirs: 0, bytes: 0, skipped: [], ignored: []}
  const join = (d: string, name: string) => (d === '/' ? '' : d) + '/' + name
  for (const src of sources) {
    if (!from[src]) throw 'no such file or folder: ' + src
    const name = src.slice(src.lastIndexOf('/') + 1)
    const inside = Object.keys(from).filter(p => p === src || p.startsWith(src + '/')).sort()
    for (const p of inside) {
      const rel = name + p.slice(src.length)
      const dst = join(destDir, rel)
      if (from[p].dir) {
        if (!to[dst]) to[dst] = {dir: true, size: 0}
        res.dirs++
      } else if (to[dst] && !overwrite) {
        res.skipped.push(rel)
      } else {
        to[dst] = {dir: false, size: from[p].size}
        res.files++
        res.bytes += from[p].size
      }
    }
    done(name)
  }
  return res
}
/** What syncing would copy from one tree into the other: files that are missing or have another size. */
function syncDiff(from: Tree, to: Tree, src: string, dst: string): SyncDiff {
  const files = (t: Tree, root: string) => Object.entries(t).filter(([p, e]) => !e.dir && p.startsWith(root === '/' ? '/' : root + '/')).map(([p, e]) => ({rel: p.slice(root === '/' ? 1 : root.length + 1), size: e.size}))
  const there = new Map(files(to, dst).map(f => [f.rel, f.size]))
  const out: SyncDiff = {copy: [], same: 0, extra: []}
  const have = new Set<string>()
  for (const f of files(from, src).sort((a, b) => a.rel.localeCompare(b.rel))) {
    have.add(f.rel)
    const t = there.get(f.rel)
    if (t === undefined) out.copy.push({path: f.rel, size: f.size, reason: 'new'})
    else if (t !== f.size) out.copy.push({path: f.rel, size: f.size, reason: 'changed'})
    else out.same++
  }
  out.extra = [...there.keys()].filter(r => !have.has(r)).sort()
  return out
}
const id = (p: string) => `${p}${++n}`

/** In-memory Backend that behaves like the Go side closely enough for UI tests. */
export function makeFake(init: Partial<Fake['state']> = {}): Fake {
  const listeners = new Map<string, Set<(d: any) => void>>()
  const state: Fake['state'] = {
    vault: {exists: true, unlocked: false, keychain_enabled: false, auto_lock_minutes: 15},
    password: 'correct horse battery',
    groups: [], hosts: [], identities: [], forwards: [], snippets: [], running: {},
    settings: {...DEFAULT_SETTINGS, font_family: 'monospace'},
    remote: {'/': {dir: true, size: 0}, '/projects': {dir: true, size: 0}, '/readme.txt': {dir: false, size: 1536}, '/projects/app.log': {dir: false, size: 20480}},
    local: {
      '/': {dir: true, size: 0}, '/Users': {dir: true, size: 0}, '/Users/me': {dir: true, size: 0}, '/Users/me/Documents': {dir: true, size: 0},
      '/Users/me/notes.txt': {dir: false, size: 300}, '/Users/me/.zshrc': {dir: false, size: 120}, '/Users/me/Documents/cv.pdf': {dir: false, size: 40960},
    },
    holdQueue: false,
    shellCwd: '',
    hostHistory: [],
    cache: {files: 3, bytes: 5120},
    serialPorts: ['/dev/cu.usbserial-1410', '/dev/cu.usbmodem1101'],
    clipboard: '',
    knownHosts: [],
    logs: [],
    testResult: {ok: true, message: 'Connected to 10.0.0.1 as root', server_version: 'SSH-2.0-OpenSSH_9.6', latency_ms: 42, hops: 1},
    calls: [],
    execRuns: {},
    server: {
      overview: {os: 'Ubuntu 24.04 LTS', kernel: '6.8.0-45-generic', cores: 4, uptime_sec: 3542400, cpu_pct: 31.5, load: [0.84, 0.71, 0.66], mem_total: 8 * GB, mem_used: 5 * GB, swap_total: 2 * GB, swap_used: GB / 4,
        disk_total: 50 * GB, disk_used: 39 * GB, net_rx: 120000, net_tx: 45000, root: false},
      processes: [{pid: 2210, user: 'app', cpu: 38.5, mem: 12.4, command: 'node server.js'}, {pid: 3011, user: 'redis', cpu: 1.3, mem: 2.6, command: 'redis-server *:6379'}],
      units: [{name: 'app-worker', description: 'Job queue', active: 'failed', sub: 'failed', enabled: 'enabled'}, {name: 'nginx', description: 'Web server', active: 'active', sub: 'running', enabled: 'enabled'}],
      docker: {installed: true, containers: [{id: 'a1', name: 'api', image: 'registry/api:1.8', state: 'running', status: 'Up 2 hours', ports: '0.0.0.0:3000->3000/tcp', project: 'shop'}, {id: 'c3', name: 'db', image: 'postgres:16', state: 'running', status: 'Up 2 hours', ports: '', project: 'shop'}, {id: 'b2', name: 'old-batch', image: 'registry/batch', state: 'exited', status: 'Exited (0) 3 days ago', ports: '', project: ''}]},
      dockerObjects: {images: [{id: 'aa11bb22', name: 'registry/api:1.8', size: '210MB', created: '2 days ago', in_use: true}, {id: 'cc33dd44', name: '<none>', size: '95MB', created: '1 month ago', in_use: false}], volumes: [{name: 'pgdata', driver: 'local', in_use: true}, {name: 'old-cache', driver: 'local', in_use: false}], networks: [{id: 'n1', name: 'bridge', driver: 'bridge', scope: 'local'}, {id: 'n2', name: 'shop_default', driver: 'bridge', scope: 'local'}]},
      logs: '2026-11-04T09:41:07+0700 host app-worker[2210]: Error: ECONNREFUSED 127.0.0.1:6380\n2026-11-04T09:41:12+0700 host sshd[88]: Accepted publickey for ubuntu',
      ports: {firewall: 'active', ports: [{port: 22, process: 'sshd', local: false, open: true}, {port: 5432, process: 'postgres', local: true, open: false}, {port: 9000, process: 'minio', local: false, open: false}], rules: [{num: 1, to: '22/tcp', action: 'ALLOW', from: 'Anywhere'}]},
      certs: {found: true, certs: [{name: 'example.com', domains: ['example.com', 'www.example.com'], not_after: 1788000000, days_left: 9}, {name: 'api.example.com', domains: ['api.example.com'], not_after: 1795000000, days_left: 88}]},
      web: {kind: 'nginx', version: 'nginx/1.24.0', running: true, sites: [{names: ['example.com', 'www.example.com'], listen: ['443 ssl'], targets: ['proxy_pass http://127.0.0.1:3000']}]},
      webResult: {ok: true, output: 'nginx: configuration file /etc/nginx/nginx.conf test is successful'},
      updates: {manager: 'apt', count: 12, security: 3, reboot_required: true, lists_age_sec: 3 * 86400, packages: [{name: 'openssl', version: '3.0.13-0ubuntu3.5', security: true}, {name: 'curl', version: '8.5.0-2ubuntu10.6', security: false}]},
      cleanup: {disks: [{mount: '/', device: '/dev/sda1', total: 50 * GB, used: 39 * GB}], items: [{id: 'docker-dangling', size: GB, note: 'Dangling images and build cache'}, {id: 'journal', size: GB / 2, note: 'Journal'}, {id: 'docker-volumes', size: 3 * GB, note: 'Volumes'}]},
      folders: {'/var': [{path: '/var/lib', size: 12 * GB}, {path: '/var/log', size: 2 * GB}], '/var/lib': [{path: '/var/lib/docker', size: 10 * GB}]},
      users: {users: [{name: 'root', uid: 0, home: '/root', shell: '/bin/bash', sudo: true, keys: 1, login: 'Thu Nov  5 09:00:00 +0700 2026'}, {name: 'deploy', uid: 1001, home: '/home/deploy', shell: '/bin/bash', sudo: false, keys: -1, login: ''}], sessions: [{user: 'root', tty: 'pts/0', since: '2026-11-05 09:00', from: '203.0.113.7'}], me: 'root'},
      userKeys: {deploy: [{type: 'ssh-ed25519', fingerprint: 'SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', comment: 'old@laptop', options: false}]},
      security: {fail2ban: {installed: true, running: true, jails: [{name: 'sshd', banned: 2, total_banned: 41, ips: ['203.0.113.5', '198.51.100.7']}]}, ssh: {known: true, port: 22, root_login: 'yes', password_auth: 'yes'}, failed: {known: true, total: 240, top: [{ip: '203.0.113.9', count: 180}]}},
      schedule: {timers: [{name: 'backup.timer', description: 'Nightly backup', unit: 'backup.service', next: 'Thu 2026-11-05 03:00:00 UTC', last: 'Wed 2026-11-04 03:00:01 UTC', result: 'success'}], cron: [{source: 'crontab', user: '', schedule: '0 4 * * *', command: '/usr/local/bin/cleanup.sh'}, {source: '/etc/cron.d/logrotate', user: 'root', schedule: '@daily', command: '/usr/sbin/logrotate'}], me: 'ubuntu'},
      backup: {found: true, file: 'db-2026-11-02.sql.gz', size: GB / 8, mtime: 1790000000 - 3 * 86400, now: 1790000000},
      fleetErrors: {},
      failAction: '',
      openError: '',
    },
    k8s: makeK8s(),
    clusters: [],
    preview: {contexts: [
      {name: 'prod', cluster: 'prod-cluster', server: 'https://10.0.0.11:6443', user: 'dev', current: true, auth: 'token'},
      {name: 'eks', cluster: 'eks-prod', server: 'https://ABC.gr7.eu-west-1.eks.amazonaws.com', user: 'eks-user', current: false, auth: 'exec', exec: 'aws eks get-token --cluster-name prod'},
    ], problems: {legacy: 'this kubeconfig reads a token from a file (tokenFile) of this computer and sends it to the server: ask for one with the token inside it'}},
    previewError: '',
    ai: {
      providers: [{id: 'claude', name: 'Claude Code', status: {state: 'ready', version: '2.1.0', detail: '', hint: ''}, enabled: true, default: true, model: '', supports_mcp: true, supports_images: true, custom: false}],
      models: {claude: {models: [{id: '', name: 'Default (CLI decides)'}, {id: 'opus', name: 'Opus 5.5'}, {id: 'sonnet', name: 'Sonnet 5.5'}], error: '', can_list: true}},
      settings: {lines: 200, patterns: [], auto_run: false, default_provider: 'claude'},
      info: {host_id: 'local', label: 'This computer', policy: 'on', stored: '', production: false, confirmed: false, connected: true},
      preview: {text: 'Host: This computer\n$ df -h\n/dev/sda1 98% /var', findings: [], redacted: 0, lines: 3, alt_screen: false, vault_locked: false, empty: false, images: 0},
      enableError: {},
      reply: {text: 'The disk is full.', commands: [{cmd: 'df -h', why: 'check usage', risk: 'low', reasons: [], warnings: [], blocked: false, block_reason: ''}], provider: 'claude', model: '', redacted: 0},
      asked: [], mcp: [], tools: {}, importable: [], modelsGate: null, checking: 0,
      check: () => ({blocked: false, block_reason: '', risk: 'low', reasons: [], warnings: [], run_allowed: true, run_reason: '', typed_confirm: false}),
    },
    ...init,
  }
  const rec = (name: string, ...a: unknown[]) => { state.calls.push([name, ...a]) }
  const emit = (e: string, d?: unknown) => listeners.get(e)?.forEach(cb => cb(d))
  // the transfer queue: one list per SFTP session; every change sends the whole list, like the Go side
  const queues: Record<string, QueueJob[]> = {}
  const push = (sid: string) => emit('sftp:queue:' + sid, (queues[sid] ?? []).map(j => ({...j})))
  const patchJob = (sid: string, jobId: string, patch: Partial<QueueJob>) => {
    const j = (queues[sid] ?? []).find(x => x.id === jobId)
    if (!j) return
    Object.assign(j, patch)
    push(sid)
  }
  const run = (sid: string, job: QueueJob, spec: {direction: QueueJob['direction']; sources: string[]; destDir: string; overwrite: boolean}) => {
    if (job.state !== 'queued') return
    const up = spec.direction === 'upload' || spec.direction === 'sync-up'
    const [from, to] = up ? [state.local, state.remote] : [state.remote, state.local]
    try {
      let res: TransferResult
      if (spec.direction === 'upload' || spec.direction === 'download') {
        res = copyTree(from, to, spec.sources, spec.destDir, spec.overwrite, () => {})
      } else {
        const src = spec.sources[0], dst = spec.destDir
        const diff = syncDiff(from, to, src, dst)
        res = {files: 0, dirs: 0, bytes: 0, skipped: [], ignored: []}
        for (const i of diff.copy) {
          const target = (dst === '/' ? '' : dst) + '/' + i.path
          for (let d = target.slice(0, target.lastIndexOf('/')); d && d !== '/'; d = d.slice(0, d.lastIndexOf('/'))) if (!to[d]) to[d] = {dir: true, size: 0}
          to[target] = {dir: false, size: i.size}
          res.files++
          res.bytes += i.size
        }
      }
      Object.assign(job, {...res, state: 'done', done: res.bytes, total: res.bytes})
    } catch (e) {
      Object.assign(job, {state: 'failed', error: String(e)})
    }
    push(sid)
  }
  const upsert = <T extends {id: string}>(list: T[], item: Partial<T>, prefix: string): T => {
    const full = {...item, id: item.id || id(prefix)} as T
    const i = list.findIndex(x => x.id === full.id)
    if (i >= 0) list[i] = full
    else list.push(full)
    return full
  }
  const fake: Fake = {
    state,
    emit,
    vault: {
      status: async () => ({...state.vault}),
      create: async (pw, hint) => { rec('vault.create', pw, ...(hint ? [hint] : [])); state.vault = {...state.vault, exists: true, unlocked: true, hint: hint || undefined} },
      setHint: async h => { rec('vault.setHint', h); state.vault = {...state.vault, hint: h.trim() || undefined} },
      unlock: async pw => {
        rec('vault.unlock', pw)
        if (pw !== state.password) throw 'Incorrect master password. Please check it and try again.'
        state.vault = {...state.vault, unlocked: true}
      },
      unlockBiometric: async () => { rec('vault.unlockBiometric'); state.vault = {...state.vault, unlocked: true} },
      lock: async () => { state.vault = {...state.vault, unlocked: false}; emit('vault:locked') },
      resetVault: async c => {
        rec('vault.resetVault', c)
        if (c.trim() !== 'RESET') throw 'type RESET to confirm'
        state.vault = {...state.vault, exists: false, unlocked: false, keychain_enabled: false}
        state.identities = state.identities.filter(i => i.auth_type === 'agent')
      },
      changePassword: async (o, nw) => { rec('vault.changePassword', o, nw); if (o !== state.password) throw 'Incorrect master password. Please check it and try again.'; state.password = nw },
      setAutoLock: async m => { rec('vault.setAutoLock', m); state.vault = {...state.vault, auto_lock_minutes: m} },
      enableBiometric: async () => { state.vault = {...state.vault, keychain_enabled: true} },
      disableBiometric: async () => { state.vault = {...state.vault, keychain_enabled: false} },
      listIdentities: async () => [...state.identities],
      health: async () => { rec('vault.health'); return state.health ?? {checked: state.identities.length, unreadable: []} },
      addKey: async (name, path, pass) => { rec('vault.addKey', name, path, pass); return upsert(state.identities, {name, auth_type: 'key', public_key: 'ssh-ed25519 AAAAimported'}, 'i') },
      addKeyPair: async (name, pem, pass, pub, cert) => { rec('vault.addKeyPair', name, pem, pass, pub, cert); return upsert(state.identities, {name, auth_type: 'key', public_key: pub.trim() || 'ssh-ed25519 AAAApasted', certificate: cert.trim() || undefined}, 'i') },
      addKeyPEM: async (name, pem, pass) => { rec('vault.addKeyPEM', name, pem, pass); return upsert(state.identities, {name, auth_type: 'key', public_key: 'ssh-ed25519 AAAApasted'}, 'i') },
      updateIdentity: async (id, name, user, secret, passphrase, publicKey, certificate) => {
        rec('vault.updateIdentity', id, name, user, secret, passphrase, publicKey, certificate)
        const x = state.identities.find(i => i.id === id)
        if (!x) throw 'not found'
        if (!name.trim()) throw 'identity name is required'
        if (secret && x.auth_type === 'key' && !secret.includes('PRIVATE KEY')) throw 'ssh: no key found'
        Object.assign(x, {name: name.trim(), username: user})
        return {...x}
      },
      addPassword: async (name, user, pw) => { rec('vault.addPassword', name, user, pw); return upsert(state.identities, {name, username: user, auth_type: 'password'}, 'i') },
      addAgent: async (name, user) => { rec('vault.addAgent', name, user); return upsert(state.identities, {name, username: user, auth_type: 'agent'}, 'i') },
      generateKey: async name => upsert(state.identities, {name, auth_type: 'key', public_key: 'ssh-ed25519 AAAAgenerated vutty'}, 'i'),
      deleteIdentity: async i => {
        if (state.hosts.some(h => h.identity_id === i)) throw 'identity is used by a host'
        state.identities = state.identities.filter(x => x.id !== i)
      },
    },
    hosts: {
      listGroups: async () => [...state.groups],
      saveGroup: async g => upsert(state.groups, {sort_order: 0, ...g}, 'g'),
      deleteGroup: async i => {
        const gone = new Set([i])
        for (let grew = true; grew;) { grew = false; for (const g of state.groups) if (g.parent_id && gone.has(g.parent_id) && !gone.has(g.id)) { gone.add(g.id); grew = true } }
        state.groups = state.groups.filter(g => !gone.has(g.id))
        state.hosts = state.hosts.filter(h => !gone.has(h.group_id ?? ""))
      },
      listHosts: async () => [...state.hosts],
      saveHost: async h => {
        rec('hosts.saveHost', h)
        if (!h.hostname) throw 'hostname is required'
        return upsert(state.hosts, {port: 22, keepalive_sec: 30, ...h, label: h.label || h.hostname}, 'h')
      },
      deleteHost: async i => { state.hosts = state.hosts.filter(h => h.id !== i) },
      setTelnetPassword: async (i, pw) => { rec('hosts.setTelnetPassword', i, pw) },
    },
    session: {
      open: async (h, c, r) => { rec('session.open', h, c, r); return id('s') },
      attach: async s => { rec('session.attach', s) },
      ack: async (s, n) => { rec('session.ack', s, n) },
      write: async (s, d) => { rec('session.write', s, d) },
      resize: async () => {},
      cwd: async s => { rec('session.cwd', s); return state.shellCwd },
      reconnect: async s => { rec('session.reconnect', s) },
      close: async s => { rec('session.close', s) },
      hostHistory: async s => { rec('session.hostHistory', s); return [...state.hostHistory] },
      trustHostKey: async (s, f) => { rec('session.trustHostKey', s, f) },
      answerAuth: async (k, a) => { rec('session.answerAuth', k, a) },
      testConnection: async (h, p, k) => { rec('session.testConnection', h, p, k); return state.testResult },
    },
    forwards: {
      list: async (): Promise<ForwardStatus[]> => state.forwards.map(f => ({
        forward: f, state: state.running[f.id] ? 'running' : 'stopped', addr: state.running[f.id],
      })),
      save: async f => upsert(state.forwards, {bind_addr: '127.0.0.1', auto_start: false, ...f}, 'f'),
      remove: async i => { state.forwards = state.forwards.filter(f => f.id !== i) },
      start: async i => { rec('forwards.start', i); state.running[i] = '127.0.0.1:5555'; emit('forward:state', {id: i}) },
      stop: async i => { delete state.running[i]; emit('forward:state', {id: i}) },
    },
    sync: {
      configure: async u => { rec('sync.configure', u) },
      configureFolder: async d => { rec('sync.configureFolder', d) },
      syncNow: async () => { rec('sync.syncNow') },
      join: async pw => { rec('sync.join', pw) },
      joinRemoteVault: async pw => { rec('sync.joinRemoteVault', pw) },
      replaceRemote: async () => { rec('sync.replaceRemote') },
      status: async () => ({configured: true, repo_url: 'git@example.com:me/sync.git', folder: '', last_sync: '0001-01-01T00:00:00Z', syncing: false}),
    },
    data: {
      importInventory: async (p, c, o) => { rec('data.importInventory', p, c, o); return {format: 'AWS EC2 (aws ec2 describe-instances)', hosts_added: 3, groups_added: 1, forwards_added: 0, keys_imported: 0, skipped: ['web-1'], warnings: ['1 instance that is not running was skipped']} },
      importSSHConfig: async (p, c) => { rec('data.import', p, c); return {hosts_added: 2, groups_added: 1, forwards_added: 0, keys_imported: 1, skipped: ['dup'], warnings: ['ProxyCommand ignored']} },
      exportSSHConfig: async (d, k) => { rec('data.exportSSH', d, k); return {files: ['a'], key_files: k ? ['k'] : [], warnings: []} },
      exportPlainJSON: async p => { rec('data.exportJSON', p) },
      exportBackup: async (p, pw) => { rec('data.exportBackup', p, pw) },
      importBackup: async (p, pw) => { rec('data.importBackup', p, pw) },
    },
    settings: {
      get: async () => ({...state.settings}),
      save: async s => { state.settings = {...state.settings, ...s}; return state.settings },
      reset: async () => { rec('settings.reset'); const {account_name, account_email, sync_url} = state.settings; state.settings = {...DEFAULT_SETTINGS, font_family: 'monospace', account_name, account_email, sync_url}; return state.settings },
      clearCache: async () => { rec('settings.clearCache'); return {...state.cache} },
      sessionLogFolder: async () => state.settings.session_log_dir || '/Users/me/Library/Application Support/vutty/session-logs',
    },
    sftp: {
      open: async (h, k) => { rec('sftp.open', h, k); return {sid: id('sf'), home: '/', host: state.hosts.find(x => x.id === h)?.label ?? 'host'} },
      list: async (s, dir) => {
        rec('sftp.list', s, dir)
        if (!state.remote[dir]?.dir) throw 'no such directory: ' + dir
        const out: RemoteEntry[] = []
        for (const [p, e] of Object.entries(state.remote)) {
          const parent = p.slice(0, p.lastIndexOf('/')) || '/'
          if (p !== '/' && parent === dir) out.push({name: p.slice(p.lastIndexOf('/') + 1), path: p, size: e.size, mode: e.dir ? 'drwxr-xr-x' : '-rw-r--r--', mod_time: '2026-10-28T10:00:00Z', is_dir: e.dir, is_link: false})
        }
        return out.sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name))
      },
      chmod: async (s, p, m) => { rec('sftp.chmod', s, p, m) },
      mkdir: async (s, p) => { rec('sftp.mkdir', s, p); if (state.remote[p]) throw 'file exists'; state.remote[p] = {dir: true, size: 0} },
      rename: async (s, a, b) => {
        rec('sftp.rename', s, a, b)
        if (state.remote[b]) throw b + ': already exists'
        for (const p of Object.keys(state.remote)) if (p === a || p.startsWith(a + '/')) { state.remote[b + p.slice(a.length)] = state.remote[p]; delete state.remote[p] }
      },
      remove: async (s, p, rec_) => {
        rec('sftp.remove', s, p, rec_)
        const kids = Object.keys(state.remote).filter(x => x.startsWith(p + '/'))
        if (kids.length && !rec_) throw 'directory not empty'
        for (const k of kids) delete state.remote[k]
        delete state.remote[p]
      },
      upload: async (s, local, dir, overwrite) => {
        rec('sftp.upload', s, local, dir, overwrite)
        if (!state.local[local] || state.local[local].dir) throw 'no such file: ' + local
        const dest = (dir === '/' ? '' : dir) + '/' + local.slice(local.lastIndexOf('/') + 1)
        if (state.remote[dest] && !overwrite) throw dest + ': already exists'
        state.remote[dest] = {dir: false, size: 2048}
        emit('sftp:progress:' + s, {name: dest.slice(dest.lastIndexOf('/') + 1), direction: 'upload', done: 2048, total: 2048})
        return dest
      },
      download: async (s, remotePath, local, overwrite) => {
        rec('sftp.download', s, remotePath, local, overwrite)
        if (state.local[local] && !overwrite) throw local + ': already exists'
        state.local[local] = {dir: false, size: 10}
        emit('sftp:progress:' + s, {name: remotePath.slice(remotePath.lastIndexOf('/') + 1), direction: 'download', done: 10, total: 10})
      },
      close: async s => { rec('sftp.close', s) },
      uploadAll: async (s, locals, dir, overwrite) => {
        rec('sftp.uploadAll', s, locals, dir, overwrite)
        return copyTree(state.local, state.remote, locals, dir, overwrite, e => emit('sftp:progress:' + s, {name: e, direction: 'upload', done: 1, total: 1}))
      },
      downloadAll: async (s, remotes, dir, overwrite) => {
        rec('sftp.downloadAll', s, remotes, dir, overwrite)
        return copyTree(state.remote, state.local, remotes, dir, overwrite, e => emit('sftp:progress:' + s, {name: e, direction: 'download', done: 1, total: 1}))
      },
      conflicts: async (s, direction, sources, destDir) => {
        rec('sftp.conflicts', s, direction, sources, destDir)
        const [from, to] = direction === 'upload' ? [state.local, state.remote] : [state.remote, state.local]
        const out: string[] = []
        for (const src of sources) {
          const name = src.slice(src.lastIndexOf('/') + 1)
          for (const p of Object.keys(from).filter(x => x === src || x.startsWith(src + '/')).sort()) {
            const rel = name + p.slice(src.length)
            if (!from[p].dir && to[(destDir === '/' ? '' : destDir) + '/' + rel]) out.push(rel)
          }
        }
        return out
      },
      enqueue: async (s, spec) => {
        rec('sftp.enqueue', s, spec)
        const first = spec.sources[0] ?? ''
        const base = first.slice(first.lastIndexOf('/') + 1) || first
        const job: QueueJob = {id: id('job'), direction: spec.direction, name: spec.sources.length > 1 ? `${base} and ${spec.sources.length - 1} more` : base, destDir: spec.destDir,
          state: 'queued', current: '', done: 0, total: 0, files: 0, dirs: 0, bytes: 0, skipped: [], ignored: [], error: ''}
        queues[s] = [...(queues[s] ?? []), job]
        if (state.holdQueue) { Object.assign(job, {state: 'running', current: base, done: 40, total: 100}); push(s) } else { push(s); void Promise.resolve().then(() => run(s, job, spec)) }
        return job.id
      },
      syncPlan: async (s, direction, src, dst) => {
        rec('sftp.syncPlan', s, direction, src, dst)
        return direction === 'sync-up' ? syncDiff(state.local, state.remote, src, dst) : syncDiff(state.remote, state.local, src, dst)
      },
      queue: async s => { rec('sftp.queue', s); return (queues[s] ?? []).map(j => ({...j})) },
      pause: async (s, j) => { rec('sftp.pause', s, j); patchJob(s, j, {state: 'paused'}) },
      resume: async (s, j) => { rec('sftp.resume', s, j); patchJob(s, j, {state: 'running'}) },
      cancel: async (s, j) => { rec('sftp.cancel', s, j); patchJob(s, j, {state: 'canceled'}) },
      retry: async (s, j) => { rec('sftp.retry', s, j); patchJob(s, j, {state: 'running', error: ''}) },
      clearFinished: async s => { rec('sftp.clearFinished', s); queues[s] = (queues[s] ?? []).filter(j => !['done', 'failed', 'canceled'].includes(j.state)); push(s) },
      editStart: async (s, r, l) => { rec('sftp.editStart', s, r, l) },
      editPush: async (s, r, f) => { rec('sftp.editPush', s, r, f) },
      editStop: async (s, r) => { rec('sftp.editStop', s, r) },
    },
    assistant: {
      providers: async () => (state.ai.checking-- > 0 ? state.ai.providers.map(p => ({...p, status: {...p.status, state: '' as const}})) : state.ai.providers.map(p => ({...p}))),
      refreshProviders: async () => { rec('assistant.refreshProviders'); return state.ai.providers.map(p => ({...p})) },
      setProviderEnabled: async (i, on) => {
        rec('assistant.setProviderEnabled', i, on)
        if (on && state.ai.enableError[i]) throw state.ai.enableError[i]
        state.ai.providers = state.ai.providers.map(p => (p.id === i ? {...p, enabled: on} : p))
        if (on && !state.ai.settings.default_provider) state.ai.settings.default_provider = i
      },
      setDefaultProvider: async i => { rec('assistant.setDefaultProvider', i); state.ai.providers = state.ai.providers.map(p => ({...p, default: p.id === i})); state.ai.settings.default_provider = i },
      setProviderModel: async (i, m) => { rec('assistant.setProviderModel', i, m); state.ai.providers = state.ai.providers.map(p => (p.id === i ? {...p, model: m} : p)) },
      models: async (i, r) => { rec('assistant.models', i, r); if (state.ai.modelsGate) await state.ai.modelsGate; return state.ai.models[i] ?? {models: [{id: '', name: 'Default (CLI decides)'}], error: '', can_list: false} },
      settings: async () => ({...state.ai.settings}),
      saveSettings: async s => { rec('assistant.saveSettings', s); state.ai.settings = {...s}; return {...s} },
      saveCustomProvider: async p => { rec('assistant.saveCustomProvider', p) },
      deleteCustomProvider: async i => { rec('assistant.deleteCustomProvider', i) },
      sessionInfo: async sid => { rec('assistant.sessionInfo', sid); return {...state.ai.info} },
      setHostPolicy: async (h, p) => { rec('assistant.setHostPolicy', h, p); state.ai.info = {...state.ai.info, stored: p, policy: p || (state.ai.info.production ? 'off' : 'on')} },
      confirmShare: async sid => { rec('assistant.confirmShare', sid); state.ai.info = {...state.ai.info, confirmed: true} },
      resetConversation: async sid => { rec('assistant.resetConversation', sid) },
      preview: async (sid, l, sel, att) => { rec('assistant.preview', sid, l, sel, att); return {...state.ai.preview, images: (att ?? []).filter(a => a.data).length} },
      ask: async (sid, q, o) => {
        rec('assistant.ask', sid, q, o)
        if (state.ai.info.policy === 'ask' && !state.ai.info.confirmed) throw 'confirm_share'
        if (state.ai.info.policy === 'off') throw 'The assistant is turned off for this host'
        if ((o.attachments ?? []).some(a => a.data) && !o.allow_images) throw 'confirm_images'
        const rid = o.request_id || id('ask-')
        state.ai.asked.push(rid)
        return rid
      },
      cancel: async i => { rec('assistant.cancel', i) },
      confirmTool: async (r, c, a) => { rec('assistant.confirmTool', r, c, a) },
      checkCommand: async (sid, cmd) => { rec('assistant.checkCommand', sid, cmd); return state.ai.check(cmd) },
      insert: async (sid, cmd) => { rec('assistant.insert', sid, cmd) },
      run: async (sid, cmd, t) => { rec('assistant.run', sid, cmd, t) },
      runReadOnly: async (sid, cmd) => { rec('assistant.runReadOnly', sid, cmd) },
      mcp: {
        list: async () => state.ai.mcp.map(s => ({...s})),
        save: async s => {
          rec('assistant.mcp.save', s)
          const i = state.ai.mcp.findIndex(x => x.id === s.id)
          const view: McpServer = {id: s.id || id('mcp'), name: s.name, type: s.type, command: s.command ?? '', args: s.args ?? [], url: s.url ?? '',
            env: Object.keys(s.env ?? {}), headers: Object.keys(s.headers ?? {}), enabled: s.enabled, trusted: s.trusted, host_ids: s.host_ids ?? [], providers: s.providers ?? [], status: '', reviewed: 0}
          if (i >= 0) state.ai.mcp[i] = view
          else state.ai.mcp.push(view)
          return view
        },
        remove: async i => { rec('assistant.mcp.remove', i); state.ai.mcp = state.ai.mcp.filter(s => s.id !== i) },
        test: async i => { rec('assistant.mcp.test', i); const t = state.ai.tools[i]; if (!t) throw 'cannot start the server'; return t.map(x => ({...x})) },
        setTool: async (s, t, m) => { rec('assistant.mcp.setTool', s, t, m); state.ai.tools[s] = (state.ai.tools[s] ?? []).map(x => (x.name === t ? {...x, mode: m as McpTool['mode'], new: false} : x)) },
        approveNew: async i => { rec('assistant.mcp.approveNew', i); state.ai.tools[i] = (state.ai.tools[i] ?? []).map(x => ({...x, new: false})) },
        importable: async () => state.ai.importable.map(x => ({...x})),
        import: async ref => { rec('assistant.mcp.import', ref); const it = state.ai.importable.find(x => x.ref === ref)!; const v: McpServer = {id: id('mcp'), name: it.name, type: it.type, command: it.command, args: it.args, url: it.url, env: it.env, headers: it.headers, enabled: false, trusted: false, host_ids: [], providers: [], status: '', reviewed: 0}; state.ai.mcp.push(v); return v },
      },
    },
    serial: {
      ports: async () => [...state.serialPorts],
    },
    knownHosts: {
      list: async () => [...state.knownHosts],
      forget: async (h, f) => {
        rec('knownHosts.forget', h, f)
        const n = state.knownHosts.length
        state.knownHosts = state.knownHosts.filter(k => !(k.host === h && k.fingerprint === f))
        if (state.knownHosts.length === n) throw 'that host is not in known_hosts'
      },
    },
    logs: {
      list: async f => {
        rec('logs.list', f)
        const q = f.query.trim().toLowerCase()
        return state.logs
          .filter(e => !f.kinds.length || f.kinds.includes(e.kind))
          .filter(e => !q || (e.message + ' ' + (e.host_label ?? '')).toLowerCase().includes(q))
          .slice(0, f.limit || 500)
      },
      clear: async () => { rec('logs.clear'); state.logs = [] },
    },
    exec: {
      run: async spec => {
        rec('exec.run', spec)
        const id = 'run' + (Object.keys(state.execRuns).length + 1)
        state.execRuns[id] = {id, done: false, results: spec.host_ids.map(h => ({host_id: h, label: state.hosts.find(x => x.id === h)?.label ?? h, env: state.hosts.find(x => x.id === h)?.env, state: 'queued' as const, exit_code: 0, output: '', duration_ms: 0}))}
        return id
      },
      cancel: async id => { rec('exec.cancel', id) },
      get: async id => { const r = state.execRuns[id]; if (!r) throw 'that run is no longer kept'; return {...r, results: r.results.map(x => ({...x}))} },
      forget: async id => { rec('exec.forget', id); delete state.execRuns[id] },
    },
    server: {
      open: async (h, k) => { rec('server.open', h, k); if (state.server.openError) throw state.server.openError; return {sid: 'srv1', host: state.hosts.find(x => x.id === h)?.label ?? h} },
      close: async sid => { rec('server.close', sid) },
      overview: async () => ({...state.server.overview}),
      processes: async () => state.server.processes.map(p => ({...p})),
      kill: async (sid, pid, sig) => { rec('server.kill', pid, sig); if (state.server.failAction) throw state.server.failAction; state.server.processes = state.server.processes.filter(p => p.pid !== pid) },
      services: async () => state.server.units.map(u => ({...u})),
      reboot: async () => { rec('server.reboot'); if (state.server.failAction) throw state.server.failAction },
      serviceAction: async (sid, name, action) => {
        rec('server.serviceAction', name, action)
        if (state.server.failAction) throw state.server.failAction
        state.server.units = state.server.units.map(u => (u.name === name ? {...u, active: action === 'stop' ? 'inactive' : 'active', sub: action === 'stop' ? 'dead' : 'running'} : u))
      },
      containers: async () => ({installed: state.server.docker.installed, containers: state.server.docker.containers.map(c => ({...c}))}),
      containerAction: async (sid, name, action) => {
        rec('server.containerAction', name, action)
        if (state.server.failAction) throw state.server.failAction
        state.server.docker.containers = state.server.docker.containers.map(c => (c.name === name ? {...c, state: action === 'stop' ? 'exited' : 'running'} : c))
      },
      dockerShellCommand: async (sid, name) => { rec('server.dockerShellCommand', name); return 'docker exec -it ' + name + ' sh' },
      dockerStats: async () => state.server.docker.containers.filter(c => c.state === 'running').map((c, i) => ({name: c.name, cpu: 12 - i * 5, mem_pct: 4 + i, mem_usage: (60 + i * 40) + 'MiB'})),
      dockerInspect: async (sid, name) => ({name, image: 'registry/' + name, created: '2026-01-01T00:00:00Z', state: 'running', restart: 'always', command: 'node app.js', env: [{key: 'NODE_ENV', value: 'production'}, {key: 'DB_PASSWORD', value: 'hunter2'}], mounts: [{type: 'volume', source: '/var/lib/docker/volumes/data', dest: '/data', rw: true}], ports: ['0.0.0.0:3000 → 3000/tcp'], networks: ['bridge'], labels: {}}),
      dockerObjects: async () => ({images: state.server.dockerObjects.images.map(i => ({...i})), volumes: state.server.dockerObjects.volumes.map(v => ({...v})), networks: state.server.dockerObjects.networks.map(n => ({...n}))}),
      dockerPull: async (sid, image) => { rec('server.dockerPull', image); if (state.server.failAction) throw state.server.failAction; return 'Status: Downloaded newer image for ' + image },
      dockerRemove: async (sid, kind, id) => {
        rec('server.dockerRemove', kind, id)
        if (state.server.failAction) throw state.server.failAction
        if (kind === 'image') state.server.dockerObjects.images = state.server.dockerObjects.images.filter(i => i.id !== id)
        else state.server.dockerObjects.volumes = state.server.dockerObjects.volumes.filter(v => v.name !== id)
      },
      logs: async (sid, kind, name, opts) => { rec('server.logs', kind, name, ...(opts ? [opts] : [])); return state.server.logs },
      ports: async () => ({firewall: state.server.ports.firewall, ports: state.server.ports.ports.map(p => ({...p})), rules: state.server.ports.rules.map(r => ({...r}))}),
      firewallAdd: async (sid, action, port, proto, from, force) => {
        rec('server.firewallAdd', action, port, proto, from, force)
        if (state.server.failAction) throw state.server.failAction
        const pt = state.server.ports
        pt.rules.push({num: pt.rules.length + 1, to: port + (proto ? '/' + proto : ''), action: action.toUpperCase(), from: from || 'Anywhere'})
        pt.ports = pt.ports.map(p => (p.port === port && !from ? {...p, open: action === 'allow'} : p))
      },
      firewallDelete: async (sid, rule, force) => {
        rec('server.firewallDelete', rule.num, force)
        if (state.server.failAction) throw state.server.failAction
        state.server.ports.rules = state.server.ports.rules.filter(r => r.num !== rule.num).map((r, i) => ({...r, num: i + 1}))
      },
      firewallEnable: async () => { rec('server.firewallEnable'); state.server.ports.firewall = 'active' },
      firewallDisable: async () => { rec('server.firewallDisable'); state.server.ports.firewall = 'inactive' },
      stackAction: async (sid, project, action) => {
        rec('server.stackAction', project, action)
        if (state.server.failAction) throw state.server.failAction
        if (action === 'down') state.server.docker.containers = state.server.docker.containers.filter(c => c.project !== project)
        if (action === 'stop' || action === 'start') state.server.docker.containers = state.server.docker.containers.map(c => (c.project === project ? {...c, state: action === 'stop' ? 'exited' : 'running'} : c))
      },
      certificates: async () => ({found: state.server.certs.found, certs: state.server.certs.certs.map(c => ({...c}))}),
      web: async () => ({...state.server.web, sites: state.server.web.sites.map(x => ({...x}))}),
      webTest: async (sid, kind) => { rec('server.webTest', kind); return {...state.server.webResult} },
      webReload: async (sid, kind) => { rec('server.webReload', kind); if (state.server.failAction) throw state.server.failAction; return {...state.server.webResult} },
      updates: async () => ({...state.server.updates, packages: state.server.updates.packages.map(p => ({...p}))}),
      updatesRefresh: async () => { rec('server.updatesRefresh'); if (state.server.failAction) throw state.server.failAction },
      cleanup: async () => ({disks: state.server.cleanup.disks.map(d => ({...d})), items: state.server.cleanup.items.map(i => ({...i}))}),
      cleanupRun: async (sid, id) => {
        rec('server.cleanupRun', id)
        if (state.server.failAction) throw state.server.failAction
        state.server.cleanup.items = state.server.cleanup.items.filter(i => i.id !== id)
      },
      diskUsage: async (sid, path) => { rec('server.diskUsage', path); return (state.server.folders[path] ?? []).map(f => ({...f})) },
      users: async () => JSON.parse(JSON.stringify(state.server.users)),
      addUser: async (sid, name, shell, sudo, key) => {
        rec('server.addUser', name, shell, sudo, key)
        if (state.server.failAction) throw state.server.failAction
        state.server.users.users.push({name, uid: 1002, home: '/home/' + name, shell, sudo, keys: key ? 1 : 0, login: ''})
      },
      deleteUser: async (sid, name, removeHome) => {
        rec('server.deleteUser', name, removeHome)
        if (state.server.failAction) throw state.server.failAction
        state.server.users.users = state.server.users.users.filter(u => u.name !== name)
      },
      userKeys: async (sid, name) => (state.server.userKeys[name] ?? []).map(k => ({...k})),
      addUserKey: async (sid, name, key) => {
        rec('server.addUserKey', name, key)
        if (state.server.failAction) throw state.server.failAction
        state.server.userKeys[name] = [...(state.server.userKeys[name] ?? []), {type: key.split(' ')[0], fingerprint: 'SHA256:' + 'b'.repeat(43), comment: key.split(' ')[2] ?? '', options: false}]
      },
      removeUserKey: async (sid, name, fp) => {
        rec('server.removeUserKey', name, fp)
        if (state.server.failAction) throw state.server.failAction
        state.server.userKeys[name] = (state.server.userKeys[name] ?? []).filter(k => k.fingerprint !== fp)
      },
      security: async () => JSON.parse(JSON.stringify(state.server.security)),
      unban: async (sid, jail, ip) => {
        rec('server.unban', jail, ip)
        if (state.server.failAction) throw state.server.failAction
        state.server.security.fail2ban.jails = state.server.security.fail2ban.jails.map(j => (j.name === jail ? {...j, banned: j.banned - 1, ips: j.ips.filter(x => x !== ip)} : j))
      },
      schedule: async () => JSON.parse(JSON.stringify(state.server.schedule)),
      cronAdd: async (sid, owner, schedule, command) => {
        rec('server.cronAdd', owner, schedule, command)
        if (state.server.failAction) throw state.server.failAction
        state.server.schedule.cron.push({source: owner, user: owner === 'root' ? 'root' : '', schedule, command})
      },
      cronUpdate: async (sid, owner, oldS, oldC, schedule, command) => {
        rec('server.cronUpdate', owner, oldS, oldC, schedule, command)
        if (state.server.failAction) throw state.server.failAction
        state.server.schedule.cron = state.server.schedule.cron.map(c => (c.source === owner && c.schedule === oldS && c.command === oldC ? {...c, schedule, command} : c))
      },
      cronDelete: async (sid, owner, schedule, command) => {
        rec('server.cronDelete', owner, schedule, command)
        if (state.server.failAction) throw state.server.failAction
        state.server.schedule.cron = state.server.schedule.cron.filter(c => !(c.source === owner && c.schedule === schedule && c.command === command))
      },
      backup: async (sid, path) => { rec('server.backup', path); return {...state.server.backup} },
      fleetHost: async hostId => {
        rec('server.fleetHost', hostId)
        const err = state.server.fleetErrors[hostId]
        return err ? {host_id: hostId, error: err, overview: {...state.server.overview}, failed_units: 0, latency_ms: 0} : {host_id: hostId, error: '', overview: {...state.server.overview}, failed_units: 1, latency_ms: 42}
      },
      ...k8sApi(() => state.k8s, rec),
    },
    local: {
      home: async () => '/Users/me',
      list: async dir => {
        rec('local.list', dir)
        if (!state.local[dir]?.dir) throw 'no such directory: ' + dir
        const out: RemoteEntry[] = []
        for (const [p, e] of Object.entries(state.local)) {
          const parent = p.slice(0, p.lastIndexOf('/')) || '/'
          if (p !== '/' && parent === dir) out.push({name: p.slice(p.lastIndexOf('/') + 1), path: p, size: e.size, mode: e.dir ? 'drwxr-xr-x' : '-rw-r--r--', mod_time: '2026-10-28T10:00:00Z', is_dir: e.dir, is_link: false})
        }
        return out.sort((a, b) => Number(b.is_dir) - Number(a.is_dir) || a.name.localeCompare(b.name))
      },
      chmod: async (p, m) => { rec('local.chmod', p, m) },
      tempDir: async () => '/tmp/vutty-open-1',
      conflicts: async (sources, destDir) => {
        rec('local.conflicts', sources, destDir)
        return sources.map(s => destDir + s.slice(s.lastIndexOf('/'))).filter(d => state.local[d] && !state.local[d].dir)
      },
      copy: async (sources, destDir, overwrite) => {
        rec('local.copy', sources, destDir, overwrite)
        const r: TransferResult = {files: 0, dirs: 0, bytes: 0, skipped: [], ignored: []}
        for (const s of sources) {
          const d = destDir + s.slice(s.lastIndexOf('/'))
          for (const p of Object.keys(state.local)) {
            if (p !== s && !p.startsWith(s + '/')) continue
            const to = d + p.slice(s.length), e = state.local[p]
            if (e.dir) { if (!state.local[to]) { state.local[to] = {...e}; r.dirs++ }; continue }
            if (state.local[to] && !overwrite) { r.skipped.push(to); continue }
            state.local[to] = {...e}; r.files++; r.bytes += e.size
          }
        }
        return r
      },
      mkdir: async p => { rec('local.mkdir', p); if (state.local[p]) throw 'file exists'; state.local[p] = {dir: true, size: 0} },
      rename: async (a, b) => {
        rec('local.rename', a, b)
        if (state.local[b]) throw b + ' already exists'
        for (const p of Object.keys(state.local)) if (p === a || p.startsWith(a + '/')) { state.local[b + p.slice(a.length)] = state.local[p]; delete state.local[p] }
      },
      remove: async (p, r) => {
        rec('local.remove', p, r)
        const kids = Object.keys(state.local).filter(x => x.startsWith(p + '/'))
        if (kids.length && !r) throw 'directory not empty'
        for (const k of kids) delete state.local[k]
        delete state.local[p]
      },
    },
    snippets: {
      list: async () => [...state.snippets],
      save: async s => {
        rec('snippets.save', s)
        if (!s.label?.trim() || !s.script?.trim()) throw 'snippet label and script are required'
        return upsert(state.snippets, s, 'sn')
      },
      remove: async i => { state.snippets = state.snippets.filter(x => x.id !== i) },
    },
    clusters: {
      list: async () => [...state.clusters].sort((a, b) => a.label.localeCompare(b.label)),
      preview: async (p, x) => { rec('clusters.preview', p, x); if (state.previewError) throw state.previewError; return JSON.parse(JSON.stringify(state.preview)) },
      describe: async i => { rec('clusters.describe', i); return JSON.parse(JSON.stringify(state.preview)) },
      add: async c => {
        rec('clusters.add', c)
        if (!c.path && !c.kubeconfig.trim()) throw 'choose a kubeconfig file or paste its text'
        const ctx = state.preview.contexts.find(x => x.name === c.context) ?? state.preview.contexts.find(x => x.current) ?? state.preview.contexts[0]
        const now = '2026-11-04T10:00:00Z'
        return upsert(state.clusters, {label: c.label || ctx.cluster, group_id: c.group_id || undefined, env: c.env || undefined, context: ctx.name, server: ctx.server, created_at: now, updated_at: now}, 'cl')
      },
      update: async c => { rec('clusters.update', c); return upsert(state.clusters, {...state.clusters.find(x => x.id === c.id), label: c.label, group_id: c.group_id, env: c.env, context: c.context}, 'cl') },
      replaceKubeconfig: async (i, p, x) => { rec('clusters.replaceKubeconfig', i, p, x); const c = state.clusters.find(y => y.id === i)!; c.approved_exec = []; return {...c} },
      revokeExec: async (i, cmd) => { rec('clusters.revokeExec', i, cmd); const c = state.clusters.find(y => y.id === i)!; c.approved_exec = (c.approved_exec ?? []).filter(x => x !== cmd); return {...c} },
      approveExec: async (i, cmd) => { rec('clusters.approveExec', i, cmd); const c = state.clusters.find(y => y.id === i)!; c.approved_exec = [...(c.approved_exec ?? []), cmd]; return {...c} },
      remove: async i => { rec('clusters.remove', i); state.clusters = state.clusters.filter(c => c.id !== i) },
      open: async i => { rec('clusters.open', i); if (state.server.openError) throw state.server.openError; return {sid: 'kube1', host: state.clusters.find(c => c.id === i)?.label ?? i} },
    },
    system: {
      isFullscreen: async () => false,
      setMenuShortcuts: async s => { rec('system.setMenuShortcuts', s) },
      pickFile: async () => '/tmp/picked',
      pickDirectory: async () => '/tmp/dir',
      saveFile: async (_t, name) => '/tmp/' + name,
      saveText: async (_t, name, text) => { rec('system.saveText', name, text); return '/tmp/' + name },
      openUrl: async u => { rec('system.openUrl', u) },
      setLanguage: async l => { rec('system.setLanguage', l) },
      info: async () => ({version: '1.1.0-test'}),
      checkUpdate: async () => { rec('system.checkUpdate'); const latest = state.latest ?? '1.1.0-test'; return {current: '1.1.0-test', latest, available: latest !== '1.1.0-test', url: 'https://github.com/vunguyenvu35/vutty/releases/tag/v' + latest, notes: ''} },
      selfUpdateStatus: async () => state.selfUpdate ?? {supported: false, reason: 'this copy of Vutty is not an installed application (a development build)'},
      installUpdate: async () => { rec('system.installUpdate'); if (state.installError) throw state.installError },
      toggleMaximise: async () => { rec('system.toggleMaximise') },
      readClipboard: async () => { rec('system.readClipboard'); return state.clipboard },
      takePendingSSHLink: async () => { const l = state.pendingSSHLink ?? null; state.pendingSSHLink = undefined; return l },
      writeClipboard: async t => { rec('system.writeClipboard', t); state.clipboard = t },
      openWindow: async (h, t) => { rec('system.openWindow', h, t) },
      openPath: async p => { rec('system.openPath', p) },
      openPathWith: async (p, a) => { rec('system.openPathWith', p, a) },
    },
    finishJob: (sid, jobId, patch = {}) => patchJob(sid, jobId, {state: 'done', done: 100, total: 100, ...patch}),
    jobs: sid => (queues[sid] ?? []).map(j => ({...j})),
    on: (event, cb) => {
      if (!listeners.has(event)) listeners.set(event, new Set())
      listeners.get(event)!.add(cb)
      return () => listeners.get(event)!.delete(cb)
    },
  }
  return fake
}

export function Providers({be, children}: {be: Backend; children: ReactNode}) {
  return (
    <BackendContext.Provider value={be}>
      <ToastProvider>{children}</ToastProvider>
    </BackendContext.Provider>
  )
}

export const host = (o: Partial<Host> = {}): Host => ({id: id('h'), label: 'web-1', hostname: '10.0.0.1', port: 22, username: 'root', keepalive_sec: 30, ...o})

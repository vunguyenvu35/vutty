export type Env = '' | 'dev' | 'staging' | 'prod'
export type AuthType = 'key' | 'password' | 'agent'
export type ForwardType = 'local' | 'remote' | 'dynamic'

export interface Group {
  id: string
  name: string
  parent_id?: string
  color?: string
  sort_order: number
}

export interface Host {
  id: string
  label: string
  group_id?: string
  hostname: string
  port: number
  username: string
  identity_id?: string
  jump_host_id?: string
  env?: Env
  tags?: string[]
  keepalive_sec: number
  /** server type learned on the first connection (ubuntu, debian, freebsd…); '' until then */
  os?: string
  /** this host may use the keys of the user's ssh-agent (or this host's saved key) to sign in to other machines while connected (ssh -A) */
  forward_agent?: boolean
  /** also reachable with Telnet (unencrypted) on this port */
  telnet_enabled?: boolean
  telnet_port?: number
  telnet_username?: string
  /** '' or 'utf-8' (default), or 'iso-8859-1' */
  telnet_encoding?: string
  /** this host's own terminal colour theme (an id from the catalogue); empty follows Settings */
  terminal_theme?: string
  /** the AI assistant switch of this host: '' (off on prod hosts, ask elsewhere), off, ask or on */
  ai_policy?: AiPolicy
  /** a folder on the server where backups land: the Panel shows how old the newest file in it is (set from the Panel, not the host form) */
  backup_path?: string
}

export interface Identity {
  id: string
  name: string
  username?: string
  auth_type: AuthType
  public_key?: string
  /** an OpenSSH user certificate for the key (public, not a secret) */
  certificate?: string
}

export interface PortForward {
  id: string
  label?: string
  host_id: string
  type: ForwardType
  bind_addr: string
  bind_port: number
  dest_host?: string
  dest_port?: number
  auto_start: boolean
  allow_public_bind?: boolean
}

export interface ForwardStatus {
  forward: PortForward
  state: 'stopped' | 'running' | 'error'
  addr?: string
  error?: string
}

export interface VaultStatus {
  exists: boolean
  unlocked: boolean
  keychain_enabled: boolean
  auto_lock_minutes: number
  /** the reminder of the master password the user wrote, shown on the unlock screen */
  hint?: string
  /** this Mac can ask for Touch ID */
  biometric_available?: boolean
}

export type ThemeMode = 'auto' | 'light' | 'dark'
export type CursorStyle = 'block' | 'bar' | 'underline'

/** What Clear cache removed. */
export interface CacheReport {
  files: number
  bytes: number
}

export interface Settings {
  theme: ThemeMode
  /** language of the interface: auto follows the system */
  language?: 'auto' | 'en' | 'vi' | 'ja' | 'zh'
  terminal_theme: string // 'auto' follows the app theme, otherwise a catalogue id
  font_size: number
  font_family: string
  cursor_style: CursorStyle
  cursor_blink: boolean
  /** copy the selection to the clipboard as soon as it is made */
  copy_on_select: boolean
  /** what a right click in a terminal does */
  right_click: 'paste' | 'menu' | 'none'
  scrollback: number
  auto_lock_minutes: number
  sync_url: string
  zoom: number // interface scale in percent
  confirm_tab_close: boolean
  /** ask before pasting text with a line break, or running a snippet or a history command, in a production host's terminal */
  confirm_prod: boolean
  /** lock the vault when the computer goes to sleep (and, on a Mac, when the screen is locked) */
  lock_on_sleep: boolean
  /** write what each terminal session printed to a text file: off, prod (production hosts only) or all */
  session_log: 'off' | 'prod' | 'all'
  /** the folder for those files; '' means the default one inside the data folder */
  session_log_dir: string
  /** a host opens on a Choose protocol screen first */
  ask_protocol: boolean
  /** the lock screen asks for Touch ID / Windows Hello by itself, without a click */
  biometric_on_open: boolean
  suggest: boolean // show command suggestions while typing in a terminal
  suggest_host: boolean // also read the shell history of an SSH host for them
  /** colour URLs, IP addresses, paths and error / warning / success words in terminal output */
  highlight: boolean
  /** draw the terminal that has the focus with WebGL (falls back to the DOM renderer by itself) */
  gpu_renderer: boolean
  /** the groups of preferences that travel with the sync bundle (backend ids: appearance, terminal, language, shortcuts, behaviour, account) */
  sync_what: string[]
  reduce_motion: boolean
  /** look for a newer release when the app opens (at most once a day) */
  auto_update_check: boolean
  account_name: string
  account_email: string
  shortcuts: Record<string, string> // overrides only: action id -> combo
}

export interface ConnectionTest {
  ok: boolean
  message: string
  server_version?: string
  latency_ms: number
  hops: number
}

export interface SyncStatus {
  configured: boolean
  repo_url: string
  folder: string
  last_sync: string
  last_error?: string
  syncing: boolean
  /** this machine holds nothing of its own: the vault of a repository can simply be taken over */
  local_empty?: boolean
}

export interface HostFilter {
  query: string
  group_id: string
  env: Env
  tag: string
}

/** What a listing of servers does not say (see ImportInventory). */
export interface InventoryOptions {
  user: string
  /** 'public' (the public address when there is one, else the private one) or 'private' */
  address: 'public' | 'private'
  group: string
  include_stopped: boolean
}

export interface ImportReport {
  /** what an inventory import took the file to be */
  format?: string
  hosts_added: number
  groups_added: number
  forwards_added: number
  keys_imported: number
  skipped: string[]
  warnings: string[]
}

export interface ExportReport {
  files: string[]
  key_files: string[]
  warnings: string[]
}

/** What a server asks while signing in (keyboard-interactive): each prompt wants one answer. */
export interface AuthChallenge {
  host: string
  name: string
  instruction: string
  prompts: {text: string; echo: boolean}[]
}

/** A saved secret that the vault's key cannot open (it was sealed with another key): its owner has to be entered again. */
export interface UnreadableItem {
  secret_id: string
  name: string
  identity_id?: string
  kind: 'key' | 'password' | 'agent' | 'telnet' | 'other'
}
export interface VaultHealth {
  checked: number
  unreadable: UnreadableItem[]
}

/** One command on several hosts. {{host}}, {{hostname}} and {{user}} in the command become each host's own. */
export interface ExecSpec {
  host_ids: string[]
  command: string
  timeout_sec: number
}
export interface ExecResult {
  host_id: string
  label: string
  env?: string
  state: 'queued' | 'running' | 'ok' | 'failed' | 'canceled'
  exit_code: number
  /** standard output and standard error together (the first 64 KB) */
  output: string
  truncated?: boolean
  error?: string
  duration_ms: number
}
export interface ExecRun {
  id: string
  done: boolean
  results: ExecResult[]
}

/** Where an ssh:// link points. A password, a path and a query are dropped before it gets here. */
export interface SSHLink {
  username: string
  hostname: string
  port: number
}

export interface HostKeyPrompt {
  host: string
  fingerprint: string
  key_type: string
}

export interface ClosedInfo {
  reason: string
  clean: boolean
}

export interface Snippet {
  id: string
  label: string
  description?: string
  script: string
  /** when it was last changed (RFC 3339) */
  updated_at?: string
}

export interface RemoteEntry {
  name: string
  path: string
  size: number
  mode: string
  mod_time: string
  is_dir: boolean
  is_link: boolean
}

export interface SftpInfo {
  sid: string
  home: string
  host: string
}

/** A machine whose host key was trusted (a line of known_hosts). */
export interface KnownHost {
  host: string
  key_type: string
  fingerprint: string
}

/** One line of the activity log. */
export interface ActivityEntry {
  id: number
  at: string
  kind: 'connect' | 'disconnect' | 'hostkey' | 'forward' | 'sftp' | 'transfer' | 'vault' | 'sync'
  host_id?: string
  host_label?: string
  message: string
  ok: boolean
}

/** What a multi-file transfer did (see usecase.TransferResult). */
export interface TransferResult {
  files: number
  dirs: number
  bytes: number
  /** files that already exist and were not replaced */
  skipped: string[]
  /** links and special files, never followed */
  ignored: string[]
}

export interface TransferProgress {
  name: string
  direction: 'upload' | 'download'
  done: number
  total: number
}

/** Where a queued transfer stands. */
export type QueueState = 'queued' | 'preparing' | 'running' | 'paused' | 'done' | 'failed' | 'canceled'
export type QueueDirection = 'upload' | 'download' | 'sync-up' | 'sync-down'

/** What to queue: upload (sources are local paths), download (remote paths), sync-up / sync-down (one folder each). */
export interface QueueSpec {
  direction: QueueDirection
  sources: string[]
  destDir: string
  overwrite: boolean
}

/** One transfer in the queue (see usecase.QueueJob). The counts and lists describe what has happened so far. */
export interface QueueJob {
  id: string
  direction: QueueDirection
  name: string
  destDir: string
  state: QueueState
  /** the file being worked on */
  current: string
  done: number
  total: number
  files: number
  dirs: number
  bytes: number
  skipped: string[]
  ignored: string[]
  error: string
}

export interface SyncItem {
  path: string
  size: number
  reason: 'new' | 'changed'
}

/** What a sync would do: files to copy, how many are identical, and files only on the target (never touched). */
export interface SyncDiff {
  copy: SyncItem[]
  same: number
  extra: string[]
}

/** The result of saving an edited remote file back. */
export interface EditEvent {
  remote: string
  state: 'uploaded' | 'conflict' | 'error'
  message: string
}

// ---- AI assistant ----

export type AiPolicy = '' | 'off' | 'ask' | 'on'
export type AiState = 'ready' | 'needs_signin' | 'unsupported' | 'not_installed' | ''
export type AiRisk = 'low' | 'medium' | 'high'

export interface AiStatus {state: AiState; version: string; detail: string; hint: string}

export interface AiProvider {
  id: string
  name: string
  status: AiStatus
  enabled: boolean
  default: boolean
  /** the default model of the provider; '' lets the CLI decide */
  model: string
  supports_mcp: boolean
  /** takes pictures (sent as they are, after the user confirms) */
  supports_images: boolean
  custom: boolean
}

export interface AiModel {id: string; name: string; description?: string}
export interface AiModelList {models: AiModel[]; error: string; can_list: boolean}

export interface AiSettings {
  lines: number
  patterns: string[]
  auto_run: boolean
  default_provider: string
}

export interface AiSessionState {
  host_id: string
  label: string
  /** the effective policy */
  policy: 'off' | 'ask' | 'on'
  /** what is saved on the host ('' = default) */
  stored: AiPolicy
  production: boolean
  confirmed: boolean
  connected: boolean
}

export interface AiFinding {kind: string; count: number}

export interface AiPreview {
  text: string
  findings: AiFinding[]
  redacted: number
  lines: number
  alt_screen: boolean
  vault_locked: boolean
  empty: boolean
  /** pictures that would be sent as they are: nothing can be hidden in a picture */
  images: number
}

export interface AiCommand {
  cmd: string
  why: string
  risk: AiRisk
  reasons: string[]
  warnings: string[]
  blocked: boolean
  block_reason: string
  /** it only reads: it is run as soon as the answer shows, without asking */
  auto?: boolean
}

export interface AiReply {text: string; commands: AiCommand[]; provider: string; model: string; redacted: number}

export interface AiCommandCheck {
  blocked: boolean
  block_reason: string
  risk: AiRisk
  reasons: string[]
  warnings: string[]
  run_allowed: boolean
  run_reason: string
  typed_confirm: boolean
}

export interface AiAttachment {name: string; mime: string; /** the content of a text file */ text?: string; /** the base64 content of an image */ data?: string}
export interface AiAskOptions {provider: string; model: string; lines: number; selection: string; request_id?: string; attachments?: AiAttachment[]; /** the user agreed to send pictures as they are */ allow_images?: boolean}
export interface AiCustomProvider {id?: string; name: string; command: string; args?: string[]; prompt_via?: 'stdin' | 'arg'}

export type McpMode = 'allow' | 'ask' | 'off'

export interface McpServer {
  id: string
  name: string
  type: 'stdio' | 'http'
  command: string
  args: string[]
  url: string
  /** names only: the values are in the vault and are never read back */
  env: string[]
  headers: string[]
  enabled: boolean
  trusted: boolean
  host_ids: string[]
  providers: string[]
  status: string
  reviewed: number
}

export interface McpServerInput {
  id?: string
  name: string
  type: 'stdio' | 'http'
  command?: string
  args?: string[]
  url?: string
  /** a non-empty value is saved to the vault; an empty one keeps what is stored */
  env?: Record<string, string>
  headers?: Record<string, string>
  remove_env?: string[]
  remove_headers?: string[]
  enabled: boolean
  trusted: boolean
  host_ids?: string[]
  providers?: string[]
}

export interface McpTool {name: string; description: string; read_only: boolean; mode: McpMode; new: boolean}
export interface McpImportItem {ref: string; source: string; name: string; type: 'stdio' | 'http'; command: string; args: string[]; url: string; env: string[]; headers: string[]}

/** One step of an answer: a tool the assistant used. */
export interface AiStep {server: string; tool: string; state: string; note: string}

/** The Server section: a look at a Linux server over SSH (see usecase.ServerService). */
export interface ServerInfo {
  sid: string
  host: string
}
export interface ServerOverview {
  os: string
  kernel: string
  cores: number
  uptime_sec: number
  /** percent, over about half a second */
  cpu_pct: number
  load: [number, number, number]
  mem_total: number
  mem_used: number
  swap_total: number
  swap_used: number
  disk_total: number
  disk_used: number
  /** bytes per second; 0 on the first call */
  net_rx: number
  net_tx: number
  /** the login is root: no sudo is needed */
  root: boolean
}
export interface ServerProcess {
  pid: number
  user: string
  cpu: number
  mem: number
  command: string
}
export interface ServerUnit {
  name: string
  description: string
  active: string
  sub: string
  enabled: string
}
export interface ServerContainer {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: string
  /** the Docker Compose project it belongs to ('' when it was not started by Compose) */
  project: string
}
export interface ServerContainers {
  installed: boolean
  containers: ServerContainer[]
}
export interface ServerUsers {
  users: {name: string; uid: number; home: string; shell: string; sudo: boolean; keys: number; login: string}[]
  sessions: {user: string; tty: string; since: string; from: string}[]
  me: string // the account this connection is logged in as
}
export interface UserKey {type: string; fingerprint: string; comment: string; options: boolean}
export interface DockerStat {name: string; cpu: number; mem_pct: number; mem_usage: string}
export interface DockerInspect {name: string; image: string; created: string; state: string; restart: string; command: string; env: {key: string; value: string}[]; mounts: {type: string; source: string; dest: string; rw: boolean}[]; ports: string[]; networks: string[]; labels: Record<string, string>}
export interface DockerObjects {
  images: {id: string; name: string; size: string; created: string; in_use: boolean}[]
  volumes: {name: string; driver: string; in_use: boolean}[]
  networks: {id: string; name: string; driver: string; scope: string}[]
}
/** The System box of Maintenance: hostname, time zone and swap. Sizes are bytes. */
export interface ServerSystem {
  hostname: string
  timezone: string
  /** hostnamectl and timedatectl are there */
  systemd: boolean
  swap_total: number
  /** the size of /swapfile when it is in use, else 0 */
  swap_file: number
  /** some swap that is not /swapfile (a partition): shown, never touched */
  swap_other: boolean
  ram: number
  disk_free: number
}
export interface ServerPort {
  port: number
  process: string
  /** listens on the loopback address only */
  local: boolean
  /** the firewall lets it in (also true when there is no firewall to say otherwise) */
  open: boolean
}
export interface ServerPorts {
  /** ufw: 'active' or 'inactive'; 'unknown' when the server has none or the login may not ask */
  firewall: 'active' | 'inactive' | 'unknown'
  ports: ServerPort[]
  /** the rules of ufw, numbered as ufw numbers them (only when it is on) */
  rules: FirewallRule[]
}
export interface FirewallRule {
  num: number
  to: string
  action: string
  from: string
}

/** Let's Encrypt certificates found on the server (read only). */
export interface ServerCert {
  /** the folder name under /etc/letsencrypt/live */
  name: string
  domains: string[]
  /** unix seconds */
  not_after: number
  days_left: number
}
export interface ServerCerts {
  /** /etc/letsencrypt was found (and could be read) */
  found: boolean
  certs: ServerCert[]
}
/** A file of /etc/nginx/sites-available, and whether sites-enabled links to it. */
export interface WebSiteFile {
  name: string
  enabled: boolean
}
/** One server { } block of nginx, or one site of a Caddyfile. */
export interface ServerSite {
  names: string[]
  listen: string[]
  /** proxy_pass, fastcgi_pass, root, reverse_proxy… */
  targets: string[]
}
export interface ServerWeb {
  /** 'nginx', 'caddy' or '' when neither is installed */
  kind: '' | 'nginx' | 'caddy'
  version: string
  running: boolean
  sites: ServerSite[]
}
/** What nginx -t (caddy validate) printed; for a reload, ok means it passed and the server was reloaded. */
export interface ServerWebResult {
  ok: boolean
  output: string
}
export interface ServerPackage {
  name: string
  version: string
  security: boolean
}
export interface ServerUpdates {
  /** 'apt', 'dnf', 'yum' or '' when none is known */
  manager: '' | 'apt' | 'dnf' | 'yum'
  count: number
  security: number
  reboot_required: boolean
  /** seconds since the package lists were refreshed, -1 when unknown */
  lists_age_sec: number
  packages: ServerPackage[]
}
export interface ServerDisk {
  mount: string
  device: string
  total: number
  used: number
}
/** Something that can be cleaned, and about how much it would free. docker-volumes is shown only: it is never cleaned from the Panel. */
export interface ServerReclaim {
  id: 'docker-dangling' | 'docker-images' | 'journal' | 'apt-cache' | 'docker-volumes'
  /** bytes, 0 when unknown */
  size: number
  note: string
}
export interface ServerCleanup {
  disks: ServerDisk[]
  items: ServerReclaim[]
}
export interface ServerFolder {
  path: string
  size: number
}
export interface ServerJail {
  name: string
  banned: number
  total_banned: number
  ips: string[]
}
export interface ServerSecurity {
  fail2ban: {installed: boolean; running: boolean; jails: ServerJail[]}
  /** sshd -T; known is false when the login may not ask */
  ssh: {known: boolean; port: number; root_login: string; password_auth: string}
  /** failed SSH logins in the last 24 hours, by address, most first; known is false when there is no log to read */
  failed: {known: boolean; total: number; top: Array<{ip: string; count: number}>}
}
/** What the Security check reads besides ServerSecurity. */
export interface ServerAudit {
  /** ufw, firewalld, nft (on); ufw-off, firewalld-off, none */
  firewall: string
  /** yes, no (installed, not scheduled), none (not installed) */
  auto_updates: string
  /** accounts other than root with user id 0 */
  uid0: string[]
  empty_passwords: string[]
  /** the login could read /etc/shadow */
  passwords_known: boolean
  /** sudoers rules that need no password; -1 when unknown */
  nopasswd: number
  reboot_required: boolean
}
/** A login that worked (one line of last). */
export interface ServerLogin {
  user: string
  tty: string
  from: string
  when: string
  /** "still logged in", or how it ended */
  status: string
}
export interface ServerTimer {
  /** the timer unit, e.g. backup.timer */
  name: string
  description: string
  /** the unit it starts, e.g. backup.service */
  unit: string
  /** as the server prints them ('' when never or unknown) */
  next: string
  last: string
  /** how the last run of the unit ended: success, exit-code… ('' when unknown) */
  result: string
}
export interface ServerCron {
  /** where the line is: 'crontab' (the login's own), 'root', '/etc/crontab' or '/etc/cron.d/<file>' */
  source: string
  user: string
  schedule: string
  command: string
}
export interface ServerSchedule {
  timers: ServerTimer[]
  cron: ServerCron[]
  me: string // the account of this connection: whose crontab 'crontab' is
}
export interface ServerBackup {
  /** a file was found under the path */
  found: boolean
  /** the newest file, relative to the path */
  file: string
  size: number
  /** unix seconds */
  mtime: number
  /** now on the server, unix seconds, so that the age does not depend on this computer's clock */
  now: number
}
/** One saved host as the fleet overview sees it: a one-off connection, no prompts (a host key that is not trusted yet is an error). */
export interface ServerFleetHost {
  host_id: string
  /** empty when the host answered */
  error: string
  overview: ServerOverview
  failed_units: number
  /** how long the SSH connection took, in milliseconds */
  latency_ms: number
  /** days left of the Let's Encrypt certificate that ends first (negative: it has ended); null or absent when there is none or it cannot be read */
  cert_days?: number | null
}

/** The Kubernetes tab of the Panel: kubectl run on the host over SSH (see usecase.ServerService, K8s*). */
/** direct: a cluster opened with a kubeconfig, with no host and no kubectl (the shell and the port-forward go through the API server) */
export interface K8sDetect {found: boolean; cmd: string; path: string; client_version: string; tried: string[]; direct?: boolean}
export type KubeAuth = 'token' | 'certificate' | 'exec' | 'password' | 'none'
/** auth and exec say how a kubeconfig signs in, and the login program it runs (the whole command line) */
export interface K8sContext {name: string; cluster: string; server: string; user: string; current: boolean; auth?: KubeAuth; exec?: string; insecure?: boolean}
export interface K8sContexts {kubeconfig: string; sudo: boolean; contexts: K8sContext[]; kind: '' | 'kubeconfig'; raw: string}
export type K8sFailKind = 'unreachable' | 'unauthorized' | 'certificate' | 'forbidden' | 'other'
/** kind 'approval': raw is the command line of a login program that the person has to approve before it may run */
export interface K8sCheck {ok: boolean; kind: '' | K8sFailKind | 'approval'; raw: string; server: string; server_version: string}
export interface K8sAccess {rights: Record<string, boolean>}
export interface K8sNamespaces {names: string[]}
export interface K8sNode {name: string; status: 'Ready' | 'NotReady' | 'Unknown'; roles: string; version: string; ip: string; unschedulable: boolean; cpu_pct: number | null; mem_pct: number | null; pods: number; pods_max: number; age_sec: number}
export interface K8sAPIResource {group: string; version: string; plural: string; kind: string; namespaced: boolean; short?: string[]}
export interface K8sResRef {group: string; version: string; plural: string}
export interface K8sObjRow {namespace: string; name: string; age_sec: number; cells: string[]}
export interface K8sObjList {namespaced: boolean; columns: string[]; rows: K8sObjRow[]; more: boolean}
export interface K8sMultiLogs {text: string; pods: number; skipped: number; failed: string[]}
export interface K8sHelmRelease {name: string; namespace: string; revision: number; status: string; age_sec: number}
export interface K8sHelmDetail {release: K8sHelmRelease; chart: string; chart_version: string; app_version: string; description: string; notes: string; manifest: string; has_values: boolean; history: K8sHelmRelease[]}
export interface K8sDebug {container: string; command: string}
export interface K8sFinding {severity: 'high' | 'medium' | 'low'; rule: string; kind: string; namespace: string; name: string; container?: string; detail: string; count: number}
export interface K8sSecurity {findings: K8sFinding[]; pods: number; services: number}
export interface K8sEvent {age_sec: number; type: 'Normal' | 'Warning'; reason: string; object: string; namespace: string; message: string; count: number}
export interface K8sPodContainer {name: string; image: string; init: boolean; ready: boolean; running: boolean; state: string; restarts: number; resources: string; probes: string}
export interface K8sPod {name: string; namespace: string; status: string; tone: 'ok' | 'warn' | 'bad' | ''; ready: string; restarts: number; node: string; age_sec: number; why: string; owner: string; containers: K8sPodContainer[]}
export interface K8sPodDetail {pod: K8sPod; ip: string; qos: string; service_account: string; labels: string[]; conditions: {type: string; status: string}[]; volumes: {name: string; source: string}[]; events: K8sEvent[]}
export interface K8sWorkload {kind: 'Deployment' | 'StatefulSet' | 'DaemonSet' | 'Job' | 'CronJob'; name: string; namespace: string; ready: string; desired: number; available: number; up_to_date: number; image: string; age_sec: number; tone: 'ok' | 'warn' | 'bad' | ''; note: string; schedule: string; last_run_sec: number | null; suspended: boolean; result: string; volumes: string; hpa: boolean; /** what made a Job ("CronJob cron-sync") */ owner?: string}
export interface K8sWorkloads {items: K8sWorkload[]}
export interface K8sRevision {revision: number; image: string; cause: string; age_sec: number; current: boolean}
export interface K8sHPA {name: string; min: number; max: number; current: number; cpu_pct: number | null; target_cpu_pct: number | null; last_scaled_sec: number | null}
export interface K8sDeployment {workload: K8sWorkload; strategy: string; max_surge: string; max_unavailable: string; selector: string; paused: boolean; updated: number; rolling: boolean; revision: number; revisions: K8sRevision[]; hpa: K8sHPA | null}
export interface K8sService {name: string; namespace: string; type: string; cluster_ip: string; ports: string; ready_endpoints: number; not_ready_endpoints: number}
export interface K8sIngress {name: string; namespace: string; host: string; path: string; service: string; class: string; tls_secret: string; cert_days_left: number | null; address: string}
export interface K8sNetwork {services: K8sService[]; ingresses: K8sIngress[]}
export interface K8sConfigItem {name: string; namespace: string; type: string; keys: string[]; used_by: string[]; age_sec: number}
export interface K8sVolume {name: string; namespace: string; status: string; size: string; used_pct: number | null; used: string; storage_class: string; used_by: string[]}
export interface K8sConfig {configmaps: K8sConfigItem[]; secrets: K8sConfigItem[]; volumes: K8sVolume[]}
/** what pods, CPU and memory take against what the nodes give; used is null without a metrics-server */
export interface K8sCapacity {known: boolean; partial: boolean; pods_used: number; pods_allocatable: number; cpu_reserved: number; cpu_allocatable: number; mem_reserved_gib: number; mem_allocatable_gib: number; cpu_used: number | null; mem_used_gib: number | null}
export interface K8sOverview {
  capacity: K8sCapacity
  server_version: string
  nodes: {total: number; ready: number; cordoned: number}
  pods: {total: number; running: number; pending: number; failing: number}
  /** the login may not read the nodes (they are cluster-wide): the node and usage cards are empty */
  nodes_hidden?: boolean
  /** the nodes the pods run on, with their pod count: what is known when the nodes are not readable */
  nodes_seen?: {name: string; pods: number}[]
  cpu_requested_pct: number | null; mem_requested_pct: number | null; cpu_cores: number | null; mem_gib: number | null
  namespaces: {name: string; pods: number; not_ready: number; restarts: number}[]
  attention: {severity: 'bad' | 'warn'; kind: 'pod' | 'node' | 'deployment' | 'ingress' | 'pvc'; namespace: string; name: string; text: string}[]
}
export interface K8sDrainOpts {ignore_daemonsets: boolean; delete_emptydir: boolean; force: boolean; grace_sec: number; timeout_sec: number}
export interface K8sDrainPlan {node: string; moved: number; daemonsets: number; emptydir: string[]; bare: string[]; pdb: {namespace: string; name: string; detail: string}[]; blocked: boolean}
export interface K8sLogOpts {container: string; lines: 100 | 300 | 1000 | 5000; since: '' | '15m' | '1h' | '24h'; previous: boolean; timestamps: boolean}
export interface K8sForward {id: string; namespace: string; service: string; local_port: number; remote_port: number; status: 'running' | 'stopped' | 'failed'; error: string}

/** what the Rancher-style menu of a workload works with: Deployment, StatefulSet and DaemonSet */
export type K8sWorkloadKind = 'deployment' | 'statefulset' | 'daemonset'
export interface K8sEnvVar {name: string; value: string; /** where a value comes from when it is not written in the spec ("secret db:password"); such a variable is never given a value here */ from: string}
export interface K8sContainerSpec {
  name: string; image: string; init: boolean; command: string[]; args: string[]; ports: string[]
  env: K8sEnvVar[]; env_from: string[]; requests: Record<string, string>; limits: Record<string, string>; mounts: string[]
}
export interface K8sWorkloadSpec {kind: string; name: string; namespace: string; replicas: number | null; service_account: string; containers: K8sContainerSpec[]}
export interface K8sEditable {yaml: string; version: string}
export interface K8sDiffLine {op: ' ' | '+' | '-' | '~'; text: string}
export interface K8sEditPreview {diff: K8sDiffLine[]; changed: number}
/** one container of a workload changed; what is left out stays. In requests and limits a value of "" removes the entry */
export interface K8sContainerEdit {
  container: string; init?: boolean; image?: string
  env?: {name: string; value: string; remove?: boolean}[]
  requests?: Record<string, string>; limits?: Record<string, string>
}
export interface K8sSidecar {name: string; image: string; command?: string[]; args?: string[]}

/** A Kubernetes cluster reached with a kubeconfig that someone gave the user. The kubeconfig itself stays in the vault. */
export interface Cluster {
  id: string
  label: string
  group_id?: string
  env?: Env
  /** the context chosen when it was added */
  context?: string
  /** the API server of that context, for display */
  server?: string
  /** login programs the user agreed to run for this cluster (a command line each) */
  approved_exec?: string[]
  created_at: string
  updated_at: string
}
export interface KubeContext {name: string; cluster: string; server: string; user: string; namespace?: string; current: boolean; auth: KubeAuth; exec?: string; insecure?: boolean}
/** what a kubeconfig holds: the contexts that can be used, and why the others cannot (by context name) */
export interface ClusterPreview {contexts: KubeContext[]; problems: Record<string, string>}
export interface ClusterInput {label: string; group_id: string; env: Env; path: string; kubeconfig: string; context: string}

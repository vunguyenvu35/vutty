import {createContext, useContext} from 'react'
import {t} from './i18n'
import type {ExecRun, ExecSpec, InventoryOptions, SSHLink, VaultHealth, 
  CacheReport, ConnectionTest, ExportReport, ForwardStatus, Group, Host, HostFilter, Identity, ImportReport, PortForward, Settings,
  ActivityEntry, KnownHost, QueueJob, QueueSpec, RemoteEntry, SftpInfo, SyncDiff, Snippet, SyncStatus, TransferResult, VaultStatus,
  AiAskOptions, AiAttachment, AiCommandCheck, AiCustomProvider, AiModelList, AiPolicy, AiPreview, AiProvider, AiSessionState, AiSettings,
  McpImportItem, McpServer, McpServerInput, McpTool, ServerContainers, ServerInfo, ServerOverview, ServerPorts, ServerProcess, ServerUnit,
} from './types'

/**
 * Everything the UI needs from the Go backend. The real implementation talks to the
 * Wails bindings; tests provide a fake. No method ever returns a secret.
 */
export interface UpdateInfo {current: string; latest: string; available: boolean; url: string; notes: string}
/** whether this copy of the app can replace itself with a newer release, and if not, why */
export interface SelfUpdateStatus {supported: boolean; reason?: string}
/** what an update that installs itself is doing (the `update:progress` event) */
export interface UpdateProgress {stage: 'checking' | 'downloading' | 'verifying' | 'installing' | 'restarting'; done?: number; total?: number}

export interface Backend {
  vault: {
    status(): Promise<VaultStatus>
    create(pw: string, hint?: string): Promise<void>
    unlock(pw: string): Promise<void>
    unlockBiometric(): Promise<void>
    lock(): Promise<void>
    resetVault(confirm: string): Promise<void>
    changePassword(oldPW: string, newPW: string): Promise<void>
    setAutoLock(minutes: number): Promise<void>
    setHint(hint: string): Promise<void>
    enableBiometric(): Promise<void>
    disableBiometric(): Promise<void>
    listIdentities(): Promise<Identity[]>
    /** opens every saved secret to find the ones the vault's key cannot read (they have to be entered again) */
    health(): Promise<VaultHealth>
    addKey(name: string, path: string, passphrase: string): Promise<Identity>
    addKeyPair(name: string, pem: string, passphrase: string, publicKey: string, certificate: string): Promise<Identity>
    addKeyPEM(name: string, pem: string, passphrase: string): Promise<Identity>
    addPassword(name: string, user: string, pw: string): Promise<Identity>
    /** changes an identity where it is (the hosts that use it keep using it); a non-empty secret replaces the private key or the password */
    updateIdentity(id: string, name: string, user: string, secret: string, passphrase: string, publicKey: string, certificate: string): Promise<Identity>
    addAgent(name: string, user: string): Promise<Identity>
    generateKey(name: string): Promise<Identity>
    deleteIdentity(id: string): Promise<void>
  }
  hosts: {
    listGroups(): Promise<Group[]>
    saveGroup(g: Partial<Group>): Promise<Group>
    deleteGroup(id: string): Promise<void>
    listHosts(f: Partial<HostFilter>): Promise<Host[]>
    saveHost(h: Partial<Host>): Promise<Host>
    deleteHost(id: string): Promise<void>
    /** keeps the Telnet password of a host in the vault (empty forgets it) */
    setTelnetPassword(hostId: string, password: string): Promise<void>
  }
  session: {
    open(hostId: string, cols: number, rows: number): Promise<string>
    attach(sid: string): Promise<void>
    /** flow control: the terminal has parsed this many bytes of the session's output since it attached (a running total) */
    ack(sid: string, parsed: number): Promise<void>
    write(sid: string, data: string): Promise<void>
    resize(sid: string, cols: number, rows: number): Promise<void>
    /** the folder the shell of a local terminal is in ("" for the others: they report it themselves, or not at all) */
    cwd(sid: string): Promise<string>
    reconnect(sid: string, cols: number, rows: number): Promise<void>
    close(sid: string): Promise<void>
    /** commands in the shell history of the server, newest first (only when Suggest from host history is on) */
    hostHistory(sid: string): Promise<string[]>
    trustHostKey(sid: string, fingerprint: string): Promise<void>
    /** answers a `term:auth:<key>` question (a verification code…): one answer per prompt, or null to cancel the sign-in */
    answerAuth(key: string, answers: string[] | null): Promise<void>
    testConnection(host: Partial<Host>, password: string, promptKey: string): Promise<ConnectionTest>
  }
  forwards: {
    list(): Promise<ForwardStatus[]>
    save(f: Partial<PortForward>): Promise<PortForward>
    remove(id: string): Promise<void>
    start(id: string): Promise<void>
    stop(id: string): Promise<void>
  }
  sync: {
    configure(url: string): Promise<void>
    configureFolder(dir: string): Promise<void>
    syncNow(): Promise<void>
    join(masterPassword: string): Promise<void>
    /** this machine has its own vault and the repository belongs to another: take that vault's key and merge into it */
    joinRemoteVault(masterPassword: string): Promise<void>
    /** the repository cannot be opened by anyone: make it hold this vault's data (the old files are kept in backups) */
    replaceRemote(): Promise<void>
    status(): Promise<SyncStatus>
  }
  data: {
    importSSHConfig(path: string, conflict: string): Promise<ImportReport>
    /** adds the hosts of a listing you made with your own tools (aws ec2 describe-instances, gcloud compute instances list, ansible-inventory --list, an Ansible INI file or a CSV) */
    importInventory(path: string, conflict: string, opts: InventoryOptions): Promise<ImportReport>
    exportSSHConfig(dir: string, withKeys: boolean): Promise<ExportReport>
    exportPlainJSON(path: string): Promise<void>
    exportBackup(path: string, pw: string): Promise<void>
    importBackup(path: string, pw: string): Promise<void>
  }
  settings: {
    get(): Promise<Settings>
    save(s: Settings): Promise<Settings>
    /** every preference back to its default (the account profile and the sync target stay) */
    reset(): Promise<Settings>
    /** removes what the app can make again; nothing the user created */
    clearCache(): Promise<CacheReport>
    /** the folder session logs are written to now (the chosen one, else the default) */
    sessionLogFolder(): Promise<string>
  }
  sftp: {
    open(hostId: string, promptKey: string): Promise<SftpInfo>
    list(sid: string, dir: string): Promise<RemoteEntry[]>
    mkdir(sid: string, path: string): Promise<void>
    rename(sid: string, from: string, to: string): Promise<void>
    chmod(sid: string, path: string, mode: number): Promise<void>
    remove(sid: string, path: string, recursive: boolean): Promise<void>
    upload(sid: string, local: string, remoteDir: string, overwrite: boolean): Promise<string>
    download(sid: string, remote: string, local: string, overwrite: boolean): Promise<void>
    close(sid: string): Promise<void>
    /** Files and folders (everything inside) into a remote folder. Existing files are skipped unless overwrite. */
    uploadAll(sid: string, locals: string[], remoteDir: string, overwrite: boolean): Promise<TransferResult>
    /** Remote files and folders into a local folder. Existing files are skipped unless overwrite. */
    downloadAll(sid: string, remotes: string[], localDir: string, overwrite: boolean): Promise<TransferResult>
    /** Names that already exist at the destination (relative to it), so the UI can ask once before replacing. */
    conflicts(sid: string, direction: 'upload' | 'download', sources: string[], destDir: string): Promise<string[]>
    /** Adds a transfer to the queue and returns its id at once; it runs in the background, two at a time. */
    enqueue(sid: string, spec: QueueSpec): Promise<string>
    /** What syncing srcDir into dstDir would copy. Nothing is ever deleted. */
    syncPlan(sid: string, direction: 'sync-up' | 'sync-down', srcDir: string, dstDir: string): Promise<SyncDiff>
    queue(sid: string): Promise<QueueJob[]>
    pause(sid: string, jobId: string): Promise<void>
    /** Continues a paused job from the bytes already sent. */
    resume(sid: string, jobId: string): Promise<void>
    cancel(sid: string, jobId: string): Promise<void>
    /** Starts a failed or canceled job again, continuing a partial file. */
    retry(sid: string, jobId: string): Promise<void>
    /** Drops the done, failed and canceled jobs from the list. */
    clearFinished(sid: string): Promise<void>
    /** The file at localPath was fetched from remotePath and opened: save it back to the server each time it changes. */
    editStart(sid: string, remotePath: string, localPath: string): Promise<void>
    /** Saves the edited file now; force replaces a file that changed on the server meanwhile. */
    editPush(sid: string, remotePath: string, force: boolean): Promise<void>
    editStop(sid: string, remotePath: string): Promise<void>
  }
  serial: {
    ports(): Promise<string[]>
  }
  knownHosts: {
    list(): Promise<KnownHost[]>
    /** Forget a host's key: the next connection asks to trust it again. */
    forget(host: string, fingerprint: string): Promise<void>
  }
  logs: {
    list(f: {kinds: string[]; query: string; limit: number}): Promise<ActivityEntry[]>
    clear(): Promise<void>
  }
  /**
   * The AI terminal assistant. What an agent receives and what it says is redacted by the Go side; nothing
   * here ever returns a secret. Events of a question: assistant:chunk|step|confirm|done|error:<id>.
   */
  assistant: {
    providers(): Promise<AiProvider[]>
    refreshProviders(): Promise<AiProvider[]>
    /** rejects with the reason when the provider is not ready or fails the isolation self-test */
    setProviderEnabled(id: string, on: boolean): Promise<void>
    setDefaultProvider(id: string): Promise<void>
    setProviderModel(id: string, model: string): Promise<void>
    models(id: string, refresh: boolean): Promise<AiModelList>
    settings(): Promise<AiSettings>
    saveSettings(s: AiSettings): Promise<AiSettings>
    saveCustomProvider(p: AiCustomProvider): Promise<void>
    deleteCustomProvider(id: string): Promise<void>
    sessionInfo(sid: string): Promise<AiSessionState>
    setHostPolicy(hostId: string, policy: AiPolicy): Promise<void>
    confirmShare(sid: string): Promise<void>
    resetConversation(sid: string): Promise<void>
    preview(sid: string, lines: number, selection: string, attachments?: AiAttachment[]): Promise<AiPreview>
    /** returns the request id; rejects with "confirm_share" when the user must confirm sharing first */
    ask(sid: string, question: string, opts: AiAskOptions): Promise<string>
    cancel(id: string): Promise<void>
    confirmTool(runId: string, callId: string, allow: boolean): Promise<void>
    checkCommand(sid: string, cmd: string): Promise<AiCommandCheck>
    /** puts the command on the prompt without pressing Enter */
    insert(sid: string, cmd: string): Promise<void>
    /** presses Enter too; `typed` must be "run" for a risky command */
    run(sid: string, cmd: string, typed: string): Promise<void>
    /** runs a command that only reads, without asking; refused for anything else */
    runReadOnly(sid: string, cmd: string): Promise<void>
    mcp: {
      list(): Promise<McpServer[]>
      save(s: McpServerInput): Promise<McpServer>
      remove(id: string): Promise<void>
      test(id: string): Promise<McpTool[]>
      setTool(serverId: string, tool: string, mode: string): Promise<void>
      approveNew(serverId: string): Promise<void>
      importable(): Promise<McpImportItem[]>
      import(ref: string): Promise<McpServer>
    }
  }
  /** One command on several hosts at once, each on its own connection and without a terminal. */
  exec: {
    /** starts the run and returns its id; progress arrives as `exec:update:<id>` events that carry the whole run */
    run(spec: ExecSpec): Promise<string>
    cancel(id: string): Promise<void>
    get(id: string): Promise<ExecRun>
    /** drops the run and what it printed */
    forget(id: string): Promise<void>
  }
  /** The Server section of the sidebar: fixed commands over SSH on a connection of its own, nothing installed on the server. */
  server: {
    open(hostId: string, promptKey: string): Promise<ServerInfo>
    close(sid: string): Promise<void>
    overview(sid: string): Promise<ServerOverview>
    processes(sid: string): Promise<ServerProcess[]>
    kill(sid: string, pid: number, signal: 'TERM' | 'KILL'): Promise<void>
    services(sid: string): Promise<ServerUnit[]>
    serviceAction(sid: string, name: string, action: 'start' | 'stop' | 'restart' | 'enable' | 'disable'): Promise<void>
    containers(sid: string): Promise<ServerContainers>
    containerAction(sid: string, name: string, action: 'start' | 'stop' | 'restart'): Promise<void>
    /** the last lines of the journal: kind is '' (the whole system), 'service' or 'container' */
    logs(sid: string, kind: '' | 'service' | 'container', name: string): Promise<string>
    /** the TCP ports something listens on, with what the ufw firewall says about each (read only: the panel never changes the firewall) */
    ports(sid: string): Promise<ServerPorts>
  }
  /** This computer's files (the Local pane of the SFTP screen). */
  local: {
    home(): Promise<string>
    list(dir: string): Promise<RemoteEntry[]>
    mkdir(path: string): Promise<void>
    rename(from: string, to: string): Promise<void>
    remove(path: string, recursive: boolean): Promise<void>
    chmod(path: string, mode: number): Promise<void>
    /** a new private folder for files that are only looked at */
    tempDir(): Promise<string>
    /** the files that copying sources into destDir would replace (asked about once) */
    conflicts(sources: string[], destDir: string): Promise<string[]>
    /** copies files and folders into a folder of this computer; existing files stay unless overwrite */
    copy(sources: string[], destDir: string, overwrite: boolean): Promise<TransferResult>
  }
  snippets: {
    list(): Promise<Snippet[]>
    save(s: Partial<Snippet>): Promise<Snippet>
    remove(id: string): Promise<void>
  }
  system: {
    pickFile(title: string): Promise<string>
    pickDirectory(title: string): Promise<string>
    saveFile(title: string, name: string): Promise<string>
    openUrl(url: string): Promise<void>
    /** Tells the native menu bar which language to show (en|vi|ja|zh). */
    setLanguage(lang: string): Promise<void>
    info(): Promise<{version: string}>
    /** Asks GitHub for the latest release and compares it with this build. */
    checkUpdate(): Promise<UpdateInfo>
    selfUpdateStatus(): Promise<SelfUpdateStatus>
    /** downloads the latest release, checks its signature and checksum, replaces this app with it and restarts: returns only when that failed (with the reason) */
    installUpdate(): Promise<void>
    /** Opens the Settings window (a second native window). Rejects where none exists: the UI then shows its in-app window. */
    /** the window is in full screen (the macOS window buttons are gone then) */
    isFullscreen(): Promise<boolean>
    /** tells the native menu bar the effective shortcuts, so each menu item shows its key (macOS) */
    setMenuShortcuts(shortcuts: Record<string, string>): Promise<void>
    /** Opens a new native window with a new session to the same place (host id, "local", "serial:..."). Rejects where there is none. */
    openWindow(hostId: string, title: string): Promise<void>
    /** Opens a file with the app the system has for it (never a file that runs programs). */
    openPath(path: string): Promise<void>
    /** Opens a file with the chosen application. */
    openPathWith(path: string, app: string): Promise<void>
    /** Zoom the native window (the green button / a double click on the title bar). A no-op in a browser. */
    toggleMaximise(): Promise<void>
    /** The system clipboard: through the native runtime (a webview often refuses to read it), else the browser. */
    readClipboard(): Promise<string>
    writeClipboard(text: string): Promise<void>
    /** the ssh:// link the system handed to the app that has not been looked at yet (null when none); also announced as `app:open-ssh` */
    takePendingSSHLink(): Promise<SSHLink | null>
  }
  /** Subscribe to a backend event; returns an unsubscribe function. */
  on(event: string, cb: (data: any) => void): () => void
}

type Bound = Record<string, (...a: any[]) => Promise<any>>

declare global {
  interface Window {
    go?: Record<string, Record<string, Bound>>
    runtime?: {EventsOn(name: string, cb: (...d: any[]) => void): () => void; WindowToggleMaximise?(): void; WindowIsFullscreen?(): Promise<boolean>; Quit?(): void}
  }
}

function svc(pkg: string, name: string): Bound {
  const s = window.go?.[pkg]?.[name]
  if (!s) {
    console.warn(`backend service ${pkg}.${name} is not available`)
    throw new Error(t('Vutty cannot reach its backend from this window. Close the window and open it again; if it keeps happening, restart Vutty.'))
  }
  return s
}

/** Backend backed by the Wails-generated bindings on window.go. */
export function wailsBackend(): Backend {
  const U = (n: string) => svc('usecase', n)
  const call = <T,>(pkg: string, n: string, m: string, ...a: any[]) => svc(pkg, n)[m](...a) as Promise<T>
  const u = <T,>(n: string, m: string, ...a: any[]) => call<T>('usecase', n, m, ...a)
  void U
  return {
    vault: {
      status: () => u('VaultService', 'Status'),
      create: (pw, hint) => hint?.trim() ? u('VaultService', 'CreateWithHint', pw, hint) : u('VaultService', 'Create', pw),
      unlock: pw => u('VaultService', 'Unlock', pw),
      unlockBiometric: () => u('VaultService', 'UnlockBiometric'),
      lock: () => u('VaultService', 'Lock'),
      resetVault: c => u('VaultService', 'ResetVault', c),
      changePassword: (o, n) => u('VaultService', 'ChangePassword', o, n),
      setAutoLock: m => u('VaultService', 'SetAutoLock', m),
      setHint: h => u('VaultService', 'SetHint', h),
      enableBiometric: () => u('VaultService', 'EnableBiometric'),
      disableBiometric: () => u('VaultService', 'DisableBiometric'),
      listIdentities: () => u('VaultService', 'ListIdentities'),
      health: async () => { const h = await u<VaultHealth>('VaultService', 'Health'); return {...h, unreadable: h.unreadable ?? []} },
      addKey: (n, p, pp) => u('VaultService', 'AddKey', n, p, pp),
      addKeyPEM: (n, p, pp) => u('VaultService', 'AddKeyPEM', n, p, pp),
      addKeyPair: (n, p, pp, pub, cert) => u('VaultService', 'AddKeyPair', n, p, pp, pub, cert),
      addPassword: (n, user, pw) => u('VaultService', 'AddPassword', n, user, pw),
      updateIdentity: (id, n, user, s, pp, pub, cert) => u('VaultService', 'UpdateIdentity', id, n, user, s, pp, pub, cert),
      addAgent: (n, user) => u('VaultService', 'AddAgent', n, user),
      generateKey: n => u('VaultService', 'GenerateKey', n, 'ed25519'),
      deleteIdentity: id => u('VaultService', 'DeleteIdentity', id),
    },
    hosts: {
      listGroups: () => u('HostService', 'ListGroups'),
      saveGroup: g => u('HostService', 'SaveGroup', g),
      deleteGroup: id => u('HostService', 'DeleteGroup', id),
      listHosts: f => u('HostService', 'ListHosts', {query: '', group_id: '', env: '', tag: '', ...f}),
      saveHost: h => u('HostService', 'SaveHost', h),
      deleteHost: id => u('HostService', 'DeleteHost', id),
      setTelnetPassword: (id, pw) => u('HostService', 'SetTelnetPassword', id, pw),
    },
    session: {
      open: (h, c, r) => u('SessionService', 'Open', h, c, r),
      attach: s => u('SessionService', 'Attach', s),
      ack: (s, n) => u('SessionService', 'Ack', s, n),
      write: (s, d) => u('SessionService', 'Write', s, d),
      resize: (s, c, r) => u('SessionService', 'Resize', s, c, r),
      cwd: s => u('SessionService', 'Cwd', s),
      reconnect: (s, c, r) => u('SessionService', 'Reconnect', s, c, r),
      close: s => u('SessionService', 'Close', s),
      hostHistory: async s => (await u<string[]>('SessionService', 'HostHistory', s)) ?? [],
      trustHostKey: (s, f) => u('SessionService', 'TrustHostKey', s, f),
      answerAuth: (k, a) => u('SessionService', 'AnswerAuth', k, a),
      testConnection: (h, p, k) => u('SessionService', 'TestConnection', h, p, k),
    },
    forwards: {
      list: () => u('ForwardService', 'List'),
      save: f => u('ForwardService', 'SaveForward', f),
      remove: id => u('ForwardService', 'DeleteForward', id),
      start: id => u('ForwardService', 'Start', id),
      stop: id => u('ForwardService', 'Stop', id),
    },
    sync: {
      configure: url => u('SyncService', 'Configure', url),
      configureFolder: dir => u('SyncService', 'ConfigureFolder', dir),
      syncNow: () => u('SyncService', 'SyncNow'),
      join: pw => u('SyncService', 'JoinExisting', pw),
      joinRemoteVault: pw => u('SyncService', 'JoinRemoteVault', pw),
      replaceRemote: () => u('SyncService', 'ReplaceRemote'),
      status: () => u('SyncService', 'Status'),
    },
    data: {
      importSSHConfig: async (p, c) => normalizeImport(await u<ImportReport>('ImportExportService', 'ImportSSHConfig', p, c)),
      importInventory: async (p, c, o) => normalizeImport(await u<ImportReport>('ImportExportService', 'ImportInventory', p, c, o)),
      exportSSHConfig: async (d, k) => normalizeExport(await u<ExportReport>('ImportExportService', 'ExportSSHConfig', d, k)),
      exportPlainJSON: p => u('ImportExportService', 'ExportPlainJSON', p),
      exportBackup: (p, pw) => u('ImportExportService', 'ExportBackup', p, pw),
      importBackup: (p, pw) => u('ImportExportService', 'ImportBackup', p, pw),
    },
    settings: {
      get: () => u('SettingsService', 'Get'),
      save: s => u('SettingsService', 'Save', s),
      reset: () => u('SettingsService', 'ResetSettings'),
      clearCache: () => u('SettingsService', 'ClearCache'),
      sessionLogFolder: () => u('SettingsService', 'SessionLogFolder'),
    },
    sftp: {
      open: (h, k) => u('SftpService', 'Open', h, k),
      list: async (s, d) => (await u<RemoteEntry[]>('SftpService', 'List', s, d)) ?? [],
      mkdir: (s, p) => u('SftpService', 'Mkdir', s, p),
      rename: (s, a, b) => u('SftpService', 'Rename', s, a, b),
      chmod: (s, p, m) => u('SftpService', 'Chmod', s, p, m),
      remove: (s, p, r) => u('SftpService', 'Delete', s, p, r),
      upload: (s, l, d, o) => u('SftpService', 'Upload', s, l, d, o),
      download: (s, r, l, o) => u('SftpService', 'Download', s, r, l, o),
      close: s => u('SftpService', 'Close', s),
      uploadAll: (s, l, d, o) => u('SftpService', 'UploadAll', s, l, d, o),
      downloadAll: (s, r, d, o) => u('SftpService', 'DownloadAll', s, r, d, o),
      conflicts: async (s, dir, src, d) => (await u<string[]>('SftpService', 'Conflicts', s, dir, src, d)) ?? [],
      enqueue: (s, spec) => u('SftpService', 'Enqueue', s, spec),
      syncPlan: async (s, dir, a, b) => {
        const r = await u<SyncDiff>('SftpService', 'SyncPlan', s, dir, a, b)
        return {copy: r?.copy ?? [], same: r?.same ?? 0, extra: r?.extra ?? []}
      },
      queue: async s => (await u<QueueJob[]>('SftpService', 'Queue', s)) ?? [],
      pause: (s, j) => u('SftpService', 'Pause', s, j),
      resume: (s, j) => u('SftpService', 'Resume', s, j),
      cancel: (s, j) => u('SftpService', 'Cancel', s, j),
      retry: (s, j) => u('SftpService', 'Retry', s, j),
      clearFinished: s => u('SftpService', 'ClearFinished', s),
      editStart: (s, r, l) => u('SftpService', 'EditStart', s, r, l),
      editPush: (s, r, f) => u('SftpService', 'EditPush', s, r, f),
      editStop: (s, r) => u('SftpService', 'EditStop', s, r),
    },
    knownHosts: {
      list: async () => (await call<KnownHost[]>('usecase', 'KnownHostsService', 'List')) ?? [],
      forget: (h, f) => call('usecase', 'KnownHostsService', 'Forget', h, f),
    },
    logs: {
      list: async f => (await call<ActivityEntry[]>('usecase', 'LogService', 'List', f)) ?? [],
      clear: () => call('usecase', 'LogService', 'Clear'),
    },
    assistant: {
      providers: async () => (await u<AiProvider[]>('AssistantService', 'Providers')) ?? [],
      refreshProviders: async () => (await u<AiProvider[]>('AssistantService', 'RefreshProviders')) ?? [],
      setProviderEnabled: (id, on) => u('AssistantService', 'SetProviderEnabled', id, on),
      setDefaultProvider: id => u('AssistantService', 'SetDefaultProvider', id),
      setProviderModel: (id, m) => u('AssistantService', 'SetProviderModel', id, m),
      models: async (id, r) => { const l = await u<AiModelList>('AssistantService', 'Models', id, r); return {...l, models: l?.models ?? []} },
      settings: async () => { const s = await u<AiSettings>('AssistantService', 'Settings'); return {...s, patterns: s?.patterns ?? []} },
      saveSettings: async s => { const r = await u<AiSettings>('AssistantService', 'SaveSettings', s); return {...r, patterns: r?.patterns ?? []} },
      saveCustomProvider: p => u('AssistantService', 'SaveCustomProvider', p),
      deleteCustomProvider: id => u('AssistantService', 'DeleteCustomProvider', id),
      sessionInfo: sid => u('AssistantService', 'SessionInfo', sid),
      setHostPolicy: (h, p) => u('AssistantService', 'SetHostPolicy', h, p),
      confirmShare: sid => u('AssistantService', 'ConfirmShare', sid),
      resetConversation: sid => u('AssistantService', 'ResetConversation', sid),
      preview: async (sid, l, sel, att) => { const p = await u<AiPreview>('AssistantService', 'Preview', sid, l, sel, att ?? []); return {...p, findings: p?.findings ?? []} },
      ask: (sid, q, o) => u('AssistantService', 'Ask', sid, q, o),
      cancel: id => u('AssistantService', 'Cancel', id),
      confirmTool: (r, c, a) => u('AssistantService', 'ConfirmTool', r, c, a),
      checkCommand: (sid, cmd) => u('AssistantService', 'CheckCommand', sid, cmd),
      insert: (sid, cmd) => u('AssistantService', 'Insert', sid, cmd),
      run: (sid, cmd, t) => u('AssistantService', 'Run', sid, cmd, t),
      runReadOnly: (sid, cmd) => u('AssistantService', 'RunReadOnly', sid, cmd),
      mcp: {
        list: async () => (await u<McpServer[]>('AssistantService', 'McpServers')) ?? [],
        save: s => u('AssistantService', 'SaveMcpServer', s),
        remove: id => u('AssistantService', 'DeleteMcpServer', id),
        test: async id => (await u<McpTool[]>('AssistantService', 'TestMcpServer', id)) ?? [],
        setTool: (s, t, m) => u('AssistantService', 'SetMcpTool', s, t, m),
        approveNew: id => u('AssistantService', 'ApproveNewTools', id),
        importable: async () => (await u<McpImportItem[]>('AssistantService', 'ImportableMcpServers')) ?? [],
        import: ref => u('AssistantService', 'ImportMcpServer', ref),
      },
    },
    serial: {
      ports: async () => (await call<string[]>('usecase', 'SerialService', 'Ports')) ?? [],
    },
    exec: {
      run: spec => u('ExecService', 'Run', spec),
      cancel: id => u('ExecService', 'Cancel', id),
      get: async id => { const r = await u<ExecRun>('ExecService', 'Get', id); return {...r, results: r.results ?? []} },
      forget: id => u('ExecService', 'Forget', id),
    },
    server: {
      open: (h, k) => u('ServerService', 'Open', h, k),
      close: s => u('ServerService', 'Close', s),
      overview: s => u('ServerService', 'Overview', s),
      processes: async s => (await u<ServerProcess[]>('ServerService', 'Processes', s)) ?? [],
      kill: (s, p, g) => u('ServerService', 'Kill', s, p, g),
      services: async s => (await u<ServerUnit[]>('ServerService', 'Services', s)) ?? [],
      serviceAction: (s, n, a) => u('ServerService', 'ServiceAction', s, n, a),
      containers: async s => { const r = await u<ServerContainers>('ServerService', 'Containers', s); return {...r, containers: r.containers ?? []} },
      containerAction: (s, n, a) => u('ServerService', 'ContainerAction', s, n, a),
      logs: (s, k, n) => u('ServerService', 'Logs', s, k, n),
      ports: async s => { const r = await u<ServerPorts>('ServerService', 'Ports', s); return {...r, ports: r.ports ?? []} },
    },
    local: {
      home: () => call('usecase', 'LocalService', 'Home'),
      list: async d => (await call<RemoteEntry[]>('usecase', 'LocalService', 'List', d)) ?? [],
      mkdir: p => call('usecase', 'LocalService', 'Mkdir', p),
      rename: (a, b) => call('usecase', 'LocalService', 'Rename', a, b),
      remove: (p, r) => call('usecase', 'LocalService', 'Delete', p, r),
      chmod: (p, m) => call('usecase', 'LocalService', 'Chmod', p, m),
      tempDir: () => call('usecase', 'LocalService', 'TempDir'),
      conflicts: async (s, d) => (await call<string[]>('usecase', 'LocalService', 'Conflicts', s, d)) ?? [],
      copy: (s, d, o) => call('usecase', 'LocalService', 'Copy', s, d, o),
    },
    snippets: {
      list: async () => (await u<Snippet[]>('SnippetService', 'List')) ?? [],
      save: s => u('SnippetService', 'Save', s),
      remove: id => u('SnippetService', 'Delete', id),
    },
    system: {
      setMenuShortcuts: s => call('app', 'SystemService', 'SetMenuShortcuts', s),
      pickFile: t => call('app', 'SystemService', 'PickFile', t),
      pickDirectory: t => call('app', 'SystemService', 'PickDirectory', t),
      saveFile: (t, n) => call('app', 'SystemService', 'SaveFile', t, n),
      setLanguage: l => call('app', 'SystemService', 'SetLanguage', l),
      openUrl: async url => {
        try {
          await call('app', 'SystemService', 'OpenURL', url)
        } catch {
          window.open(url, '_blank', 'noopener') // browser mode (e2e)
        }
      },
      info: () => call('app', 'SystemService', 'Info'),
      checkUpdate: () => call('app', 'SystemService', 'CheckUpdate'),
      selfUpdateStatus: async () => { try { return await call<SelfUpdateStatus>('app', 'SystemService', 'SelfUpdateStatus') } catch { return {supported: false} } },
      installUpdate: () => call('app', 'SystemService', 'InstallUpdate'),
      openWindow: (h, t) => call('app', 'SystemService', 'OpenWindow', h, t),
      openPath: p => call('app', 'SystemService', 'OpenPath', p),
      openPathWith: (p, a) => call('app', 'SystemService', 'OpenPathWith', p, a),
      toggleMaximise: async () => { window.runtime?.WindowToggleMaximise?.() },
      isFullscreen: async () => (await window.runtime?.WindowIsFullscreen?.()) ?? false,
      readClipboard: async () => {
        try { return String((await call('app', 'SystemService', 'ClipboardText')) ?? '') } catch { return navigator.clipboard.readText() }
      },
      writeClipboard: async text => {
        try { await call('app', 'SystemService', 'SetClipboardText', text) } catch { await navigator.clipboard.writeText(text) }
      },
      takePendingSSHLink: async () => {
        try {
          const l = await call<SSHLink>('app', 'SystemService', 'TakePendingSSHLink')
          return l?.hostname ? l : null
        } catch { return null } // a browser has no system to hand it a link
      },
    },
    on: (event, cb) => {
      const rt = window.runtime
      if (!rt) return () => {}
      return rt.EventsOn(event, (...d: any[]) => cb(d[0]))
    },
  }
}

/** True in a terminal window process (Duplicate in new window): it has the TerminalWindow object bound. */
export const inTerminalWindow = () => !!window.go?.app?.TerminalWindow

/** What a terminal window was opened for, as the Go side was told. */
export async function windowTarget(): Promise<{host_id: string; title: string}> {
  return window.go!.app.TerminalWindow.Target() as unknown as {host_id: string; title: string}
}

/**
 * An app window (Duplicate in new window) is the whole app in another process: it holds no use cases of its own.
 * Every call the app makes on window.go.usecase.<Service>.<Method> is sent to the main window's process through
 * the Go bridge, and what the main process emits (sessions' output, hosts changed...) comes back as local events.
 * Only the native parts (dialogs, clipboard) stay with this process.
 */
export function installWindowBridge() {
  const call = (service: string, method: string) => async (...args: unknown[]) => {
    const text: string = await window.go!.app.TerminalWindow.Call(service, method, JSON.stringify(args))
    return text ? JSON.parse(text) : undefined
  }
  const usecase = new Proxy({}, {get: (_t, service: string) => new Proxy({}, {get: (_u, method: string) => call(service, method)})})
  window.go!.usecase = usecase as never
}

/** Go marshals nil slices as null; the UI relies on arrays, so normalise at the boundary. */
export function normalizeImport(r: ImportReport): ImportReport {
  return {...r, skipped: r?.skipped ?? [], warnings: r?.warnings ?? []}
}

export function normalizeExport(r: ExportReport): ExportReport {
  return {...r, files: r?.files ?? [], key_files: r?.key_files ?? [], warnings: r?.warnings ?? []}
}

export const BackendContext = createContext<Backend | null>(null)

export function useBackend(): Backend {
  const b = useContext(BackendContext)
  if (!b) throw new Error('BackendContext is missing')
  return b
}

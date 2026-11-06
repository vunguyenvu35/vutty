import {createContext, useContext} from 'react'
import {t} from './i18n'
import type {Cluster, K8sContainerEdit, K8sEditable, K8sEditPreview, K8sSidecar, K8sWorkloadKind, K8sWorkloadSpec, ClusterInput, ClusterPreview, K8sAccess, K8sCheck, K8sConfig, K8sConfigItem, K8sContexts, K8sDeployment, K8sDetect, K8sDrainOpts, K8sDrainPlan, K8sEvent, K8sAPIResource, K8sResRef, K8sObjList, K8sMultiLogs, K8sHelmRelease, K8sHelmDetail, K8sSecurity, K8sDebug, K8sForward, K8sLogOpts, K8sNamespaces, K8sNetwork, K8sNode, K8sOverview, K8sPod, K8sPodDetail, K8sWorkloads} from './types'
import type {ServerUsers, UserKey, DockerInspect, DockerObjects, DockerStat, ExecRun, ExecSpec, InventoryOptions, SSHLink, VaultHealth, 
  CacheReport, ConnectionTest, ExportReport, ForwardStatus, Group, Host, HostFilter, Identity, ImportReport, PortForward, Settings,
  ActivityEntry, KnownHost, QueueJob, QueueSpec, RemoteEntry, SftpInfo, SyncDiff, Snippet, SyncStatus, TransferResult, VaultStatus,
  AiAskOptions, AiAttachment, AiCommandCheck, AiCustomProvider, AiModelList, AiPolicy, AiPreview, AiProvider, AiSessionState, AiSettings,
  McpImportItem, McpServer, McpServerInput, McpTool, FirewallRule, ServerBackup, ServerCerts, ServerCleanup, ServerContainers, ServerFleetHost, ServerFolder, ServerInfo, ServerOverview, ServerPorts, ServerProcess, ServerSchedule, ServerSecurity, ServerSystem, ServerUnit, ServerUpdates, ServerWeb, ServerWebResult,
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
    reboot(sid: string): Promise<void>
    serviceAction(sid: string, name: string, action: 'start' | 'stop' | 'restart' | 'enable' | 'disable'): Promise<void>
    containers(sid: string): Promise<ServerContainers>
    containerAction(sid: string, name: string, action: 'start' | 'stop' | 'restart'): Promise<void>
    /** the line to type into a terminal tab on the host to get a shell in a container; nothing is run */
    dockerShellCommand(sid: string, name: string): Promise<string>
    /** one reading of the CPU and memory of the running containers */
    dockerStats(sid: string): Promise<DockerStat[]>
    dockerInspect(sid: string, name: string): Promise<DockerInspect>
    dockerObjects(sid: string): Promise<DockerObjects>
    /** delete an image (by id) or a volume (by name); Docker refuses one in use */
    dockerRemove(sid: string, kind: 'image' | 'volume', id: string): Promise<void>
    /** the last lines of the journal: kind is '' (the whole system), 'service', 'container', 'stack' (a Compose project) or 'web' (name: nginx or caddy) */
    /** the last lines of a log; opts is for another number of lines (100, 300, 1000, 5000) and a time (15m, 1h, 24h) */
    logs(sid: string, kind: '' | 'service' | 'container' | 'stack' | 'web', name: string, opts?: {lines: number; since: '' | '15m' | '1h' | '24h'}): Promise<string>
    /** start, stop or restart every container of a Compose project, or 'update' it (pull the images, then up -d) */
    stackAction(sid: string, project: string, action: 'start' | 'stop' | 'restart' | 'update'): Promise<void>
    certificates(sid: string): Promise<ServerCerts>
    web(sid: string): Promise<ServerWeb>
    /** nginx -t / caddy validate: changes nothing */
    webTest(sid: string, kind: 'nginx' | 'caddy'): Promise<ServerWebResult>
    /** tests the configuration and reloads only when it passed */
    webReload(sid: string, kind: 'nginx' | 'caddy'): Promise<ServerWebResult>
    updates(sid: string): Promise<ServerUpdates>
    system(sid: string): Promise<ServerSystem>
    setHostname(sid: string, name: string): Promise<void>
    setTimezone(sid: string, zone: string): Promise<void>
    /** create /swapfile of this many MB, turn it on and keep it for the next boot */
    swapCreate(sid: string, mb: number): Promise<void>
    swapRemove(sid: string): Promise<void>
    /** refreshes the package lists (apt-get update, dnf makecache) */
    updatesRefresh(sid: string): Promise<void>
    cleanup(sid: string): Promise<ServerCleanup>
    /** runs one cleanup by id (see ServerReclaim); docker-volumes is refused */
    cleanupRun(sid: string, id: string): Promise<void>
    /** the folders directly under path (an absolute path of letters, digits and . _ - /), biggest first */
    diskUsage(sid: string, path: string): Promise<ServerFolder[]>
    security(sid: string): Promise<ServerSecurity>
    /** the accounts that can log in (with sudo and the number of SSH keys), and the logins open now */
    users(sid: string): Promise<ServerUsers>
    addUser(sid: string, name: string, shell: string, sudo: boolean, key: string): Promise<void>
    deleteUser(sid: string, name: string, removeHome: boolean): Promise<void>
    userKeys(sid: string, name: string): Promise<UserKey[]>
    addUserKey(sid: string, name: string, key: string): Promise<void>
    removeUserKey(sid: string, name: string, fingerprint: string): Promise<void>
    unban(sid: string, jail: string, ip: string): Promise<void>
    schedule(sid: string): Promise<ServerSchedule>
    cronAdd(sid: string, owner: string, schedule: string, command: string): Promise<void>
    cronUpdate(sid: string, owner: string, oldSchedule: string, oldCommand: string, schedule: string, command: string): Promise<void>
    cronDelete(sid: string, owner: string, schedule: string, command: string): Promise<void>
    /** the newest file under path on the server */
    backup(sid: string, path: string): Promise<ServerBackup>
    /** one saved host: connects on its own, answers or says why not (never asks a question) */
    fleetHost(hostId: string): Promise<ServerFleetHost>
    /** the TCP ports something listens on, with what the ufw firewall says about each, and its rules */
    ports(sid: string): Promise<ServerPorts>
    /** allow or deny a port (proto '' = tcp and udp, from '' = anyone). force: the port is the one of this connection */
    firewallAdd(sid: string, action: 'allow' | 'deny', port: number, proto: string, from: string, force: boolean): Promise<void>
    /** delete a rule, found again by its number and its text so that another change cannot make it a different rule */
    firewallDelete(sid: string, rule: FirewallRule, force: boolean): Promise<void>
    firewallEnable(sid: string): Promise<void>
    firewallDisable(sid: string): Promise<void>
    /** The Kubernetes tab: kubectl on the host. ctx is the kubeconfig context, ns the namespace ('' = all, for lists). Every change asks first (the screen) and is audited (Go). */
    k8sDetect(sid: string): Promise<K8sDetect>
    k8sContexts(sid: string): Promise<K8sContexts>
    k8sCheck(sid: string, ctx: string): Promise<K8sCheck>
    k8sAccess(sid: string, ctx: string, ns: string): Promise<K8sAccess>
    k8sNamespaces(sid: string, ctx: string): Promise<K8sNamespaces>
    /** ns "" is every namespace; a login that may only look inside its own namespaces needs one */
    k8sOverview(sid: string, ctx: string, ns?: string): Promise<K8sOverview>
    k8sNodes(sid: string, ctx: string): Promise<K8sNode[]>
    k8sPods(sid: string, ctx: string, ns: string): Promise<K8sPod[]>
    k8sPod(sid: string, ctx: string, ns: string, name: string): Promise<K8sPodDetail>
    k8sWorkloads(sid: string, ctx: string, ns: string): Promise<K8sWorkloads>
    k8sDeployment(sid: string, ctx: string, ns: string, name: string): Promise<K8sDeployment>
    k8sNetwork(sid: string, ctx: string, ns: string): Promise<K8sNetwork>
    k8sConfig(sid: string, ctx: string, ns: string): Promise<K8sConfig>
    k8sConfigMap(sid: string, ctx: string, ns: string, name: string): Promise<Record<string, string>>
    k8sEvents(sid: string, ctx: string, ns: string): Promise<K8sEvent[]>
    k8sLogs(sid: string, ctx: string, ns: string, pod: string, opts: K8sLogOpts): Promise<string>
    k8sLogsMulti(sid: string, ctx: string, ns: string, kind: string, name: string, opts: K8sLogOpts): Promise<K8sMultiLogs>
    k8sYAML(sid: string, ctx: string, kind: string, ns: string, name: string): Promise<string>
    k8sCounts(sid: string, ctx: string, ns: string): Promise<Record<string, number>>
    k8sSecurity(sid: string, ctx: string, ns: string): Promise<K8sSecurity>
    k8sDebugStart(sid: string, ctx: string, ns: string, pod: string, image: string, target: string): Promise<K8sDebug>
    k8sHelmReleases(sid: string, ctx: string, ns: string): Promise<K8sHelmRelease[]>
    k8sHelmRelease(sid: string, ctx: string, ns: string, name: string, revision: number): Promise<K8sHelmDetail>
    k8sHelmValues(sid: string, ctx: string, ns: string, name: string, revision: number): Promise<string>
    podFilesHome(hostId: string): Promise<string>
    podFilesList(hostId: string, dir: string): Promise<RemoteEntry[]>
    podFilesFetch(hostId: string, remote: string, localDir: string): Promise<string>
    k8sResources(sid: string, ctx: string): Promise<K8sAPIResource[]>
    k8sObjects(sid: string, ctx: string, ns: string, ref: K8sResRef): Promise<K8sObjList>
    k8sObjectYAML(sid: string, ctx: string, ns: string, ref: K8sResRef, name: string): Promise<string>
    k8sDrainPlan(sid: string, ctx: string, node: string, opts: K8sDrainOpts): Promise<K8sDrainPlan>
    k8sScale(sid: string, ctx: string, ns: string, name: string, replicas: number): Promise<void>
    k8sRestart(sid: string, ctx: string, ns: string, kind: 'deployment' | 'statefulset' | 'daemonset', name: string): Promise<void>
    k8sRolloutPause(sid: string, ctx: string, ns: string, name: string, pause: boolean): Promise<void>
    k8sRollback(sid: string, ctx: string, ns: string, name: string, revision: number): Promise<void>
    k8sDeletePod(sid: string, ctx: string, ns: string, name: string): Promise<void>
    k8sDeleteJob(sid: string, ctx: string, ns: string, name: string): Promise<void>
    k8sCronJob(sid: string, ctx: string, ns: string, name: string, action: 'run' | 'suspend' | 'resume'): Promise<void>
    k8sCordon(sid: string, ctx: string, node: string, on: boolean): Promise<void>
    k8sDrain(sid: string, ctx: string, node: string, opts: K8sDrainOpts): Promise<void>
    /** the line to type into a terminal tab on the same host (kubectl exec -it ...) */
    k8sShellCommand(sid: string, ctx: string, ns: string, pod: string, container: string): Promise<string>
    /** a shell in a pod of a cluster opened with a kubeconfig: the id of a terminal tab (host_id) that opens it, and a name for the tab */
    k8sShellSession(sid: string, ctx: string, ns: string, pod: string, container: string): Promise<{host_id: string; title: string}>
    /** the Rancher-style menu of a Deployment, StatefulSet or DaemonSet. Reading and the changes (preview, apply, set container, sidecar) work for a host and a cluster */
    k8sWorkloadSpec(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string): Promise<K8sWorkloadSpec>
    k8sEditable(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string): Promise<K8sEditable>
    /** the path of the file written in the Downloads folder */
    k8sSaveYAML(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string): Promise<string>
    k8sEditPreview(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string, yaml: string): Promise<K8sEditPreview>
    k8sEditApply(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string, yaml: string): Promise<void>
    k8sSetContainer(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string, edit: K8sContainerEdit): Promise<void>
    k8sAddSidecar(sid: string, ctx: string, kind: K8sWorkloadKind, ns: string, name: string, sidecar: K8sSidecar): Promise<void>
    k8sPortForwardStart(sid: string, ctx: string, ns: string, service: string, local: number, remote: number): Promise<K8sForward>
    k8sPortForwardStop(sid: string, id: string): Promise<void>
    k8sPortForwards(sid: string): Promise<K8sForward[]>
    /** one key of a Secret, decoded; audited */
    k8sSecretKey(sid: string, ctx: string, ns: string, name: string, key: string): Promise<string>
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
  /** Kubernetes clusters reached with a kubeconfig (no SSH host). The kubeconfig goes into the vault and never comes back. path is a file the person picked; text is the fallback when no dialog is available. */
  clusters: {
    list(): Promise<Cluster[]>
    /** what a kubeconfig holds, before it is saved */
    preview(path: string, text: string): Promise<ClusterPreview>
    /** the contexts of a saved cluster */
    describe(id: string): Promise<ClusterPreview>
    add(c: ClusterInput): Promise<Cluster>
    /** name, group, environment and context only */
    update(c: Cluster): Promise<Cluster>
    replaceKubeconfig(id: string, path: string, text: string): Promise<Cluster>
    /** agrees that the login program of the kubeconfig (a command line shown to the person) may run */
    approveExec(id: string, command: string): Promise<Cluster>
    /** takes the approval of a login program back */
    revokeExec(id: string, command: string): Promise<Cluster>
    remove(id: string): Promise<void>
    /** opens a session; its sid goes to the same server.k8s* calls, and server.close ends it */
    open(id: string): Promise<ServerInfo>
  }
  system: {
    pickFile(title: string): Promise<string>
    pickDirectory(title: string): Promise<string>
    saveFile(title: string, name: string): Promise<string>
    /** asks where to save, then writes the text there: the path, or '' when it was cancelled */
    saveText(title: string, name: string, text: string): Promise<string>
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
      reboot: s => u('ServerService', 'Reboot', s),
      serviceAction: (s, n, a) => u('ServerService', 'ServiceAction', s, n, a),
      containers: async s => { const r = await u<ServerContainers>('ServerService', 'Containers', s); return {...r, containers: r.containers ?? []} },
      containerAction: (s, n, a) => u('ServerService', 'ContainerAction', s, n, a),
      dockerShellCommand: (s, n) => u('ServerService', 'DockerShellCommand', s, n),
      dockerStats: async s => (await u<DockerStat[]>('ServerService', 'DockerStats', s)) ?? [],
      dockerInspect: async (s, n) => { const r = await u<DockerInspect>('ServerService', 'DockerInspect', s, n); return {...r, env: r.env ?? [], mounts: r.mounts ?? [], ports: r.ports ?? [], networks: r.networks ?? [], labels: r.labels ?? {}} },
      dockerObjects: async s => { const r = await u<DockerObjects>('ServerService', 'DockerObjects', s); return {images: r.images ?? [], volumes: r.volumes ?? [], networks: r.networks ?? []} },
      dockerRemove: (s, k, i) => u('ServerService', 'DockerRemove', s, k, i),
      logs: (s, k, n, o) => (o ? u('ServerService', 'LogsFor', s, k, n, o.lines, o.since) : u('ServerService', 'Logs', s, k, n)),
      stackAction: (s, p, a) => u('ServerService', 'StackAction', s, p, a),
      certificates: async s => { const r = await u<ServerCerts>('ServerService', 'Certificates', s); return {...r, certs: r.certs ?? []} },
      web: async s => { const r = await u<ServerWeb>('ServerService', 'Web', s); return {...r, sites: (r.sites ?? []).map(x => ({...x, names: x.names ?? [], listen: x.listen ?? [], targets: x.targets ?? []}))} },
      webTest: (s, k) => u('ServerService', 'WebTest', s, k),
      webReload: (s, k) => u('ServerService', 'WebReload', s, k),
      system: s => u<ServerSystem>('ServerService', 'System', s),
      setHostname: (s, n) => u('ServerService', 'SetHostname', s, n),
      setTimezone: (s, z) => u('ServerService', 'SetTimezone', s, z),
      swapCreate: (s, mb) => u('ServerService', 'SwapCreate', s, mb),
      swapRemove: s => u('ServerService', 'SwapRemove', s),
      updates: async s => { const r = await u<ServerUpdates>('ServerService', 'Updates', s); return {...r, packages: r.packages ?? []} },
      updatesRefresh: s => u('ServerService', 'UpdatesRefresh', s),
      cleanup: async s => { const r = await u<ServerCleanup>('ServerService', 'Cleanup', s); return {disks: r.disks ?? [], items: r.items ?? []} },
      cleanupRun: (s, id) => u('ServerService', 'CleanupRun', s, id),
      diskUsage: async (s, p) => (await u<ServerFolder[]>('ServerService', 'DiskUsage', s, p)) ?? [],
      users: async s => { const r = await u<ServerUsers>('ServerService', 'Users', s); return {users: r.users ?? [], sessions: r.sessions ?? [], me: r.me ?? ''} },
      addUser: (s, n, sh, sudo, k) => u('ServerService', 'AddUser', s, n, sh, sudo, k),
      deleteUser: (s, n, h) => u('ServerService', 'DeleteUser', s, n, h),
      userKeys: async (s, n) => (await u<UserKey[]>('ServerService', 'UserKeys', s, n)) ?? [],
      addUserKey: (s, n, k) => u('ServerService', 'AddUserKey', s, n, k),
      removeUserKey: (s, n, f) => u('ServerService', 'RemoveUserKey', s, n, f),
      security: async s => { const r = await u<ServerSecurity>('ServerService', 'Security', s); return {...r, fail2ban: {...r.fail2ban, jails: (r.fail2ban.jails ?? []).map(j => ({...j, ips: j.ips ?? []}))}, failed: {...r.failed, top: r.failed.top ?? []}} },
      unban: (s, j, ip) => u('ServerService', 'Unban', s, j, ip),
      schedule: async s => { const r = await u<ServerSchedule>('ServerService', 'Schedule', s); return {timers: r.timers ?? [], cron: r.cron ?? [], me: r.me ?? ''} },
      cronAdd: (s, o, sc, c) => u('ServerService', 'CronAdd', s, o, sc, c),
      cronUpdate: (s, o, os, oc, sc, c) => u('ServerService', 'CronUpdate', s, o, os, oc, sc, c),
      cronDelete: (s, o, sc, c) => u('ServerService', 'CronDelete', s, o, sc, c),
      backup: (s, p) => u('ServerService', 'Backup', s, p),
      fleetHost: h => u('ServerService', 'FleetHost', h),
      ports: async s => { const r = await u<ServerPorts>('ServerService', 'Ports', s); return {...r, ports: r.ports ?? [], rules: r.rules ?? []} },
      firewallAdd: (s, a, p, pr, f, force) => u('ServerService', 'FirewallAdd', s, a, p, pr, f, force),
      firewallDelete: (s, r, force) => u('ServerService', 'FirewallDelete', s, r.num, r.to, r.action, r.from, force),
      firewallEnable: s => u('ServerService', 'FirewallEnable', s),
      firewallDisable: s => u('ServerService', 'FirewallDisable', s),
      k8sDetect: async s => { const r = await u<K8sDetect>('ServerService', 'K8sDetect', s); return {...r, tried: r.tried ?? []} },
      k8sContexts: async s => { const r = await u<K8sContexts>('ServerService', 'K8sContexts', s); return {...r, contexts: r.contexts ?? []} },
      k8sCheck: (s, c) => u('ServerService', 'K8sCheck', s, c),
      k8sAccess: async (s, c, n) => { const r = await u<K8sAccess>('ServerService', 'K8sAccess', s, c, n); return {rights: r.rights ?? {}} },
      k8sNamespaces: async (s, c) => { const r = await u<K8sNamespaces>('ServerService', 'K8sNamespaces', s, c); return {names: r.names ?? []} },
      k8sOverview: async (s, c, n = '') => { const r = await u<K8sOverview>('ServerService', 'K8sOverviewIn', s, c, n); return {...r, namespaces: r.namespaces ?? [], attention: r.attention ?? []} },
      k8sNodes: async (s, c) => (await u<K8sNode[]>('ServerService', 'K8sNodes', s, c)) ?? [],
      k8sPods: async (s, c, n) => ((await u<K8sPod[]>('ServerService', 'K8sPods', s, c, n)) ?? []).map(p => ({...p, containers: p.containers ?? []})),
      k8sPod: async (s, c, n, p) => { const r = await u<K8sPodDetail>('ServerService', 'K8sPod', s, c, n, p); return {...r, pod: {...r.pod, containers: r.pod.containers ?? []}, labels: r.labels ?? [], conditions: r.conditions ?? [], volumes: r.volumes ?? [], events: r.events ?? []} },
      k8sWorkloads: async (s, c, n) => { const r = await u<K8sWorkloads>('ServerService', 'K8sWorkloads', s, c, n); return {items: r.items ?? []} },
      k8sDeployment: async (s, c, n, d) => { const r = await u<K8sDeployment>('ServerService', 'K8sDeployment', s, c, n, d); return {...r, revisions: r.revisions ?? []} },
      k8sNetwork: async (s, c, n) => { const r = await u<K8sNetwork>('ServerService', 'K8sNetwork', s, c, n); return {services: r.services ?? [], ingresses: r.ingresses ?? []} },
      k8sConfig: async (s, c, n) => {
        const r = await u<K8sConfig>('ServerService', 'K8sConfig', s, c, n)
        const item = (x: K8sConfigItem) => ({...x, keys: x.keys ?? [], used_by: x.used_by ?? []})
        return {configmaps: (r.configmaps ?? []).map(item), secrets: (r.secrets ?? []).map(item), volumes: (r.volumes ?? []).map(v => ({...v, used_by: v.used_by ?? []}))}
      },
      k8sConfigMap: async (s, c, n, m) => (await u<Record<string, string>>('ServerService', 'K8sConfigMap', s, c, n, m)) ?? {},
      k8sEvents: async (s, c, n) => (await u<K8sEvent[]>('ServerService', 'K8sEvents', s, c, n)) ?? [],
      k8sLogs: (s, c, n, p, o) => u('ServerService', 'K8sLogs', s, c, n, p, o),
      k8sLogsMulti: async (s, c, n, k, m, o) => { const r = await u<K8sMultiLogs>('ServerService', 'K8sLogsMulti', s, c, n, k, m, o); return {...r, failed: r.failed ?? []} },
      k8sYAML: (s, c, k, n, m) => u('ServerService', 'K8sYAML', s, c, k, n, m),
      k8sCounts: async (s, c, n) => (await u<Record<string, number>>('ServerService', 'K8sCounts', s, c, n)) ?? {},
      k8sDebugStart: (s, c, n, p, i, t) => u('ServerService', 'K8sDebugStart', s, c, n, p, i, t),
      k8sSecurity: async (s, c, n) => { const r = await u<K8sSecurity>('ServerService', 'K8sSecurity', s, c, n); return {...r, findings: r.findings ?? []} },
      k8sHelmReleases: async (s, c, n) => (await u<K8sHelmRelease[]>('ServerService', 'K8sHelmReleases', s, c, n)) ?? [],
      k8sHelmRelease: async (s, c, n, m, r) => { const d = await u<K8sHelmDetail>('ServerService', 'K8sHelmRelease', s, c, n, m, r); return {...d, history: d.history ?? []} },
      k8sHelmValues: (s, c, n, m, r) => u('ServerService', 'K8sHelmValues', s, c, n, m, r),
      podFilesHome: hostId => u('ServerService', 'PodFilesHome', hostId),
      podFilesList: async (hostId, dir) => (await u<RemoteEntry[]>('ServerService', 'PodFilesList', hostId, dir)) ?? [],
      podFilesFetch: (hostId, remote, localDir) => u('ServerService', 'PodFilesFetch', hostId, remote, localDir),
      k8sResources: async (s, c) => (await u<K8sAPIResource[]>('ServerService', 'K8sResources', s, c)) ?? [],
      k8sObjects: async (s, c, n, r) => { const l = await u<K8sObjList>('ServerService', 'K8sObjects', s, c, n, r); return {...l, columns: l.columns ?? [], rows: (l.rows ?? []).map(x => ({...x, cells: x.cells ?? []}))} },
      k8sObjectYAML: (s, c, n, r, m) => u('ServerService', 'K8sObjectYAML', s, c, n, r, m),
      k8sDrainPlan: async (s, c, n, o) => { const r = await u<K8sDrainPlan>('ServerService', 'K8sDrainPlan', s, c, n, o); return {...r, emptydir: r.emptydir ?? [], bare: r.bare ?? [], pdb: r.pdb ?? []} },
      k8sScale: (s, c, n, d, r) => u('ServerService', 'K8sScale', s, c, n, d, r),
      k8sRestart: (s, c, n, k, d) => u('ServerService', 'K8sRestart', s, c, n, k, d),
      k8sRolloutPause: (s, c, n, d, p) => u('ServerService', 'K8sRolloutPause', s, c, n, d, p),
      k8sRollback: (s, c, n, d, r) => u('ServerService', 'K8sRollback', s, c, n, d, r),
      k8sDeletePod: (s, c, n, p) => u('ServerService', 'K8sDeletePod', s, c, n, p),
      k8sDeleteJob: (s, c, n, j) => u('ServerService', 'K8sDeleteJob', s, c, n, j),
      k8sCronJob: (s, c, n, j, a) => u('ServerService', 'K8sCronJob', s, c, n, j, a),
      k8sCordon: (s, c, n, on) => u('ServerService', 'K8sCordon', s, c, n, on),
      k8sDrain: (s, c, n, o) => u('ServerService', 'K8sDrain', s, c, n, o),
      k8sShellCommand: (s, c, n, p, k) => u('ServerService', 'K8sShellCommand', s, c, n, p, k),
      k8sShellSession: (s, c, n, p, k) => u('ServerService', 'K8sShellSession', s, c, n, p, k),
      k8sWorkloadSpec: async (s, c, kind, n, name) => {
        const r = await u<K8sWorkloadSpec>('ServerService', 'K8sWorkloadSpec', s, c, kind, n, name)
        return {...r, containers: (r.containers ?? []).map(x => ({...x, command: x.command ?? [], args: x.args ?? [], ports: x.ports ?? [], env: x.env ?? [], env_from: x.env_from ?? [], requests: x.requests ?? {}, limits: x.limits ?? {}, mounts: x.mounts ?? []}))}
      },
      k8sEditable: (s, c, kind, n, name) => u('ServerService', 'K8sEditable', s, c, kind, n, name),
      k8sSaveYAML: (s, c, kind, n, name) => u('ServerService', 'K8sSaveYAML', s, c, kind, n, name),
      k8sEditPreview: async (s, c, kind, n, name, y) => { const r = await u<K8sEditPreview>('ServerService', 'K8sEditPreview', s, c, kind, n, name, y); return {...r, diff: r.diff ?? []} },
      k8sEditApply: (s, c, kind, n, name, y) => u('ServerService', 'K8sEditApply', s, c, kind, n, name, y),
      k8sSetContainer: (s, c, kind, n, name, e) => u('ServerService', 'K8sSetContainer', s, c, kind, n, name, e),
      k8sAddSidecar: (s, c, kind, n, name, sc) => u('ServerService', 'K8sAddSidecar', s, c, kind, n, name, sc),
      k8sPortForwardStart: (s, c, n, v, l, r) => u('ServerService', 'K8sPortForwardStart', s, c, n, v, l, r),
      k8sPortForwardStop: (s, i) => u('ServerService', 'K8sPortForwardStop', s, i),
      k8sPortForwards: async s => (await u<K8sForward[]>('ServerService', 'K8sPortForwards', s)) ?? [],
      k8sSecretKey: (s, c, n, m, k) => u('ServerService', 'K8sSecretKey', s, c, n, m, k),
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
    clusters: {
      list: async () => (await u<Cluster[]>('ClusterService', 'List')) ?? [],
      preview: async (p, x) => { const r = await u<ClusterPreview>('ClusterService', 'Preview', p, x); return {contexts: r.contexts ?? [], problems: r.problems ?? {}} },
      describe: async id => { const r = await u<ClusterPreview>('ClusterService', 'Describe', id); return {contexts: r.contexts ?? [], problems: r.problems ?? {}} },
      add: c => u('ClusterService', 'Add', c),
      update: c => u('ClusterService', 'Update', c),
      replaceKubeconfig: (id, p, x) => u('ClusterService', 'ReplaceKubeconfig', id, p, x),
      approveExec: (id, cmd) => u('ClusterService', 'ApproveExec', id, cmd),
      revokeExec: (id, cmd) => u('ClusterService', 'RevokeExec', id, cmd),
      remove: id => u('ClusterService', 'Delete', id),
      open: id => u('ClusterService', 'Open', id),
    },
    system: {
      setMenuShortcuts: s => call('app', 'SystemService', 'SetMenuShortcuts', s),
      pickFile: t => call('app', 'SystemService', 'PickFile', t),
      pickDirectory: t => call('app', 'SystemService', 'PickDirectory', t),
      saveFile: (t, n) => call('app', 'SystemService', 'SaveFile', t, n),
      saveText: (t, n, x) => call('app', 'SystemService', 'SaveText', t, n, x),
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

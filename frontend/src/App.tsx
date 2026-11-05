import {useCallback, useEffect, useRef, useState} from 'react'
import {useBackend} from './lib/backend'
import {isMac} from './lib/shortcuts/shortcuts'
import {UnlockScreen} from './lib/vault/UnlockScreen'
import {KeychainPage, KeyMode} from './lib/vault/KeychainPage'
import {KeyPanel} from './lib/vault/KeyPanel'
import {SerialPanel, portName} from './lib/hosts/SerialPanel'
import {HostList, QuickConnect} from './lib/hosts/HostList'
import {groupAndBelow, groupContents} from './lib/hosts/tree'
import {setLanguageSetting} from './lib/i18n/store'
import {useT, useTn} from './lib/i18n'
import {GroupForm, HostForm} from './lib/hosts/HostForm'
import {Workspace} from './lib/terminal/Workspace'
import {ForwardPanel} from './lib/panels/ForwardPanel'
import {AddMenu} from './lib/hosts/AddMenu'
import {KnownHostsPage} from './lib/panels/KnownHostsPage'
import {LogsPage} from './lib/panels/LogsPage'
import {SnippetsPage} from './lib/snippets/SnippetsPage'
import {SnippetForm} from './lib/snippets/SnippetForm'
import {FillVariables} from './lib/snippets/FillVariables'
import {RunOnHostsPanel} from './lib/snippets/RunOnHostsPanel'
import {fillVariables, parseVariables} from './lib/snippets/variables'
import {toTerminalInput, writeToPane} from './lib/terminal/panes'
import {PortForwardsPage} from './lib/panels/PortForwardsPage'
import {AboutPanel} from './lib/panels/AboutPanel'
import {SupportPanel} from './lib/panels/SupportPanel'
import {DataPanel} from './lib/panels/DataPanel'
import {SettingsWindow} from './lib/settings/SettingsWindow'
import {useAppearance} from './lib/settings/appearance'
import {CLOSE_HOST_TAB} from './lib/sftp/SftpBrowser'
import {AI_NEXT_MODEL} from './lib/assistant/AssistantTab'
import {SUGGEST_POPUP} from './lib/terminal/suggester'
import {SIDE_DEFAULT, SIDE_MAX, SIDE_MIN, SidebarTab, TerminalSidebar} from './lib/terminal/Sidebar'
import {DEFAULT_SETTINGS} from './lib/settings/defaults'
import {Nav, Page} from './lib/shell/Nav'
import {FleetPage} from './lib/server/FleetPage'
import {ClustersPage} from './lib/server/ClustersPage'
import {ClusterAddForm, ClusterEditForm} from './lib/server/ClusterForm'
import {TopBar} from './lib/shell/TopBar'
import {IconPlus, IconSerial, IconTerminal} from './lib/ui/Icons'
import {recordRecent} from './lib/hosts/recent'
import type {Protocol} from './lib/terminal/ProtocolScreen'
import {closePane, connectBlankTab, graftTab, moveTab, movePane, popPane, duplicateOf, hasPane, isTerminalTab, newBlankTab, leaves, MAX_TAB_TITLE, newPodShellTab, newShellTab, newTab, pinnedSftpTab, SETTINGS_TAB_ID, settingsTab, SFTP_TAB_ID, splitPane, Tab, toggleBroadcast, toggleZoom, uniqueTitle} from './lib/layout'
import {capture, effective, formatCombo, isChord, keyOf, matches, sameStep, stepMatches, stepsOf} from './lib/shortcuts/shortcuts'
import {errMsg} from './lib/util'
import {useToast} from './lib/ui/Toast'
import {useConfirm} from './lib/ui/Confirm'
import {DragBar} from './lib/ui/DragBar'
import type {DropSide, Leaf} from './lib/layout'
import type {Cluster, ForwardType, Group, PortForward, Host, Identity, Settings, Snippet, SSHLink, VaultStatus} from './lib/types'

type Dialog =
  | {k: 'host'; host?: Host; prefill?: QuickConnect}
  | {k: 'group'; group?: Group}
  | {k: 'forward'; forward?: PortForward; hostId?: string}
  | {k: 'snippet'; snippet?: Snippet}
  | {k: 'cluster'; cluster?: Cluster; replace?: boolean}
  | {k: 'key'; mode: KeyMode; identity?: Identity}
  | {k: 'serial'}
  | {k: 'data'}
  | {k: 'about'; check?: boolean}
  | {k: 'support'}
  | null


/** The tab to show when the active one closes: the last terminal that is left (the pinned SFTP tab does not count), else Vaults. */
const fallbackTab = (rest: Tab[]) => rest.filter(t => !t.pinned).at(-1)?.id ?? 'home'

export default function App({initial}: {/** an app window opened for this place (a host id, "local"...): its first tab connects to it */ initial?: {host_id: string; title: string}} = {}) {
  const be = useBackend()
  const toast = useToast()
  const confirm = useConfirm()
  const t = useT()
  const tn = useTn()
  const [vault, setVault] = useState<VaultStatus | null>(null)
  const [settings, setSettings] = useState<Settings>(DEFAULT_SETTINGS)
  const saving = useRef<Promise<unknown>>(Promise.resolve())
  const [groups, setGroups] = useState<Group[]>([])
  const [hosts, setHosts] = useState<Host[]>([])
  const [identities, setIdentities] = useState<Identity[]>([])
  const [snippets, setSnippets] = useState<Snippet[]>([])
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [lastTerm, setLastTerm] = useState('') // most recently focused terminal tab
  // the SFTP tab is always there, right after Vaults
  const [tabs, setTabs] = useState<Tab[]>(() => [pinnedSftpTab()])
  const [active, setActive] = useState('home') // 'home' or a tab id
  const [page, setPage] = useState<Page>('hosts')
  const [openGroupId, setOpenGroupId] = useState('') // the group open on the hosts page: new hosts and groups start in it
  const [hostsReset, setHostsReset] = useState(0) // bumped when Hosts is pressed while already on it
  const [settingsOpen, setSettingsOpen] = useState(false)
  const beforeSettings = useRef('home') // the tab that was in front when the Settings tab was shown: the shortcut goes back to it
  // the panel on the right of a terminal (hosts, snippets, history, themes): open or closed is remembered
  const [sidebarOpen, setSidebarOpen] = useState(() => { try { return localStorage.getItem('vutty.sidebar') !== 'closed' } catch { return true } })
  const [sidebarTab, setSidebarTabState] = useState<SidebarTab>(() => {
    try { const t = localStorage.getItem('vutty.sidebar.tab'); if (t === 'hosts' || t === 'snippets' || t === 'history' || t === 'files' || t === 'themes' || t === 'assistant') return t } catch { /* default */ }
    return 'hosts'
  })
  const [sideWidth, setSideWidthState] = useState<number>(() => { try { const n = Number(localStorage.getItem('vutty.sidebar.width')); return n >= SIDE_MIN && n <= SIDE_MAX ? n : SIDE_DEFAULT } catch { return SIDE_DEFAULT } })
  const setSideWidth = (w: number) => { setSideWidthState(w); try { localStorage.setItem('vutty.sidebar.width', String(w)) } catch { /* not remembered */ } }
  const setSidebarTab = (t: SidebarTab) => { setSidebarTabState(t); try { localStorage.setItem('vutty.sidebar.tab', t) } catch { /* not remembered */ } }
  const toggleSidebar = () => setSidebarOpen(o => { try { localStorage.setItem('vutty.sidebar', o ? 'closed' : 'open') } catch { /* not remembered */ } return !o })
  const [dialog, setDialog] = useState<Dialog | null>(null)
  const [fill, setFill] = useState<{snippet: Snippet; execute: boolean} | null>(null)
  const [runHosts, setRunHosts] = useState<{snippet?: Snippet} | null>(null)
  const [draft, setDraft] = useState('')
  const [fwDraft, setFwDraft] = useState<{label: string; type: ForwardType}>({label: '', type: 'local'})
  const [fwTick, setFwTick] = useState(0) // bumped when the forwarding panel saved something
  const appearance = useAppearance(settings.theme)

  const refreshVault = useCallback(async () => setVault(await be.vault.status()), [be])
  const refreshData = useCallback(async () => {
    const [g, h, i, sn, cl] = await Promise.all([be.hosts.listGroups(), be.hosts.listHosts({}), be.vault.listIdentities(), be.snippets.list(), be.clusters.list().catch(() => [] as Cluster[])])
    setClusters(cl ?? [])
    setGroups(g ?? [])
    setHosts(h ?? [])
    setIdentities(i ?? [])
    setSnippets(sn ?? [])
  }, [be])

  // an app window (Duplicate in new window) opens the place it was made for, once the vault is open
  const openedInitial = useRef(false)
  useEffect(() => {
    if (!initial || openedInitial.current || !vault?.unlocked) return
    const device = initial.host_id === 'local' || /^(serial|telnet):/.test(initial.host_id)
    if (!device && !hosts.some(h => h.id === initial.host_id)) return // the hosts are not loaded yet
    openedInitial.current = true
    const t = newTab(initial.host_id, initial.title)
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, initial.title)}])
    setActive(t.id)
  }, [initial, vault?.unlocked, hosts])

  // an ssh:// link: the system hands it to the app, the app asks before it opens anything (see sshLinkHandler)
  const sshLinkHandler = useRef<(l: SSHLink) => void>(() => {})
  const takeLink = useCallback(() => { void be.system.takePendingSSHLink().then(l => { if (l?.hostname) sshLinkHandler.current(l) }).catch(() => {}) }, [be])
  useEffect(() => { if (vault?.unlocked) takeLink() }, [vault?.unlocked, takeLink]) // one that came while the vault was locked
  const unlockedRef = useRef(false)
  unlockedRef.current = !!vault?.unlocked
  useEffect(() => be.on('app:open-ssh', () => { if (unlockedRef.current) takeLink() }), [be, takeLink])

  const refreshDataRef = useRef<() => Promise<void> | void>(() => {})
  refreshDataRef.current = () => { if (vault?.unlocked) return refreshData() }
  useEffect(() => {
    void refreshVault()
    void be.settings.get().then(setSettings).catch(() => {})
    // the vault auto-locked (idle): drop back to the unlock screen
    const offLocked = be.on('vault:locked', () => { void refreshVault() })
    // another window of this app (Duplicate in new window) changed something here: pick it up
    const offBridge = be.on('bridge:changed', (service: string) => {
      if (service === 'SettingsService') void be.settings.get().then(setSettings).catch(() => {})
      else void refreshVault().then(() => refreshDataRef.current())
    })
    // the first connection to a host revealed its server type (ubuntu, debian…): the icon changes
    // a sync brought newer preferences (theme, font, shortcuts, language…)
    const offSettings = be.on('settings:changed', () => { void be.settings.get().then(setSettings).catch(() => {}) })
    const offHosts = be.on('hosts:changed', () => { void refreshDataRef.current() })
    const offMenu = be.on('menu:action', (action: string) => menuHandler.current(action))
    return () => { offLocked(); offBridge(); offSettings(); offHosts(); offMenu() }
  }, [be, refreshVault])

  useEffect(() => {
    document.documentElement.dataset.theme = appearance
    try { localStorage.setItem('vutty.appearance', appearance) } catch { /* not remembered */ }
  }, [appearance])

  // Configurable shortcuts (Settings → Shortcuts). The handlers are re-assigned on every render once the
  // vault is unlocked, so the listener below never sees stale state.
  const act = useRef<Record<string, () => void>>({})
  // what the application menu bar asks for (see app/menu.go); replaced on every render so it never sees stale state
  const menuHandler = useRef<(action: string) => void>(() => {})
  const bindings = effective(settings.shortcuts)
  const bindingsRef = useRef(bindings)
  bindingsRef.current = bindings
  // in full screen macOS hides the window buttons, so the room kept for them on the title bar goes (CSS: data-fullscreen)
  useEffect(() => {
    const check = () => { void be.system.isFullscreen().then(f => { document.documentElement.dataset.fullscreen = f ? 'true' : 'false' }).catch(() => {}) }
    check()
    let t = 0
    const later = () => { window.clearTimeout(t); t = window.setTimeout(check, 150) }
    window.addEventListener('resize', later)
    return () => { window.clearTimeout(t); window.removeEventListener('resize', later) }
  }, [be])
  // the native menu shows the keys the page uses (macOS): tell it now and whenever they change
  const bindingsKey = JSON.stringify(bindings)
  useEffect(() => { void be.system.setMenuShortcuts(JSON.parse(bindingsKey)).catch(() => {}) }, [be, bindingsKey])
  // Two-step shortcuts ("Mod+K M"): the first step arms them, the second one runs the action. This listens before the
  // terminal does (capture phase), so neither step reaches the shell; a wrong key, Esc or a pause of 1.5 s cancels.
  const [chordHint, setChordHint] = useState('')
  useEffect(() => {
    let armed: {first: string; timer: number} | null = null
    const disarm = () => { if (armed) window.clearTimeout(armed.timer); armed = null; setChordHint('') }
    const onKey = (e: KeyboardEvent) => {
      if (capture.active) return
      const chords = Object.entries(bindingsRef.current).filter(([, c]) => isChord(c))
      if (armed) {
        if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); disarm(); return }
        if (!keyOf(e)) return // only a modifier so far: keep waiting
        e.preventDefault()
        e.stopPropagation()
        const first = armed.first
        const hit = chords.find(([, c]) => sameStep(stepsOf(c)[0], first) && stepMatches(e, stepsOf(c)[1]))
        disarm()
        if (hit) act.current[hit[0]]?.()
        return
      }
      const start = chords.find(([, c]) => stepMatches(e, stepsOf(c)[0]))
      if (!start) return
      e.preventDefault()
      e.stopPropagation()
      const first = stepsOf(start[1])[0]
      armed = {first, timer: window.setTimeout(disarm, 1500)}
      setChordHint(formatCombo(first))
    }
    window.addEventListener('keydown', onKey, true)
    return () => { disarm(); window.removeEventListener('keydown', onKey, true) }
  }, [])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (capture.active) return // the editor is recording a new shortcut
      for (const [id, combo] of Object.entries(bindingsRef.current)) {
        if (id === 'terminal_search' || id === 'terminal_copy' || id === 'terminal_paste') continue // handled by the focused terminal
        if (matches(e, combo)) {
          const fn = act.current[id]
          if (fn) { e.preventDefault(); fn() }
          return
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // interface zoom and reduced motion
  useEffect(() => { document.documentElement.style.setProperty('zoom', String((settings.zoom || 100) / 100)) }, [settings.zoom])
  useEffect(() => { setLanguageSetting(settings.language) }, [settings.language])
  useEffect(() => { document.documentElement.dataset.motion = settings.reduce_motion ? 'reduced' : 'full' }, [settings.reduce_motion])
  useEffect(() => { document.documentElement.dataset.os = isMac() ? 'mac' : 'other' }, []) // the window buttons of macOS sit in a bar taller than the top bar: see base.css

  // a side panel belongs to the page it was opened on: leaving the home view closes it
  const leftHome = active !== 'home' && tabs.some(t => t.id === active)
  useEffect(() => { if (leftHome) setDialog(null) }, [leftHome])
  useEffect(() => { if (vault?.unlocked) void refreshData() }, [vault?.unlocked, refreshData])

  // a newer release on GitHub: looked for when the app opens, at most once a day, unless switched off in Settings
  const [newVersion, setNewVersion] = useState('')
  useEffect(() => {
    if (!vault?.unlocked) return
    const KEY = 'vutty.update.checked'
    try { if (Date.now() - Number(localStorage.getItem(KEY) || 0) < 24 * 3600 * 1000) return } catch { /* check anyway */ }
    // the saved setting is read here, not taken from state: the state holds the defaults until the real ones arrive
    void be.settings.get().then(s => (s.auto_update_check ? be.system.checkUpdate() : null)).then(u => {
      if (!u) return
      try { localStorage.setItem(KEY, String(Date.now())) } catch { /* checked again next time */ }
      if (u.available) setNewVersion(u.latest)
    }).catch(() => {}) // offline or rate limited: stay quiet, it is only a courtesy
  }, [be, vault?.unlocked])

  act.current = {}
  // locked or loading: only Help → About and Check for Updates work (they need nothing from the vault)
  menuHandler.current = (action: string) => {
    if (action === 'about') setDialog({k: 'about'})
    else if (action === 'check_update') setDialog({k: 'about', check: true})
    else if (action === 'support') setDialog({k: 'support'})
  }
  const aboutOnly = dialog?.k === 'about' ? <AboutPanel onClose={() => setDialog(null)} checkNow={dialog.check} />
    : dialog?.k === 'support' ? <SupportPanel onClose={() => setDialog(null)} /> : null
  if (!vault) return <div className="gate"><DragBar /><div className="muted">{t('Loading…')}</div>{aboutOnly}</div>
  if (!vault.unlocked) {
    return <><UnlockScreen status={vault} autoBiometric={settings.biometric_on_open} onDone={() => { void refreshVault() }} />{aboutOnly}</>
  }

  // a second tab on the same host is numbered ("web", "web (2)"…); double-click a tab to give it any name
  const connect = (h: Host) => {
    recordRecent(h.id)
    const t = newTab(h.id, '')
    // a host that also has Telnet first asks which protocol to use
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, h.label), choose: !!h.telnet_enabled || settings.ask_protocol}])
    setActive(t.id)
  }
  // what an ssh:// link does: never connects by itself. A saved host it names is opened after a yes; any other address only
  // opens the New host form, filled in, and nothing happens until Connect is pressed (the host key is asked about as always).
  sshLinkHandler.current = async (l: SSHLink) => {
    const all = (await be.hosts.listHosts({})) ?? []
    const same = all.filter(h => h.hostname.toLowerCase() === l.hostname.toLowerCase() && h.port === l.port)
    const match = l.username ? same.find(h => h.username === l.username) : same[0]
    const target = `${l.username ? l.username + '@' : ''}${l.hostname}${l.port !== 22 ? ':' + l.port : ''}`
    if (match) {
      if (await confirm(t('A link wants to open {name} ({target}). Connect?', {name: match.label, target}), {confirmLabel: t('Connect')})) connect(match)
    } else if (await confirm(t('A link points to {target}, which is not one of your hosts. Add it as a new host? Nothing connects until you press Connect.', {target}), {confirmLabel: t('Add host')})) {
      goHome('hosts')
      setDialog({k: 'host', prefill: {username: l.username, hostname: l.hostname, port: l.port}})
    }
  }
  // a host card dropped on a group card
  const moveHost = (h: Host, groupId: string) => {
    be.hosts.saveHost({...h, group_id: groupId})
      .then(() => { void refreshData(); toast.info(groupId ? t('Moved {name} to {group}', {name: h.label, group: groups.find(g => g.id === groupId)?.name ?? t('the group')}) : t('Moved {name} out of its group', {name: h.label})) })
      .catch(e => toast.error(errMsg(e)))
  }
  // "Access host" in a server Panel: an SSH terminal on the same host, straight away, in a tab of its own
  const accessHost = (h: Host) => {
    recordRecent(h.id)
    const t = newTab(h.id, '')
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, h.label)}])
    setActive(t.id)
  }
  // "Shell" on a pod in the Kubernetes tab: an SSH terminal on the same host in a tab of its own, which types the kubectl exec line once connected
  const shellInPod = (h: Host, command: string, title: string) => {
    recordRecent(h.id)
    const t = newShellTab(h.id, '', command)
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, title)}])
    setActive(t.id)
  }
  // "Open" on a cluster: its Kubernetes tab, in a tab of its own (one tab for each cluster: opening it again shows that tab)
  const openCluster = (c: Cluster) => {
    const have = tabs.find(x => x.kind === 'cluster' && leaves(x.root)[0]?.hostId === 'kube:' + c.id)
    if (have) { setActive(have.id); return }
    const t = newTab('kube:' + c.id, '')
    setTabs(ts => [...ts, {...t, kind: 'cluster' as const, title: uniqueTitle(ts, c.label)}])
    setActive(t.id)
  }
  const delCluster = async (c: Cluster) => {
    if (!(await confirm(t('Delete the cluster "{name}"? Its kubeconfig is removed from the vault.', {name: c.label}), {confirmLabel: t('Delete'), danger: true}))) return
    try {
      await be.clusters.remove(c.id)
      const open = tabs.find(x => x.kind === 'cluster' && leaves(x.root)[0]?.hostId === 'kube:' + c.id)
      if (open) setTabs(ts => ts.filter(x => x.id !== open.id))
      await refreshData()
    } catch (e) { toast.error(errMsg(e)) }
  }
  // "Shell" on a pod of a cluster opened with a kubeconfig: a terminal tab of its own that talks to the cluster's API (no host, no kubectl)
  const openPodShell = (hostId: string, title: string, prod: boolean) => {
    const t = newPodShellTab(hostId, '', title, prod)
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, title)}])
    setActive(t.id)
  }
  // "Open panel" in the Fleet page: the server dashboard of that host in a tab of its own
  const openPanel = (h: Host) => {
    recordRecent(h.id)
    const t = newTab(h.id, '')
    setTabs(ts => [...ts, {...t, kind: 'server' as const, title: uniqueTitle(ts, h.label + ' (Panel)')}])
    setActive(t.id)
  }
  // the answer of a tab that asked which protocol to use: the same tab goes on with it
  const chooseProtocol = (tabId: string, proto: Protocol) => setTabs(ts => ts.map(t => {
    if (t.id !== tabId || t.root.type !== 'leaf') return t
    const h = hosts.find(x => x.id === (t.root as Leaf).hostId)
    if (proto === 'panel') return {...t, choose: false, kind: 'server' as const, title: uniqueTitle(ts.filter(x => x.id !== tabId), (h?.label ?? t.title) + ' (Panel)')}
    const title = proto === 'telnet' && h ? uniqueTitle(ts.filter(x => x.id !== tabId), h.label + ' (Telnet)') : t.title
    return {...t, choose: false, title, root: proto === 'telnet' ? {...t.root, hostId: 'telnet:' + t.root.hostId} : t.root}
  }))
  // the + tab: a blank tab with a search box and the recent connections
  const openBlankTab = () => {
    const t = newBlankTab('')
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, 'New Tab')}])
    setActive(t.id)
  }
  const openFromBlank = (tabId: string, h: Host) => {
    recordRecent(h.id)
    setTabs(ts => ts.map(t => (t.id === tabId ? {...connectBlankTab(t, h.id, uniqueTitle(ts.filter(x => x.id !== tabId), h.label), !!h.telnet_enabled || settings.ask_protocol)} : t)))
  }
  const localFromBlank = (tabId: string) => setTabs(ts => ts.map(t => (t.id === tabId ? connectBlankTab(t, 'local', uniqueTitle(ts.filter(x => x.id !== tabId), 'Local'), false) : t)))
  // a tab dropped on a side of a pane: its panes join that tab as a split (new sessions to the same places)
  const dropTab = (targetId: string, paneId: string, sourceId: string, side: DropSide) => {
    const target = tabs.find(t => t.id === targetId), source = tabs.find(t => t.id === sourceId)
    if (!target || !source) return
    const joined = graftTab(target, paneId, source, side)
    if (joined === target) return
    setTabs(ts => ts.filter(t => t.id !== sourceId).map(t => (t.id === targetId ? joined : t)))
    setActive(targetId)
    if (lastTerm === sourceId) setLastTerm(targetId)
  }
  // a pane dragged by its header onto a side of another pane: it moves there (same pane, so its session goes on)
  const movePaneTo = (fromTabId: string, paneId: string, toTabId: string, toPaneId: string, side: DropSide) => {
    const next = movePane(tabs, fromTabId, paneId, toTabId, toPaneId, side)
    if (!next) return
    setTabs(next)
    setActive(toTabId)
    if (lastTerm === fromTabId && !next.some(t => t.id === fromTabId)) setLastTerm(toTabId)
  }
  // ...or onto another terminal tab of the bar: it joins that tab, next to the pane in focus there
  const paneToTab = (fromTabId: string, paneId: string, toTabId: string) => {
    const to = tabs.find(t => t.id === toTabId)
    if (!to) return
    const target = leaves(to.root).some(l => l.id === to.activePane) ? to.activePane : leaves(to.root)[0]?.id
    if (target) movePaneTo(fromTabId, paneId, toTabId, target, 'right')
  }
  // a pane out of its split into a tab of its own (its session goes with it)
  const popPaneOut = (tabId: string, paneId: string) => {
    const tab = tabs.find(t => t.id === tabId)
    const leaf = tab && leaves(tab.root).find(l => l.id === paneId)
    if (!tab || !leaf) return
    const host = hosts.find(h => h.id === (leaf.hostId.startsWith('telnet:') ? leaf.hostId.slice(7) : leaf.hostId))
    const name = leaf.hostId === 'local' ? 'Local' : host?.label ?? leaf.hostId
    const res = popPane(tab, paneId, uniqueTitle(tabs, name))
    if (!res) return
    setTabs(ts => { const i = ts.findIndex(t => t.id === tabId); return [...ts.slice(0, i), res.rest, res.popped, ...ts.slice(i + 1)] })
    setActive(res.popped.id)
  }
  const moveTabTo = (id: string, targetId: string, after: boolean) => setTabs(ts => moveTab(ts, id, targetId, after))
  const renameTab = (id: string, name: string) => {
    const title = name.trim().slice(0, MAX_TAB_TITLE)
    if (title) setTabs(ts => ts.map(t => (t.id === id && !t.pinned ? {...t, title} : t)))
  }
  // a terminal that is not an SSH host: this computer ("local") or a serial port ("serial:<device>@<baud>")
  const openDevice = (hostId: string, title: string) => {
    const t = newTab(hostId, '')
    setTabs(ts => [...ts, {...t, title: uniqueTitle(ts, title)}])
    setActive(t.id)
  }
  const closeTab = async (id: string) => {
    const closing = tabs.find(t => t.id === id)
    if (closing?.pinned) return // the SFTP tab stays
    if (settings.confirm_tab_close && closing && closing.kind !== 'settings' && !(await confirm(t('Close "{title}"? Its connection will end.', {title: closing.title}), {confirmLabel: t('Close tab'), danger: true}))) return
    setTabs(ts => ts.filter(t => t.id !== id))
    if (id === active) {
      const rest = tabs.filter(t => t.id !== id)
      setActive(fallbackTab(rest))
    }
  }
  const split = (dir: 'row' | 'col') => setTabs(ts => ts.map(t => (t.id === active ? splitPane(t, t.activePane, dir) : t)))
  // the right-click menu of a tab acts on that tab, whichever one is showing
  const splitTab = (id: string, dir: 'row' | 'col') => { setTabs(ts => ts.map(t => (t.id === id ? splitPane(t, t.activePane, dir) : t))); setActive(id) }
  const duplicateTab = (id: string) => {
    const src = tabs.find(t => t.id === id)
    const copy = src && duplicateOf(src, uniqueTitle(tabs, src.title))
    if (!src || !copy) return
    setTabs(ts => { const i = ts.findIndex(t => t.id === id); return [...ts.slice(0, i + 1), copy, ...ts.slice(i + 1)] })
    setActive(copy.id)
  }
  // a new native window with a new session to where the tab's focused pane is connected
  const duplicateWindow = (id: string) => {
    const src = tabs.find(t => t.id === id)
    const leaf = src && leaves(src.root).find(l => l.id === src.activePane)
    if (!src || !leaf) return
    be.system.openWindow(leaf.hostId, src.title).catch(() => toast.error(t('New windows are not available here.')))
  }
  const broadcastTab = (id: string) => setTabs(ts => ts.map(t => (t.id === id ? toggleBroadcast(t) : t)))
  const zoomPane = (id: string, pane: string) => setTabs(ts => ts.map(t => (t.id === id ? toggleZoom(t, pane) : t)))
  const focusPane = (tabId: string, paneId: string) => setTabs(ts => ts.map(t => (t.id === tabId && hasPane(t, paneId) ? {...t, activePane: paneId} : t)))
  // the × on a pane of a split tab: ask first like closing a tab, then end just that pane's session
  const closePaneAsk = async (tabId: string, paneId: string) => {
    if (settings.confirm_tab_close && !(await confirm(t('Close this pane? Its connection will end.'), {confirmLabel: t('Close pane'), danger: true}))) return
    paneExit(tabId, paneId)
  }
  const paneExit = (tabId: string, paneId: string) => {
    const remaining = tabs.flatMap(t => {
      if (t.id !== tabId) return [t]
      const n = closePane(t, paneId)
      return n ? [n] : []
    })
    setTabs(remaining)
    if (!remaining.some(t => t.id === tabId) && active === tabId) setActive(fallbackTab(remaining))
  }
  /** the production host the focused pane of a tab is on, when "be careful on production hosts" is on */
  const prodHostOf = (tab: {root: Parameters<typeof leaves>[0]; activePane: string}): Host | undefined => {
    if (!settings.confirm_prod) return undefined
    const id = leaves(tab.root).find(l => l.id === tab.activePane)?.hostId
    const h = hosts.find(x => x.id === id)
    return h?.env === 'prod' ? h : undefined
  }
  /** what is typed runs lines at once: on a production host that is asked about first (a paste with line breaks counts too) */
  const okOnProd = async (tab: Parameters<typeof prodHostOf>[0], text: string, run: boolean, what: string): Promise<boolean> => {
    const h = prodHostOf(tab)
    if (!h || !(run || /[\r\n]/.test(text.replace(/[\r\n]+$/, '')))) return true
    return confirm(run ? t('{what} on the PRODUCTION host {host}? It runs at once.', {what, host: h.label}) : t('{what} on the PRODUCTION host {host}? Its lines run as soon as they are typed.', {what, host: h.label}), {confirmLabel: run ? t('Run') : t('Paste'), danger: true})
  }
  /** types text into the focused pane (the sidebar's snippets and history); `run` presses Enter too */
  const insertIntoTerminal = async (text: string, run: boolean) => {
    const tab = tabs.find(t => t.id === (tabs.some(x => x.id === active) ? active : lastTerm))
    if (!tab || !isTerminalTab(tab)) return toast.error(t('Open a terminal first.'))
    if (!(await okOnProd(tab, text, run, run ? t('Run this command') : t('Paste this text')))) return
    writeToPane(tab.activePane, toTerminalInput(text, run)) // a terminal that is still connecting takes nothing: no message for that
  }
  const sendSnippet = async (s: Snippet, script: string, execute: boolean) => {
    const tab = tabs.find(t => t.id === (tabs.some(x => x.id === active) ? active : lastTerm))
    if (!tab) return toast.error(t('Open a terminal first: the snippet is typed into the focused pane.'))
    if (isTerminalTab(tab) && !(await okOnProd(tab, script, execute, execute ? t('Run “{name}”', {name: s.label}) : t('Paste “{name}”', {name: s.label})))) return
    if (!writeToPane(tab.activePane, toTerminalInput(script, execute))) return // still connecting: nothing to say about it
    setActive(tab.id)
    toast.info(execute ? t('Ran “{name}”', {name: s.label}) : t('Pasted “{name}”', {name: s.label}))
  }
  // a snippet with blanks ({{name}}) is asked about first, then typed with the answers in it
  const runSnippet = (s: Snippet, execute: boolean) => {
    if (parseVariables(s.script).length > 0) return setFill({snippet: s, execute})
    sendSnippet(s, s.script, execute)
  }
  const delSnippet = async (s: Snippet) => {
    if (!(await confirm(t('Delete snippet "{name}"?', {name: s.label}), {confirmLabel: t('Delete'), danger: true}))) return
    try { await be.snippets.remove(s.id); await refreshData() } catch (e) { toast.error(errMsg(e)) }
  }
  /** Changing page discards an open panel (New host, New group, New snippet, forwards…). */
  const changePage = (p: Page) => {
    if (p !== page) { setPage(p); setDialog(null) }
  }
  const goHome = (p?: Page) => { setActive('home'); if (p) changePage(p) }

  const delHost = async (h: Host) => {
    if (!(await confirm(t('Delete host "{name}"?', {name: h.label}), {confirmLabel: t('Delete'), danger: true}))) return
    try { await be.hosts.deleteHost(h.id); await refreshData() } catch (e) { toast.error(errMsg(e)) }
  }
  const delGroup = async (g: Group) => {
    const inside = groupContents(groups, hosts, g.id)
    const nh = inside.hosts.length
    const hostsText = nh ? tn(nh, '{n} host', '{n} hosts') : ''
    const groupsText = inside.groups ? tn(inside.groups, '{n} group below it', '{n} groups below it') : ''
    const what = hostsText && groupsText ? t('{a} and {b}', {a: hostsText, b: groupsText}) : hostsText || groupsText
    const msg = what ? t('Delete group "{name}" with {what}? The hosts are deleted too, with their port forwards. This cannot be undone.', {name: g.name, what}) : t('Delete group "{name}"? It is empty.', {name: g.name})
    // the clusters in it are not deleted: they are taken out of the group first
    const ids = groupAndBelow(groups, g.id)
    const kept = clusters.filter(c => c.group_id && ids.has(c.group_id))
    const full = kept.length ? msg + ' ' + tn(kept.length, 'The {n} cluster in it is kept, without a group.', 'The {n} clusters in it are kept, without a group.') : msg
    if (!(await confirm(full, {confirmLabel: t('Delete'), danger: true}))) return
    try {
      for (const c of kept) await be.clusters.update({...c, group_id: undefined})
      await be.hosts.deleteGroup(g.id)
      await refreshData()
    } catch (e) { toast.error(errMsg(e)) }
  }
  // a cluster card dropped on a group card of the Kubernetes page
  const moveCluster = (c: Cluster, groupId: string) => {
    be.clusters.update({...c, group_id: groupId || undefined})
      .then(() => { void refreshData(); toast.info(groupId ? t('Moved {name} to {group}', {name: c.label, group: groups.find(g => g.id === groupId)?.name ?? t('the group')}) : t('Moved {name} out of its group', {name: c.label})) })
      .catch(e => toast.error(errMsg(e)))
  }

  const close = () => setDialog(null)
  const termActive = tabs.some(t => t.id === active) ? active : ''
  const onHome = active === 'home' || !termActive
  const activeTabObj = tabs.find(t => t.id === termActive)
  if (termActive && activeTabObj && isTerminalTab(activeTabObj) && lastTerm !== termActive) setLastTerm(termActive)

  const saveSettings = (patch: Partial<Settings>) => {
    // optimistic and serialised, so quick repeats (holding a zoom shortcut) build on each other
    const next = {...settings, ...patch}
    setSettings(next)
    saving.current = saving.current
      .then(() => be.settings.save(next))
      .then(setSettings)
      .catch(e => { toast.error(errMsg(e)); setSettings(settings) })
  }
  // Settings is a popup over the window, like in VS Code: the key toggles it, Esc, the backdrop and the × close it. Its button
  // "Open in a tab" moves it into a tab of the window (VS Code's "open as editor"); while that tab exists the gear, the menu and the
  // key show the tab instead, and the key again goes back to the tab that was in front.
  const settingsTabOpen = tabs.some(x => x.id === SETTINGS_TAB_ID)
  const openSettings = (toggle = false) => {
    if (!settingsTabOpen) return setSettingsOpen(o => (toggle ? !o : true))
    if (active === SETTINGS_TAB_ID) {
      if (toggle) setActive(tabs.some(x => x.id === beforeSettings.current) ? beforeSettings.current : 'home')
      return
    }
    beforeSettings.current = active
    setActive(SETTINGS_TAB_ID)
  }
  const settingsToTab = () => {
    beforeSettings.current = active
    setSettingsOpen(false)
    setTabs(ts => (ts.some(x => x.id === SETTINGS_TAB_ID) ? ts : [...ts, settingsTab()]))
    setActive(SETTINGS_TAB_ID)
  }
  const zoomBy = (delta: number) => saveSettings({zoom: Math.max(80, Math.min(160, settings.zoom + delta))})
  const cycle = (dir: 1 | -1) => {
    const ids = ['home', ...tabs.map(t => t.id)]
    const at = onHome ? 0 : Math.max(0, ids.indexOf(termActive))
    setActive(ids[(at + dir + ids.length) % ids.length])
  }
  const inTerminalTab = !!activeTabObj && isTerminalTab(activeTabObj)
  act.current = {
    open_settings: () => openSettings(true),
    new_connection: openBlankTab,
    new_host: () => { goHome('hosts'); setDialog({k: 'host'}) },
    new_local_terminal: () => openDevice('local', 'Local'),
    go_hosts: () => goHome('hosts'),
    go_keychain: () => goHome('keychain'),
    go_forwards: () => goHome('forwards'),
    go_snippets: () => goHome('snippets'),
    go_knownhosts: () => goHome('knownhosts'),
    go_logs: () => goHome('logs'),
    go_vaults: () => goHome(),
    open_sftp: () => setActive(SFTP_TAB_ID),
    lock_vault: () => { void be.vault.lock().then(refreshVault) },
    close_tab: () => {
      // on the SFTP screen the shortcut closes the host tab showing in its Remote pane (the SFTP tab itself stays)
      if (activeTabObj?.kind === 'sftp' && !onHome) window.dispatchEvent(new Event(CLOSE_HOST_TAB))
      else if (termActive) closeTab(termActive)
    },
    next_tab: () => cycle(1),
    prev_tab: () => cycle(-1),
    close_pane: () => { if (activeTabObj && inTerminalTab && !onHome) void closePaneAsk(activeTabObj.id, activeTabObj.activePane) },
    broadcast_input: () => { if (activeTabObj && inTerminalTab && !onHome) broadcastTab(activeTabObj.id) },
    ai_next_model: () => { window.dispatchEvent(new Event(AI_NEXT_MODEL)) },
    toggle_suggest: () => { const on = !settings.suggest; saveSettings({suggest: on}); toast.info(on ? t('Command suggestions on') : t('Command suggestions off')) },
    split_right: () => { if (inTerminalTab) split('row') },
    split_down: () => { if (inTerminalTab) split('col') },
    zoom_in: () => zoomBy(10),
    zoom_out: () => zoomBy(-10),
    zoom_reset: () => saveSettings({zoom: 100}),
  }

  menuHandler.current = (action: string) => {
    if (action === 'new_host') { goHome('hosts'); setDialog({k: 'host'}) }
    else if (action === 'new_group') { goHome('hosts'); setDialog({k: 'group'}) }
    else if (action === 'new_terminal') openDevice('local', 'Local')
    else if (action === 'serial') { goHome('hosts'); setDialog({k: 'serial'}) }
    else if (action === 'import') setDialog({k: 'data'})
    else if (action === 'about') setDialog({k: 'about'})
    else if (action === 'check_update') setDialog({k: 'about', check: true})
    else if (action === 'support') setDialog({k: 'support'})
    else if (action === 'open_settings') openSettings()
    else if (action === 'duplicate_tab') { if (activeTabObj && !onHome) duplicateTab(activeTabObj.id) }
    else if (action === 'suggest_popup') window.dispatchEvent(new Event(SUGGEST_POPUP))
    else if (action === 'page:sftp') setActive(SFTP_TAB_ID)
    else if (action.startsWith('page:')) goHome(action.slice('page:'.length) as Page)
    else act.current[action]?.()
  }

  return (
    <div className="shell">
      <TopBar tabs={tabs} active={onHome ? 'home' : termActive} onHome={() => goHome()} onSelect={setActive} onClose={closeTab}
        onPaneOut={popPaneOut} onPaneToTab={paneToTab}
        onNew={openBlankTab}
        onSplit={split} onSettings={() => openSettings()} onToggleMaximise={() => void be.system.toggleMaximise()} onRename={renameTab} onMoveTab={moveTabTo} onDuplicate={duplicateTab} onDuplicateWindow={duplicateWindow} onSplitTab={splitTab} snippets={snippets} hosts={hosts} onRunSnippet={runSnippet}
        sidebarOpen={sidebarOpen} onToggleSidebar={toggleSidebar} updateVersion={newVersion} onUpdate={() => setDialog({k: 'about', check: true})} />
      {chordHint && <div className="chord-hint" role="status"><kbd className="keycap">{chordHint}</kbd> was pressed. Waiting for the next key… (Esc to cancel)</div>}
      <div className="body">
        <div className="home" style={{display: onHome ? 'flex' : 'none'}}>
          <Nav page={page} onPage={p => { if (p === 'hosts' && page === 'hosts') { setHostsReset(n => n + 1); setDialog(null) } else changePage(p) }} shortcuts={bindings} onData={() => setDialog({k: 'data'})} settingsOpen={settingsOpen} onSettings={() => openSettings()} onLock={() => { void be.vault.lock().then(refreshVault) }} />
          <main className="main">
            {page === 'hosts' && (
              <HostList resetKey={hostsReset} onGroupChange={setOpenGroupId} groups={groups} hosts={hosts} onMoveHost={moveHost} draft={dialog?.k === 'host' && !dialog.host?.id ? draft : undefined}
                toolbar={<>
                  <span className="split-wrap">
                    <button className="split-btn" aria-label={t('New host')} onClick={() => setDialog({k: 'host'})}><IconPlus size={14} /> {t('New host')}</button>
                    <AddMenu onGroup={() => setDialog({k: 'group'})} onImport={() => setDialog({k: 'data'})} />
                  </span>
                  <button className="ghost-btn" aria-label={t('Terminal')} title={t('A terminal on this computer')} onClick={() => openDevice('local', 'Local')}><IconTerminal size={14} /> {t('Terminal')}</button>
                  <button className="ghost-btn" aria-label={t('Serial')} title={t('Connect to a serial port')} onClick={() => setDialog({k: 'serial'})}><IconSerial size={14} /> {t('Serial')}</button>
                </>}
                onConnect={connect} onEdit={h => setDialog({k: 'host', host: h})} onDelete={delHost}
                onForwards={h => { goHome('forwards'); setFwDraft({label: '', type: 'local'}); setDialog({k: 'forward', hostId: h.id}) }} onTelnet={h => openDevice(`telnet:${h.id}`, `${h.label} (Telnet)`)} onEditGroup={g => setDialog({k: 'group', group: g})}
                onDeleteGroup={delGroup} onQuickConnect={q => setDialog({k: 'host', prefill: q})} />
            )}
            {page === 'keychain' && (
              <KeychainPage identities={identities} draft={dialog?.k === 'key' && !dialog.identity ? draft : undefined}
                onNew={mode => { setDraft(''); setDialog({k: 'key', mode}) }} onEdit={i => setDialog({k: 'key', mode: i.auth_type, identity: i})} onChanged={() => void refreshData()} />
            )}
            {page === 'snippets' && (
              <SnippetsPage snippets={snippets} canRun={tabs.some(isTerminalTab)} onNew={() => setDialog({k: 'snippet'})}
                onRun={runSnippet} onRunOnHosts={s => setRunHosts({snippet: s})} onEdit={s => setDialog({k: 'snippet', snippet: s})} onDelete={delSnippet} />
            )}
            {page === 'knownhosts' && <KnownHostsPage />}
            {page === 'logs' && <LogsPage />}
            {page === 'fleet' && <FleetPage hosts={hosts} onPanel={openPanel} />}
            {page === 'clusters' && <ClustersPage clusters={clusters} groups={groups} onNew={() => setDialog({k: 'cluster'})} onOpen={openCluster}
              onEdit={c => setDialog({k: 'cluster', cluster: c})} onReplace={c => setDialog({k: 'cluster', cluster: c, replace: true})} onDelete={delCluster}
              onNewGroup={() => setDialog({k: 'group'})} onEditGroup={g => setDialog({k: 'group', group: g})} onDeleteGroup={delGroup} onMoveCluster={moveCluster} />}

            {page === 'forwards' && (
              <PortForwardsPage hosts={hosts} draft={dialog?.k === 'forward' && !dialog.forward ? fwDraft : undefined} reloadKey={fwTick}
                onNew={() => { setFwDraft({label: '', type: 'local'}); setDialog({k: 'forward'}) }} onEdit={f => setDialog({k: 'forward', forward: f})} />
            )}
          </main>
          {dialog?.k === 'host' && (
            <HostForm key={dialog.host?.id ?? dialog.prefill?.hostname ?? 'new'}
              host={dialog.host ?? (dialog.prefill ? ({id: '', label: '', hostname: dialog.prefill.hostname, port: dialog.prefill.port, username: dialog.prefill.username, keepalive_sec: 30} as Host) : undefined)}
              isNew={!dialog.host}
              groups={groups} defaultGroupId={openGroupId} hosts={hosts} identities={identities} onClose={close}
              onSaved={() => { close(); void refreshData() }}
              onConnect={h => { connect(h) }} onDraft={setDraft} />
          )}
          {dialog?.k === 'group' && <GroupForm group={dialog.group} groups={groups} defaultParentId={openGroupId} onClose={close} onSaved={() => { close(); void refreshData() }} />}
          {dialog?.k === 'forward' && (
            <ForwardPanel key={dialog.forward?.id ?? 'new-' + (dialog.hostId ?? '')} hosts={hosts} groups={groups} forward={dialog.forward} hostId={dialog.hostId}
              onClose={close} onChanged={() => setFwTick(n => n + 1)} onDraft={setFwDraft} />
          )}
          {dialog?.k === 'serial' && <SerialPanel onClose={close} onConnect={(path, baud) => { close(); openDevice(`serial:${path}@${baud}`, portName(path)) }} />}
          {dialog?.k === 'key' && <KeyPanel key={dialog.identity?.id ?? dialog.mode} mode={dialog.mode} identity={dialog.identity} onClose={close} onChanged={() => void refreshData()} onDraft={setDraft} />}
          {dialog?.k === 'cluster' && (dialog.cluster && !dialog.replace
            ? <ClusterEditForm key={dialog.cluster.id} cluster={dialog.cluster} groups={groups} onGroupCreated={() => void refreshData()} onReplace={() => setDialog({k: 'cluster', cluster: dialog.cluster, replace: true})} onChanged={() => void refreshData()} onClose={close} onSaved={() => { close(); void refreshData() }} />
            : <ClusterAddForm key={dialog.cluster?.id ?? 'new'} groups={groups} onGroupCreated={() => void refreshData()} replace={dialog.cluster} onClose={close} onSaved={c => { close(); void refreshData(); toast.info(dialog.replace ? t('Kubeconfig replaced') : t('Added {name}', {name: c.label})) }} />)}
          {dialog?.k === 'snippet' && <SnippetForm key={dialog.snippet?.id ?? 'new'} snippet={dialog.snippet} onClose={close} onSaved={() => { close(); void refreshData() }} />}
        </div>
        <Workspace tabs={tabs} activeTab={onHome ? '' : termActive} settings={settings} appearance={appearance} shortcuts={bindings} hosts={hosts} onFocusPane={focusPane} onPaneExit={paneExit} onClosePane={closePaneAsk} onToggleBroadcast={broadcastTab} onToggleZoom={zoomPane} onDropTab={dropTab} onMovePane={movePaneTo} onPopPane={popPaneOut} onProtocol={chooseProtocol} onAccessHost={accessHost} onShell={shellInPod} onPodShell={openPodShell} clusters={clusters} onOpenFromNewTab={openFromBlank} onLocalFromNewTab={localFromBlank} onSelectTab={id => setActive(id)} groups={groups} onCloseTab={closeTab} onShowLogs={() => goHome('logs')}
          renderSettings={() => vault && (
            <SettingsWindow page settings={settings} appearance={appearance} vault={vault} onSettings={setSettings}
              onVault={() => void refreshVault()} onChanged={() => void refreshData()} onClose={() => void closeTab(SETTINGS_TAB_ID)} />
          )} />
        {sidebarOpen && !onHome && activeTabObj && isTerminalTab(activeTabObj) && (
          <TerminalSidebar tab={sidebarTab} onTab={setSidebarTab} hosts={hosts} groups={groups} tabs={tabs} activeTab={termActive} onSelectTab={id => setActive(id)} onCloseTab={closeTab} snippets={snippets} settings={settings} appearance={appearance}
            onInsert={insertIntoTerminal} onSnippet={runSnippet} onNewSnippet={() => { goHome('snippets'); setDialog({k: 'snippet'}) }} onConnect={connect} onSettings={saveSettings}
            paneId={activeTabObj.activePane} hostId={leaves(activeTabObj.root).find(l => l.id === activeTabObj.activePane)?.hostId} onOpenSettings={() => openSettings()} width={sideWidth} onWidth={setSideWidth} />
        )}
      </div>

      {settingsOpen && vault && (
        <SettingsWindow settings={settings} appearance={appearance} vault={vault} onSettings={setSettings}
          onVault={() => void refreshVault()} onChanged={() => void refreshData()} onClose={() => setSettingsOpen(false)} onOpenAsTab={settingsToTab} />
      )}
      {fill && (
        <FillVariables title={fill.execute ? t('Run “{name}”', {name: fill.snippet.label}) : t('Paste “{name}”', {name: fill.snippet.label})} variables={parseVariables(fill.snippet.script)} verb={fill.execute ? t('Run') : t('Paste')}
          onClose={() => setFill(null)}
          onAsWritten={() => { const f = fill; setFill(null); sendSnippet(f.snippet, f.snippet.script, f.execute) }}
          onSubmit={values => { const f = fill; setFill(null); sendSnippet(f.snippet, fillVariables(f.snippet.script, values), f.execute) }} />
      )}
      {runHosts && <RunOnHostsPanel hosts={hosts} snippets={snippets} snippet={runHosts.snippet} onClose={() => setRunHosts(null)} />}
      {dialog?.k === 'data' && <DataPanel onClose={close} onChanged={() => void refreshData()} />}
      {dialog?.k === 'about' && <AboutPanel onClose={close} checkNow={dialog.check} />}
      {dialog?.k === 'support' && <SupportPanel onClose={close} />}
    </div>
  )
}

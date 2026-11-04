import {KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent, useEffect, useMemo, useRef, useState} from 'react'
import {clearHistory, getHistory, subscribeHistory} from './history'
import {TERMINAL_THEMES, resolveTerminalTheme} from './themes'
import {AssistantTab} from '../assistant/AssistantTab'
import {FilesTab} from '../sftp/SidebarFiles'
import {IconBraces, IconChevron, IconChevronDown, IconClock, IconClose, IconFolder, IconMinus, IconPalette, IconPlus, IconRocket, IconSearch, IconServer, IconSparkle, IconTrash} from '../ui/Icons'
import {buildTree, countHosts, countOpen, filterTree, type GroupNode} from '../hosts/tree'
import {isTerminalTab, leaves, type Tab} from '../layout'
import type {Group, Host, Settings, Snippet} from '../types'
import type {Appearance} from '../settings/appearance'
import {useT, type TFn} from '../i18n'

export type SidebarTab = 'hosts' | 'snippets' | 'history' | 'files' | 'themes' | 'assistant'

/** The sidebar can be dragged wider or narrower; these keep both the sidebar and the terminal usable. */
export const SIDE_MIN = 260
export const SIDE_MAX = 760
export const SIDE_DEFAULT = 320
const TERMINAL_MIN = 360

export const FONT_MIN = 8
export const FONT_MAX = 32
const FONTS = ['Source Code Pro', 'JetBrains Mono', 'Fira Code', 'Menlo', 'SF Mono', 'Monaco', 'Consolas', 'Courier New', 'monospace']

interface Props {
  tab: SidebarTab
  onTab(t: SidebarTab): void
  hosts: Host[]
  /** the host groups, shown as a tree (nested through parent_id) */
  groups?: Group[]
  /** the open tabs: a host with a session open shows a dot, and a click on it switches to that tab */
  tabs?: Tab[]
  activeTab?: string
  onSelectTab?(id: string): void
  onCloseTab?(id: string): void
  snippets: Snippet[]
  settings: Settings
  appearance: Appearance
  /** type this into the focused pane; `run` presses Enter after it */
  onInsert(text: string, run: boolean): void
  /** run or paste a snippet (it may have blanks to fill in first); without it a snippet is typed as it is */
  onSnippet?(s: Snippet, run: boolean): void
  onNewSnippet?(): void
  onConnect?(h: Host): void
  /** a theme or font was picked: save these settings */
  onSettings?(patch: Partial<Settings>): void
  /** the focused pane: the AI assistant reads and helps this terminal */
  paneId?: string
  /** the host of the focused pane ('local', 'serial:…' and 'telnet:…' for the others): the Files section browses it over SFTP */
  hostId?: string
  /** the assistant asks the user to enable a provider */
  onOpenSettings?(): void
  /** the width the user chose (remembered by the caller) */
  width?: number
  /** the user finished dragging the edge, or reset it: remember this width */
  onWidth?(w: number): void
}

const sideTabs = (t: TFn): {id: SidebarTab; label: string; icon: JSX.Element}[] => [
  {id: 'hosts', label: t('Hosts'), icon: <IconRocket size={17} />},
  {id: 'snippets', label: t('Snippets'), icon: <IconBraces size={17} />},
  {id: 'history', label: t('History'), icon: <IconClock size={17} />},
  {id: 'files', label: t('Files (SFTP)'), icon: <IconFolder size={17} />},
  {id: 'themes', label: t('Themes and font'), icon: <IconPalette size={17} />},
  {id: 'assistant', label: t('AI assistant'), icon: <IconSparkle size={17} />},
]

/** A tiny terminal drawn in a theme's own colours. */
export function ThemeThumb({colors}: {colors: {bg: string; fg: string; green: string; blue: string; red: string}}) {
  return (
    <span className="theme-thumb" style={{background: colors.bg}} aria-hidden="true">
      <i style={{background: colors.fg, width: '80%'}} />
      <i style={{background: colors.green, width: '95%'}} />
      <i style={{background: colors.blue, width: '45%'}} />
    </span>
  )
}

/**
 * The panel on the right of a terminal: hosts to open, snippets to run, the commands typed
 * before, the files on the server, and the colours and font of the terminal.
 */
export function TerminalSidebar(p: Props) {
  const t = useT()
  const [width, setWidth] = useState(p.width ?? SIDE_DEFAULT)
  const [dragging, setDragging] = useState(false)
  const drag = useRef<{x: number; w: number} | null>(null)
  useEffect(() => { setWidth(p.width ?? SIDE_DEFAULT) }, [p.width])
  const clamp = (v: number) => Math.round(Math.max(SIDE_MIN, Math.min(SIDE_MAX, window.innerWidth - TERMINAL_MIN, v)))
  const commit = (w: number) => { const c = clamp(w); setWidth(c); p.onWidth?.(c) }
  const move = (e: ReactPointerEvent<HTMLDivElement>) => { if (drag.current) setWidth(clamp(drag.current.w + (drag.current.x - e.clientX))) }
  const end = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current) return
    const w = clamp(drag.current.w + (drag.current.x - e.clientX))
    drag.current = null
    setDragging(false)
    commit(w)
  }
  return (
    <aside className={'term-side' + (dragging ? ' resizing' : '')} style={{width}} aria-label={t('Terminal sidebar')}>
      <div role="separator" aria-orientation="vertical" aria-label={t('Resize sidebar')} aria-valuemin={SIDE_MIN} aria-valuemax={SIDE_MAX} aria-valuenow={width} tabIndex={0}
        className="side-resizer" title={t('Drag to resize; double-click to reset')}
        onPointerDown={e => { e.preventDefault(); e.currentTarget.setPointerCapture?.(e.pointerId); drag.current = {x: e.clientX, w: width}; setDragging(true) }}
        onPointerMove={move} onPointerUp={end} onPointerCancel={end}
        onDoubleClick={() => commit(SIDE_DEFAULT)}
        onKeyDown={e => {
          const step = e.shiftKey ? 64 : 16
          if (e.key === 'ArrowLeft') { e.preventDefault(); commit(width + step) } // the edge moves left: wider
          else if (e.key === 'ArrowRight') { e.preventDefault(); commit(width - step) }
          else if (e.key === 'Home') { e.preventDefault(); commit(SIDE_DEFAULT) }
        }} />
      <div className="side-tabs" role="toolbar" aria-label={t('Sidebar sections')}>
        {sideTabs(t).map(x => (
          <button key={x.id} aria-pressed={p.tab === x.id} aria-label={x.label} title={x.label}
            className={p.tab === x.id ? 'on' : ''} onClick={() => p.onTab(x.id)}>{x.icon}</button>
        ))}
      </div>
      <div className="side-body">
        {p.tab === 'hosts' && <HostsTab hosts={p.hosts} groups={p.groups ?? []} tabs={p.tabs ?? []} activeTab={p.activeTab ?? ''} onConnect={p.onConnect} onSelectTab={p.onSelectTab} onCloseTab={p.onCloseTab} />}
        {p.tab === 'snippets' && <SnippetsTab snippets={p.snippets} onInsert={p.onInsert} onSnippet={p.onSnippet} onNew={p.onNewSnippet} />}
        {p.tab === 'history' && <HistoryTab hostId={p.hostId ?? ''} onInsert={p.onInsert} />}
        {p.tab === 'files' && <FilesTab host={p.hosts.find(h => h.id === p.hostId) ?? null} />}
        {p.tab === 'themes' && <ThemesTab settings={p.settings} appearance={p.appearance} onSettings={p.onSettings} />}
        {p.tab === 'assistant' && <AssistantTab paneId={p.paneId ?? ''} onOpenSettings={() => p.onOpenSettings?.()} />}
      </div>
    </aside>
  )
}

function SearchBox({value, onChange, label}: {value: string; onChange(v: string): void; label: string}) {
  const t = useT()
  return (
    <label className="side-search">
      <IconSearch size={15} />
      <input aria-label={label} placeholder={t('Search')} value={value} onChange={e => onChange(e.target.value)} />
    </label>
  )
}

const FOLD_KEY = 'vutty.sidebar.folded'
function loadFolded(): Set<string> {
  try { return new Set<string>(JSON.parse(localStorage.getItem(FOLD_KEY) || '[]')) } catch { return new Set() }
}

/** host id -> the terminal tabs that have a session on it (a split tab counts once per host) */
function openByHost(tabs: Tab[]): Map<string, Tab[]> {
  const m = new Map<string, Tab[]>()
  for (const t of tabs) {
    if (!isTerminalTab(t)) continue
    for (const id of new Set(leaves(t.root).map(l => l.hostId.replace(/^telnet:/, '')))) m.set(id, [...(m.get(id) ?? []), t])
  }
  return m
}

interface HostsTabProps {
  hosts: Host[]; groups: Group[]; tabs: Tab[]; activeTab: string
  onConnect?(h: Host): void; onSelectTab?(id: string): void; onCloseTab?(id: string): void
}

/**
 * Every host in its group, as a tree. A host with a session open has a dot: a click jumps to that tab
 * (again to the next one when there are several), Alt/Shift-click always opens a new one. Typing a search
 * unfolds the groups that lead to a match.
 */
function HostsTab({hosts, groups, tabs, activeTab, onConnect, onSelectTab, onCloseTab}: HostsTabProps) {
  const t = useT()
  const [q, setQ] = useState('')
  const [folded, setFolded] = useState(loadFolded)
  const searching = q.trim() !== ''
  const open = useMemo(() => openByHost(tabs), [tabs])
  const tree = useMemo(() => filterTree(buildTree(groups, hosts), q), [groups, hosts, q])
  const toggle = (id: string) => setFolded(f => {
    const n = new Set(f)
    if (!n.delete(id)) n.add(id)
    try { localStorage.setItem(FOLD_KEY, JSON.stringify([...n])) } catch { /* not remembered */ }
    return n
  })
  const pick = (h: Host, fresh: boolean) => {
    const mine = open.get(h.id)
    if (fresh || !mine?.length) return onConnect?.(h)
    // already open: go to it; when this is one of several, go to the next
    const i = mine.findIndex(x => x.id === activeTab)
    onSelectTab?.(mine[(i + 1) % mine.length].id)
  }
  const guides = (depth: number) => Array.from({length: depth}, (_, i) => <i key={i} className="tree-guide" aria-hidden="true" />)
  const hostRow = (h: Host, depth: number) => {
    const mine = open.get(h.id) ?? []
    // the second part is only worth showing when it says something the label does not
    const sub = h.label === h.hostname ? h.username : (h.username ? h.username + '@' : '') + h.hostname
    return (
      <button key={h.id} role="treeitem" aria-level={depth + 1} className={'side-item tree-host' + (mine.some(x => x.id === activeTab) ? ' on' : '') + (mine.length ? ' live' : '')}
        onClick={e => pick(h, e.altKey || e.shiftKey)}
        title={(mine.length ? t('Switch to {name} (Alt-click: new tab)', {name: h.label}) : t('Open {name} in a new tab', {name: h.label})) + '\n' + (h.username ? h.username + '@' : '') + h.hostname}>
        {guides(depth)}
        <span className="tree-lead">
          <IconServer size={14} />
          {h.env && <i className={'env-pip env-' + h.env} aria-label={h.env} />}
        </span>
        <span className="tree-label"><b>{h.label}</b>{sub && <small>{sub}</small>}</span>
        {mine.length > 0 && <span className="tree-live" aria-label={t('{n} open', {n: mine.length})}>{mine.length > 1 ? mine.length : ''}</span>}
      </button>
    )
  }
  const groupRow = (n: GroupNode, depth: number): JSX.Element => {
    const isOpen = searching || !folded.has(n.group.id)
    const live = countOpen(n, open)
    return (
      <div key={n.group.id} role="group">
        <button role="treeitem" aria-level={depth + 1} aria-expanded={isOpen} className="side-item tree-group" data-gid={n.group.id}
          onClick={() => toggle(n.group.id)}>
          {guides(depth)}
          <span className="tree-chev">{isOpen ? <IconChevronDown size={13} /> : <IconChevron size={13} />}</span>
          <span className="tree-folder" style={n.group.color ? {color: n.group.color} : undefined}><IconFolder size={14} /></span>
          <span className="tree-name" style={n.group.color ? {color: n.group.color} : undefined}>{n.group.name}</span>
          <small className="tree-count">{live > 0 && <b>{live}/</b>}{countHosts(n)}</small>
        </button>
        {isOpen && <>
          {n.children.map(c => groupRow(c, depth + 1))}
          {n.hosts.map(h => hostRow(h, depth + 1))}
        </>}
      </div>
    )
  }
  const allGroupIds = groups.map(g => g.id)
  const setAll = (fold: boolean) => {
    const n = fold ? new Set(allGroupIds) : new Set<string>()
    try { localStorage.setItem(FOLD_KEY, JSON.stringify([...n])) } catch { /* not remembered */ }
    setFolded(n)
  }
  // arrows walk the tree like in a file manager: up and down move, right opens a group, left closes it (or goes to its parent)
  const keys = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="treeitem"]')]
    const i = items.indexOf(document.activeElement as HTMLElement)
    if (i < 0) return
    const cur = items[i]
    const level = Number(cur.getAttribute('aria-level'))
    const group = cur.getAttribute('aria-expanded') !== null
    const go = (el?: HTMLElement) => { if (el) { e.preventDefault(); el.focus() } }
    if (e.key === 'ArrowDown') go(items[i + 1])
    else if (e.key === 'ArrowUp') go(items[i - 1])
    else if (e.key === 'Home') go(items[0])
    else if (e.key === 'End') go(items[items.length - 1])
    else if (e.key === 'ArrowRight' && group) {
      e.preventDefault()
      if (cur.getAttribute('aria-expanded') === 'false') cur.click()
      else go(items[i + 1])
    } else if (e.key === 'ArrowLeft') {
      if (group && cur.getAttribute('aria-expanded') === 'true' && !searching) { e.preventDefault(); cur.click() }
      else go([...items.slice(0, i)].reverse().find(x => Number(x.getAttribute('aria-level')) < level))
    }
  }
  const active = tabs.filter(isTerminalTab)
  const hostOf = (x: Tab) => hosts.find(h => h.id === leaves(x.root)[0]?.hostId.replace(/^telnet:/, ''))
  const empty = tree.roots.length === 0 && tree.ungrouped.length === 0
  return (
    <>
      <div className="side-bar tree-bar">
        <SearchBox value={q} onChange={setQ} label={t('Search hosts in the sidebar')} />
        {groups.length > 0 && <>
          <button className="icon ghost" aria-label={t('Expand all groups')} title={t('Expand all groups')} onClick={() => setAll(false)}><IconPlus size={14} /></button>
          <button className="icon ghost" aria-label={t('Collapse all groups')} title={t('Collapse all groups')} onClick={() => setAll(true)}><IconMinus size={14} /></button>
        </>}
      </div>
      {active.length > 0 && !searching && (
        <div className="side-list tree-active" role="list" aria-label={t('Active sessions')}>
          <h4 className="side-sub">{t('Active')} <small>{active.length}</small></h4>
          {active.map(x => {
            const h = hostOf(x)
            return (
              <div key={x.id} role="listitem" className={'side-item tree-session' + (x.id === activeTab ? ' on' : '')}>
                <button className="tree-session-main" aria-label={t('Switch to {name}', {name: x.title})} aria-current={x.id === activeTab} onClick={() => onSelectTab?.(x.id)}>
                  <span className="tree-lead"><i className="live-dot" aria-hidden="true" /></span>
                  <span className="tree-label"><b>{x.title}</b>{h?.env && <i className={'env-pip env-' + h.env + ' inline'} aria-label={h.env} />}{h && h.hostname !== x.title && <small>{h.hostname}</small>}</span>
                </button>
                {onCloseTab && <button className="icon ghost" aria-label={t('End session {name}', {name: x.title})} onClick={() => onCloseTab(x.id)}><IconClose size={13} /></button>}
              </div>
            )
          })}
        </div>
      )}
      <div className="side-list tree" role="tree" aria-label={t('Hosts')} onKeyDown={keys}>
        {tree.roots.map(n => groupRow(n, 0))}
        {tree.ungrouped.map(h => hostRow(h, 0))}
        {empty && <div className="side-empty">{hosts.length ? t('No hosts match.') : t('No hosts yet.')}</div>}
      </div>
    </>
  )
}

function SnippetsTab({snippets, onInsert, onSnippet, onNew}: {snippets: Snippet[]; onInsert(text: string, run: boolean): void; onSnippet?(s: Snippet, run: boolean): void; onNew?(): void}) {
  const t = useT()
  const insert = (s: Snippet, run: boolean) => (onSnippet ? onSnippet(s, run) : onInsert(s.script, run))
  const [q, setQ] = useState('')
  const [searching, setSearching] = useState(false)
  const [open, setOpen] = useState<string | null>(null) // the snippet shown in full
  const shown = snippets.filter(s => `${s.label} ${s.script}`.toLowerCase().includes(q.trim().toLowerCase()))
  return (
    <>
      <div className="side-bar">
        {searching
          ? <SearchBox value={q} onChange={setQ} label={t('Search snippets in the sidebar')} />
          : <button className="side-new" onClick={onNew}><IconBraces size={14} /> {t('New Snippet')}</button>}
        <button className="icon ghost" aria-label={searching ? t('Close search') : t('Search snippets')} onClick={() => { setSearching(!searching); setQ('') }}><IconSearch size={16} /></button>
      </div>
      <div className="side-list" role="list" aria-label={t('Snippets')}>
        {shown.map(s => {
          const full = open === s.id
          return (
            <div key={s.id} role="listitem" className={'side-snippet' + (full ? ' open' : '')}>
              <div className="side-snippet-head">
                <button className="side-snippet-title" aria-expanded={full} aria-label={s.label} onClick={() => setOpen(full ? null : s.id)}>
                  <IconBraces size={13} /> <span>{s.label}</span>
                </button>
                {full && <>
                  <button className="side-act" aria-label={t('Run {name}', {name: s.label})} onClick={() => insert(s, true)}>{t('Run')}</button>
                  <button className="side-act" aria-label={t('Paste {name}', {name: s.label})} onClick={() => insert(s, false)}>{t('Paste')}</button>
                </>}
              </div>
              {full ? <pre className="side-snippet-code">{s.script}</pre> : <code className="side-snippet-peek" onClick={() => setOpen(s.id)}>{s.script.replace(/\s+/g, ' ')}</code>}
            </div>
          )
        })}
        {shown.length === 0 && <div className="side-empty">{snippets.length ? t('No snippets match.') : t('No snippets yet.')}</div>}
      </div>
    </>
  )
}

/** The commands typed at the host of the focused pane: another host has its own list. */
function HistoryTab({hostId, onInsert}: {hostId: string; onInsert(text: string, run: boolean): void}) {
  const t = useT()
  const [q, setQ] = useState('')
  const [items, setItems] = useState(() => getHistory(hostId))
  useEffect(() => {
    setItems(getHistory(hostId))
    return subscribeHistory(() => setItems(getHistory(hostId)))
  }, [hostId])
  const shown = useMemo(() => items.filter(c => c.toLowerCase().includes(q.trim().toLowerCase())), [items, q])
  return (
    <>
      <div className="side-bar">
        <SearchBox value={q} onChange={setQ} label={t('Search history')} />
        <button className="icon ghost danger" aria-label={t('Clear history')} title={t('Clear history')} disabled={items.length === 0} onClick={() => clearHistory(hostId)}><IconTrash size={15} /></button>
      </div>
      <div className="side-list" role="list" aria-label={t('History')}>
        {shown.map(c => (
          <button key={c} className="side-item" onClick={() => onInsert(c, false)} onDoubleClick={() => onInsert(c, true)}
            title={t('Click to type it, double-click to run it')}>
            <span className="side-main"><span className="side-cmd">{c}</span></span>
          </button>
        ))}
        {shown.length === 0 && <div className="side-empty">{items.length ? t('Nothing matches.') : t('Commands you type on this host will show up here.')}</div>}
      </div>
    </>
  )
}

function ThemesTab({settings, appearance, onSettings}: {settings: Settings; appearance: Appearance; onSettings?(patch: Partial<Settings>): void}) {
  const t = useT()
  const [fontOpen, setFontOpen] = useState(true)
  const size = settings.font_size || 14
  const setSize = (n: number) => onSettings?.({font_size: Math.max(FONT_MIN, Math.min(FONT_MAX, n))})
  const current = settings.terminal_theme || 'auto'
  const app = resolveTerminalTheme('auto', appearance).theme
  return (
    <>
      <button className={'side-fold' + (fontOpen ? ' open' : '')} aria-expanded={fontOpen} onClick={() => setFontOpen(!fontOpen)}>
        {t('Font')} {fontOpen ? <IconChevronDown size={16} /> : <IconChevron size={16} />}
      </button>
      {fontOpen && (
        <div className="side-font">
          <input aria-label={t('Font family')} list="term-fonts" value={settings.font_family} spellCheck={false}
            onChange={e => onSettings?.({font_family: e.target.value})} />
          <datalist id="term-fonts">{FONTS.map(f => <option key={f} value={f} />)}</datalist>
          <div className="side-size">
            <span>{t('Text Size')}</span>
            <button aria-label={t('Smaller text')} disabled={size <= FONT_MIN} onClick={() => setSize(size - 1)}><IconMinus size={15} /></button>
            <output aria-label={t('Text size')}>{size}</output>
            <button aria-label={t('Larger text')} disabled={size >= FONT_MAX} onClick={() => setSize(size + 1)}><IconPlus size={15} /></button>
          </div>
        </div>
      )}
      <h4 className="side-sub">{t('Themes')}</h4>
      <div className="side-list" role="listbox" aria-label={t('Themes')}>
        <ThemeRow id="auto" name={t('Match app theme')} note={t('follows light / dark')} on={current === 'auto'} colors={app} onPick={() => onSettings?.({terminal_theme: 'auto'})} />
        {TERMINAL_THEMES.map(x => (
          <ThemeRow key={x.id} id={x.id} name={x.name} note={x.dark ? t('dark') : t('light')} on={current === x.id} colors={x.theme}
            onPick={() => onSettings?.({terminal_theme: x.id})} />
        ))}
      </div>
    </>
  )
}

function ThemeRow(p: {id: string; name: string; note: string; on: boolean; colors: {background?: string; foreground?: string; green?: string; blue?: string; red?: string}; onPick(): void}) {
  const c = p.colors
  return (
    <button role="option" aria-selected={p.on} className={'side-item theme' + (p.on ? ' on' : '')} onClick={p.onPick} data-theme-id={p.id}>
      <ThemeThumb colors={{bg: c.background ?? '#000', fg: c.foreground ?? '#fff', green: c.green ?? '#0f0', blue: c.blue ?? '#00f', red: c.red ?? '#f00'}} />
      <span className="side-main"><b>{p.name}</b><small>{p.note}</small></span>
    </button>
  )
}

import {useT, useTn} from '../i18n'
import {OS_INFO, OsIcon, osName} from '../ui/OsIcon'
import {loadView, saveView, ViewMenu, ViewMode} from './ViewMenu'
import {TagMenu} from './TagMenu'
import {byName, SortMenu, usePref, type SortOption} from '../ui/ListTools'
import {getRecent} from './recent'
import {DragEvent, ReactNode, useEffect, useMemo, useState} from 'react'
import type {Env, Group, Host} from '../types'
import {formatAddr} from '../util'
import {IconChevron, IconEdit, IconFolder, IconForward, IconPlay, IconSearch, IconServer, IconTerminal, IconTrash} from '../ui/Icons'

export interface QuickConnect {
  username: string
  hostname: string
  port: number
}

interface Props {
  /** changes each time the Hosts item of the navigation is pressed again: back to "All hosts" */
  resetKey?: number
  groups: Group[]
  hosts: Host[]
  /** Rendered between the search bar and the lists (New host / New group buttons). */
  toolbar?: ReactNode
  onConnect(h: Host): void
  onEdit(h: Host): void
  onDelete(h: Host): void
  onForwards(h: Host): void
  /** open the host with Telnet (only offered for hosts that have it enabled) */
  onTelnet?(h: Host): void
  /** a host card was dropped on a group card (or on "All hosts", groupId ''): put the host there */
  onMoveHost?(h: Host, groupId: string): void
  onEditGroup(g: Group): void
  onDeleteGroup(g: Group): void
  /** the group being shown changed ('' = All hosts); also called once on mount */
  onGroupChange?(groupId: string): void
  /** Text being typed in the New host panel: shown as a highlighted draft card. `undefined` = no draft. */
  draft?: string
  /** The typed text is `[ssh ][user@]host[:port]` and matches no saved host. */
  onQuickConnect?(q: QuickConnect): void
}

const ENVS: Env[] = ['prod', 'staging', 'dev']
export const HOST_DRAG = 'application/x-vutty-host'

/** Parses "ssh user@host:2222", "ssh://user@host:2222/", "user@host" or "host". */
export function parseQuickConnect(text: string): QuickConnect | null {
  const m = text.trim().replace(/^ssh(?:\s+|:\/\/)/i, '').replace(/\/+$/, '').match(/^(?:([^\s@]+)@)?([A-Za-z0-9_.\-[\]:]+?)(?::(\d{1,5}))?$/)
  if (!m) return null
  const port = m[3] ? Number(m[3]) : 22
  if (port < 1 || port > 65535) return null
  return {username: m[1] ?? '', hostname: m[2], port}
}

/** A bare name or a half-typed address is still a search; these look like somewhere to connect to. */
function looksComplete(text: string, q: QuickConnect): boolean {
  if (/^ssh(\s|:\/\/)/i.test(text.trim()) || q.username !== '' || /:\d+$/.test(text.trim())) return true // ssh …, user@host, host:port
  const h = q.hostname
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(h)) return h.split('.').every(o => Number(o) <= 255) // 10.0.0.1, not 10.0
  return /\.[A-Za-z]{2,}$/.test(h) || h.includes(':') || h.toLowerCase() === 'localhost'
}

/**
 * What Connect would do with the typed text, or null while it is still only a search: the text must name one saved host
 * exactly (its label, address or user@address), or, when it matches no saved host at all, be a complete address to start a
 * new host from. A partial name ("54", "web") that matches some hosts just filters them.
 */
export function connectTarget(text: string, matches: Host[], allowNew: boolean): {host: Host} | {quick: QuickConnect} | null {
  const needle = text.trim().replace(/^ssh(?:\s+|:\/\/)/i, '').replace(/\/+$/, '').toLowerCase()
  if (!needle) return null
  const same = matches.filter(h => [h.label, h.hostname, `${h.hostname}:${h.port}`, `${h.username}@${h.hostname}`, `${h.username}@${h.hostname}:${h.port}`].some(a => a.toLowerCase() === needle))
  if (same.length === 1) return {host: same[0]}
  if (matches.length > 0 || !allowNew) return null
  const quick = parseQuickConnect(text)
  return quick && looksComplete(text, quick) ? {quick} : null
}

function descendants(groups: Group[], id: string): Set<string> {
  const out = new Set([id])
  let grew = true
  while (grew) {
    grew = false
    for (const g of groups) if (g.parent_id && out.has(g.parent_id) && !out.has(g.id)) { out.add(g.id); grew = true }
  }
  return out
}

type Sort = 'name' | 'name-desc' | 'recent' | 'env'
const SORT_IDS: Sort[] = ['name', 'name-desc', 'recent', 'env']
const ENV_RANK: Record<string, number> = {prod: 0, staging: 1, dev: 2, '': 3}

/** Hosts page: find/connect bar, environment filter, group cards and host cards. */
export function HostList(p: Props) {
  const t = useT()
  const tn = useTn()
  const SORTS: SortOption<Sort>[] = [{value: 'name', label: t('Name A–Z')}, {value: 'name-desc', label: t('Name Z–A')}, {value: 'recent', label: t('Recently connected')}, {value: 'env', label: t('Environment')}]
  const [q, setQ] = useState('')
  const [env, setEnv] = useState<Env>('')
  const [view, setView] = useState<ViewMode>(loadView)
  const changeView = (v: ViewMode) => { setView(v); saveView(v) }
  const [groupId, setGroupId] = useState('')
  const [sort, setSort] = usePref<Sort>('vutty.hosts.sort', SORT_IDS, 'name')
  useEffect(() => { if (p.resetKey) setGroupId('') }, [p.resetKey])
  const [tagFilter, setTagFilter] = useState<string[]>([])
  const allTags = useMemo(() => Array.from(new Set(p.hosts.flatMap(h => h.tags ?? []))).sort((a, b) => a.localeCompare(b)), [p.hosts])
  // a tag that no host has any more (deleted elsewhere) is dropped from the filter
  const activeTags = tagFilter.filter(tag => allTags.includes(tag))
  const [dropOn, setDropOn] = useState<string | null>(null) // the group (or '' for All hosts) a host is being dragged over
  /** drop target props: a host card dragged over turns the target on, dropping it moves the host */
  const target = (gid: string) => ({
    onDragOver: (e: DragEvent) => { if (p.onMoveHost && Array.from(e.dataTransfer.types).includes(HOST_DRAG)) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; setDropOn(gid) } },
    onDragLeave: () => setDropOn(d => (d === gid ? null : d)),
    onDrop: (e: DragEvent) => {
      setDropOn(null)
      const id = e.dataTransfer.getData(HOST_DRAG)
      const h = p.hosts.find(x => x.id === id)
      if (!id || !h || !p.onMoveHost) return
      e.preventDefault()
      if ((h.group_id || '') !== gid) p.onMoveHost(h, gid)
    },
  })

  const groupIds = useMemo(() => new Set(p.groups.map(g => g.id)), [p.groups])
  // a group whose id vanished (deleted elsewhere) falls back to the root
  const current = groupId && groupIds.has(groupId) ? groupId : ''
  const onGroupChange = p.onGroupChange
  useEffect(() => { onGroupChange?.(current) }, [current, onGroupChange])
  const filtering = q.trim() !== '' || env !== '' || activeTags.length > 0

  const matches = useMemo(() => {
    const needle = q.trim().toLowerCase().replace(/^ssh\s+/, '')
    return p.hosts.filter(h => {
      if (env && h.env !== env) return false
      if (activeTags.length && !(h.tags ?? []).some(tag => activeTags.includes(tag))) return false
      if (!needle) return true
      return `${h.label} ${h.hostname} ${h.username} ${h.username}@${h.hostname} ${h.username}@${h.hostname}:${h.port} ${(h.tags ?? []).join(' ')}`.toLowerCase().includes(needle)
    })
  }, [p.hosts, q, env, activeTags.join('\u0000')])

  const subgroups = useMemo(
    () => p.groups.filter(g => (g.parent_id && groupIds.has(g.parent_id) ? g.parent_id : '') === current).sort((a, b) => (sort === 'name-desc' ? byName(b.name, a.name) : byName(a.name, b.name))),
    [p.groups, groupIds, current, sort])
  // "All hosts" lists every host, whichever group it is in; inside a group, the hosts of that group
  const unsorted = filtering
    ? matches
    : current === '' ? p.hosts : p.hosts.filter(h => h.group_id === current)
  const visibleHosts = useMemo(() => {
    const recent = getRecent()
    const rank = (h: Host) => { const i = recent.indexOf(h.id); return i < 0 ? 1e6 : i }
    return [...unsorted].sort((a, b) => {
      switch (sort) {
        case 'name-desc': return byName(b.label, a.label)
        case 'recent': return rank(a) - rank(b) || byName(a.label, b.label)
        case 'env': return (ENV_RANK[a.env ?? ''] ?? 3) - (ENV_RANK[b.env ?? ''] ?? 3) || byName(a.label, b.label)
        default: return byName(a.label, b.label)
      }
    })
  }, [unsorted, sort])

  const count = (g: Group) => {
    const ids = descendants(p.groups, g.id)
    return p.hosts.filter(h => h.group_id && ids.has(h.group_id)).length
  }
  const trail = useMemo(() => {
    const out: Group[] = []
    for (let id = current, n = 0; id && n < 16; n++) {
      const g = p.groups.find(x => x.id === id)
      if (!g) break
      out.unshift(g)
      id = g.parent_id && groupIds.has(g.parent_id) ? g.parent_id : ''
    }
    return out
  }, [p.groups, groupIds, current])

  const goTo = connectTarget(q, matches, !!p.onQuickConnect)
  const connectFromBar = () => {
    if (!goTo) return
    if ('host' in goTo) p.onConnect(goTo.host)
    else p.onQuickConnect?.(goTo.quick)
  }

  const hostCard = (h: Host) => (
    <div key={h.id} className="host card" role="listitem" draggable={!!p.onMoveHost} onDragStart={e => { e.dataTransfer.setData(HOST_DRAG, h.id); e.dataTransfer.effectAllowed = 'move' }} onDoubleClick={() => p.onConnect(h)} title={t('Double-click to connect to {name}', {name: h.label})}>
      {osName(h.os)
        ? (
          <span className="tile os-tile" style={{background: OS_INFO[h.os!].color}} title={`${osName(h.os)}${h.env ? ' · ' + h.env : ''}`} data-os={h.os}>
            <OsIcon os={h.os} size={22} />
            {h.env && <i className={'env-pip env-' + h.env} aria-label={h.env} />}
          </span>
        )
        : <span className={'tile env-' + (h.env || 'none')} title={h.env || t('no environment')}><IconServer size={20} /></span>}
      <div className="meta">
        <div className="label">{h.label}</div>
        <div className="addr">{['ssh', h.telnet_enabled ? 'telnet' : '', h.ftp_enabled ? (h.ftp_security ? 'ftps' : 'ftp') : '', h.rdp_enabled ? 'rdp' : '', h.vnc_enabled ? 'vnc' : '', h.username].filter(Boolean).join(', ')} · {formatAddr({hostname: h.hostname, port: h.port})}</div>
      </div>
      <div className="actions">
        <button className="icon ghost" aria-label={t('Connect {name}', {name: h.label})} onClick={() => p.onConnect(h)}><IconPlay size={14} /></button>
        {p.onTelnet && h.telnet_enabled && <button className="icon ghost" aria-label={t('Telnet {name}', {name: h.label})} title={t('Connect with Telnet on port {port} (unencrypted)', {port: h.telnet_port || 23})} onClick={() => p.onTelnet!(h)}><IconTerminal size={14} /></button>}
        <button className="icon ghost" aria-label={t('Forwards {name}', {name: h.label})} onClick={() => p.onForwards(h)}><IconForward size={14} /></button>
        <button className="icon ghost" aria-label={t('Edit {name}', {name: h.label})} onClick={() => p.onEdit(h)}><IconEdit size={14} /></button>
        <button className="icon ghost danger" aria-label={t('Delete {name}', {name: h.label})} onClick={() => p.onDelete(h)}><IconTrash size={14} /></button>
      </div>
    </div>
  )

  return (
    <div className="hosts-page">
      <div className="connect-bar">
        <IconSearch size={16} />
        <input aria-label={t('Search hosts')} placeholder={t('Find a host or ssh user@hostname…')} value={q}
          onChange={e => setQ(e.target.value)} onKeyDown={e => e.key === 'Enter' && connectFromBar()} />
        <button className="connect-btn" disabled={!goTo} onClick={connectFromBar}
          title={goTo ? ('host' in goTo ? t('Connect to {name}', {name: goTo.host.label}) : t('Connect to a new host')) : q.trim() ? t('Keep typing the name of a host, or an address such as user@host') : undefined}>{t('Connect')}</button>
      </div>

      <div className="toolbar">
        {p.toolbar}
        <div className="spacer" />
        <SortMenu value={sort} options={SORTS} onChange={setSort} />
        <ViewMenu value={view} onChange={changeView} />
        <TagMenu tags={allTags} selected={activeTags} onChange={setTagFilter} />
        <div className="chips">
          {ENVS.map(e => (
            <span key={e} role="button" aria-pressed={env === e} tabIndex={0} className={'chip' + (env === e ? ' on' : '')}
              onClick={() => setEnv(env === e ? '' : e)} onKeyDown={ev => ev.key === 'Enter' && setEnv(env === e ? '' : e)}>
              <i className={'dot env-' + e} /> {e}
            </span>
          ))}
        </div>
      </div>

      {!filtering && current && (
        <nav className="crumbs" aria-label={t('Group path')}>
          <button className={'link' + (dropOn === '' ? ' drop-over' : '')} onClick={() => setGroupId('')} {...target('')}>{t('All hosts')}</button>
          {trail.map((g, i) => (
            <span key={g.id}><IconChevron size={12} /> {i === trail.length - 1 ? <b>{g.name}</b> : <button className="link" onClick={() => setGroupId(g.id)}>{g.name}</button>}</span>
          ))}
        </nav>
      )}

      {!filtering && subgroups.length > 0 && (
        <>
          <h3 className="sec-title">{t('Groups')}</h3>
          <div className={'cards' + (view === 'list' ? ' as-list' : '')}>
            {subgroups.map(g => (
              <div key={g.id} className={'group card' + (dropOn === g.id ? ' drop-over' : '')} role="button" tabIndex={0} aria-label={t('Open group {name}', {name: g.name})} {...target(g.id)}
                onClick={() => setGroupId(g.id)} onKeyDown={e => e.key === 'Enter' && setGroupId(g.id)}>
                <span className="tile group-tile" style={g.color ? {background: g.color} : undefined}><IconFolder size={20} /></span>
                <div className="meta">
                  <div className="label">{g.name}</div>
                  <div className="addr">{tn(count(g), '{n} Host', '{n} Hosts')}</div>
                </div>
                <div className="actions" onClick={e => e.stopPropagation()}>
                  <button className="icon ghost" aria-label={t('Edit group {name}', {name: g.name})} onClick={() => p.onEditGroup(g)}><IconEdit size={14} /></button>
                  <button className="icon ghost danger" aria-label={t('Delete group {name}', {name: g.name})} onClick={() => p.onDeleteGroup(g)}><IconTrash size={14} /></button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      <h3 className="sec-title">{t('Hosts')}</h3>
      <div className={'cards' + (view === 'list' ? ' as-list' : '')} role="list" aria-label={t('Hosts')} data-view={view}>
        {p.draft !== undefined && (
          <div className="card draft" aria-label={t('New host draft')} data-testid="draft-card">
            <span className="tile env-none"><IconServer size={20} /></span>
            <div className="meta"><div className="label">{p.draft || t('Enter IP or Hostname…')}</div><div className="addr">ssh</div></div>
          </div>
        )}
        {visibleHosts.map(hostCard)}
      </div>
      {visibleHosts.length === 0 && p.draft === undefined && (
        <div className="empty-hint">
          {filtering ? t('No hosts match.') : current ? t('This group has no hosts yet.') : t('No hosts yet. Add one to get started.')}
        </div>
      )}
    </div>
  )
}

import {useMemo, useState, type DragEvent} from 'react'
import {useT, useTn} from '../i18n'
import {groupAndBelow} from '../hosts/tree'
import {IconChevron, IconCloud, IconEdit, IconFolder, IconFolderPlus, IconPlay, IconPlus, IconTrash, IconUpload} from '../ui/Icons'
import {byName, ListToolbar, usePref, VIEWS, type SortOption} from '../ui/ListTools'
import type {Cluster, Group} from '../types'

type Sort = 'name' | 'name-desc' | 'env'
const SORT_VALUES: Sort[] = ['name', 'name-desc', 'env']
const ENV_RANK: Record<string, number> = {prod: 0, staging: 1, dev: 2, '': 3}

interface Props {
  clusters: Cluster[]
  groups: Group[]
  onNew(): void
  onOpen(c: Cluster): void
  onEdit(c: Cluster): void
  onReplace(c: Cluster): void
  onDelete(c: Cluster): void
  /** groups are the ones of the Hosts page: made, changed and deleted the same way */
  onNewGroup(): void
  onEditGroup(g: Group): void
  onDeleteGroup(g: Group): void
  /** a cluster card was dropped on a group (groupId '' is out of every group) */
  onMoveCluster?(c: Cluster, groupId: string): void
}

const CLUSTER_DRAG = 'application/x-vutty-cluster'

/**
 * Clusters: Kubernetes clusters reached with a kubeconfig that someone gave you, with no SSH host in between. One card for each; Open
 * shows the cluster in a tab of its own. The kubeconfig is kept encrypted in the vault and is never shown again.
 */
export function ClustersPage(p: Props) {
  const t = useT()
  const tn = useTn()
  const [q, setQ] = useState('')
  const [groupId, setGroupId] = useState('')
  const [dropOn, setDropOn] = useState<string | null>(null) // the group ('' is All clusters) a cluster is dragged over
  const [sort, setSort] = usePref<Sort>('vutty.clusters.sort', SORT_VALUES, 'name')
  const [view, setView] = usePref('vutty.clusters.view', VIEWS, 'grid')
  const sorts: SortOption<Sort>[] = [{value: 'name', label: t('Name A–Z')}, {value: 'name-desc', label: t('Name Z–A')}, {value: 'env', label: t('Environment')}]
  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const list = p.clusters.filter(c => !needle || (c.label + '\n' + (c.server ?? '') + '\n' + (c.context ?? '')).toLowerCase().includes(needle))
    return [...list].sort((a, b) => (sort === 'name-desc' ? byName(b.label, a.label) : sort === 'env' ? (ENV_RANK[a.env ?? ''] ?? 3) - (ENV_RANK[b.env ?? ''] ?? 3) || byName(a.label, b.label) : byName(a.label, b.label)))
  }, [p.clusters, q, sort])
  const groupName = (id?: string) => p.groups.find(g => g.id === id)?.name
  const groupIds = useMemo(() => new Set(p.groups.map(g => g.id)), [p.groups])
  // a group whose id vanished (deleted elsewhere) falls back to the root
  const current = groupId && groupIds.has(groupId) ? groupId : ''
  const filtering = q.trim() !== ''
  const subgroups = useMemo(
    () => p.groups.filter(g => (g.parent_id && groupIds.has(g.parent_id) ? g.parent_id : '') === current).sort((a, b) => (sort === 'name-desc' ? byName(b.name, a.name) : byName(a.name, b.name))),
    [p.groups, groupIds, current, sort])
  // like the Hosts page: the root lists every cluster, inside a group the clusters of that group; a search looks everywhere
  const visible = filtering || current === '' ? shown : shown.filter(c => c.group_id === current)
  const count = (g: Group) => { const ids = groupAndBelow(p.groups, g.id); return p.clusters.filter(c => c.group_id && ids.has(c.group_id)).length }
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
  /** drop target props: a cluster card dragged over turns the target on, dropping it moves the cluster */
  const target = (gid: string) => ({
    onDragOver: (e: DragEvent) => { if (p.onMoveCluster && Array.from(e.dataTransfer.types).includes(CLUSTER_DRAG)) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; setDropOn(gid) } },
    onDragLeave: () => setDropOn(d => (d === gid ? null : d)),
    onDrop: (e: DragEvent) => {
      setDropOn(null)
      const c = p.clusters.find(x => x.id === e.dataTransfer.getData(CLUSTER_DRAG))
      if (!c || !p.onMoveCluster) return
      e.preventDefault()
      if ((c.group_id || '') !== gid) p.onMoveCluster(c, gid)
    },
  })
  return (
    <div className="hosts-page" aria-label={t('Kubernetes')}>
      <div className="page-head">
        <h2>{t('Kubernetes')}</h2>
        <span className="page-acts">
          <button className="ghost-btn" aria-label={t('New group')} onClick={p.onNewGroup}><IconFolderPlus size={14} /> {t('New group')}</button>
          <button className="split-btn" aria-label={t('Add cluster')} onClick={p.onNew}><IconPlus size={14} /> {t('Add cluster')}</button>
        </span>
      </div>
      <div className="notice">{t('A cluster here is reached with a kubeconfig file, with no SSH server in between. The kubeconfig is stored encrypted in your vault and is never shown again. Add only a kubeconfig from someone you trust: it can sign in with a program of this computer.')}</div>
      {p.clusters.length > 0 && <ListToolbar what={t('clusters')} query={q} onQuery={setQ} sort={sort} sortOptions={sorts} onSort={setSort} view={view} onView={setView} />}
      {!filtering && current && (
        <nav className="crumbs" aria-label={t('Group path')}>
          <button className={'link' + (dropOn === '' ? ' drop-over' : '')} onClick={() => setGroupId('')} {...target('')}>{t('All clusters')}</button>
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
                  <div className="addr">{tn(count(g), '{n} Cluster', '{n} Clusters')}</div>
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
      {(p.clusters.length > 0 || subgroups.length > 0) && <h3 className="sec-title">{t('Clusters')}</h3>}
      <div className={'cards' + (view === 'list' ? ' as-list' : '')} role="list" aria-label={t('Clusters')} data-view={view}>
        {visible.map(c => (
          <div key={c.id} className="card" role="listitem" draggable={!!p.onMoveCluster} onDragStart={e => { e.dataTransfer.setData(CLUSTER_DRAG, c.id); e.dataTransfer.effectAllowed = 'move' }} onDoubleClick={() => p.onOpen(c)}>
            <span className={'tile env-' + (c.env || 'none')} title={c.env || t('no environment')}><IconCloud size={20} /></span>
            <div className="meta">
              <div className="label">{c.label}{c.env === 'prod' && <> <span className="sp-pill bad">PROD</span></>}</div>
              <div className="addr mono">{c.server || '–'}</div>
              <div className="addr">{[c.context && t('context {name}', {name: c.context}), groupName(c.group_id)].filter(Boolean).join(' · ')}</div>
            </div>
            <div className="actions always">
              <button className="icon ghost" aria-label={t('Open {name}', {name: c.label})} title={t('Open in a tab')} onClick={() => p.onOpen(c)}><IconPlay size={14} /></button>
              <button className="icon ghost" aria-label={t('Edit {name}', {name: c.label})} onClick={() => p.onEdit(c)}><IconEdit size={14} /></button>
              <button className="icon ghost" aria-label={t('Replace the kubeconfig of {name}', {name: c.label})} title={t('Replace the kubeconfig')} onClick={() => p.onReplace(c)}><IconUpload size={14} /></button>
              <button className="icon ghost danger" aria-label={t('Delete {name}', {name: c.label})} onClick={() => p.onDelete(c)}><IconTrash size={14} /></button>
            </div>
          </div>
        ))}
      </div>
      {p.clusters.length > 0 && filtering && shown.length === 0 && <div className="empty-hint">{t('No cluster matches “{query}”.', {query: q.trim()})}</div>}
      {p.clusters.length > 0 && !filtering && visible.length === 0 && <div className="empty-hint">{current ? t('This group has no clusters yet.') : t('No clusters yet. Add the kubeconfig an administrator gave you to look at a cluster without any server in between.')}</div>}
      {p.clusters.length === 0 && <div className="empty-hint">{t('No clusters yet. Add the kubeconfig an administrator gave you to look at a cluster without any server in between.')}</div>}
    </div>
  )
}

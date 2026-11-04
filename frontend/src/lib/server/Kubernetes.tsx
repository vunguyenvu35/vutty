import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {Select} from '../ui/Select'
import {Err, useLoad} from './shared'
import {KubernetesConnect, remember, savedNamespaces, saveNamespaces, type K8sSession} from './KubernetesConnect'
import {K8sProvider, useRunner, type K8sApi, type K8sPage, type K8sView} from './KubernetesShared'
import {KubernetesOverview} from './KubernetesOverview'
import {KubernetesPods, PodDetail} from './KubernetesPods'
import {Workloads, DeploymentDetail} from './KubernetesWorkloads'
import {KubernetesNetwork} from './KubernetesNetwork'
import {KubernetesConfig} from './KubernetesConfig'
import {Drain, Nodes} from './KubernetesNodes'
import {Events} from './KubernetesEvents'
import {YamlPage} from './KubernetesYaml'
import type {Host} from '../types'

/** a namespace is a DNS label */
const NS_RE = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/

/**
 * The Kubernetes tab of the Panel: kubectl on the host, over the SSH connection the Panel already has. First the way in (connect
 * flow), then the cluster in seven views. See docs/KubernetesPanel.md.
 */
export function Kubernetes({sid, host, visible, clusterId, onReopen, onShell, onPodShell, onAccess, onLogs, onContext, onNamespace}: {
  sid: string; host: Host; visible: boolean
  /** a cluster opened with a kubeconfig (there is no SSH host): its id, for approving the login program of the kubeconfig */
  clusterId?: string
  /** asked after a login program was approved: the session is opened again so it knows (without it the check is just repeated) */
  onReopen?(): void
  onShell?(host: Host, command: string, title: string): void
  onPodShell?(hostId: string, title: string, prod: boolean): void
  onAccess?(): void
  onLogs(pod: {ns: string; name: string; container?: string; previous?: boolean}): void
  /** the cluster in use, for the Logs tab (which lists its pods) */
  onContext?(ctx: string): void
  /** the namespaces shown ('' is all of them; a list is separated by commas), for the Logs tab, which lists the pods of the same */
  onNamespace?(ns: string): void
}) {
  const [session, setSession] = useState<K8sSession | null>(null)
  const [again, setAgain] = useState<{detect: K8sSession['detect']; contexts: K8sSession['contexts']; ctx: string} | null>(null)
  const [round, setRound] = useState(0)
  const ready = useCallback((s: K8sSession) => setSession(s), [])
  useEffect(() => { onContext?.(session?.ctx ?? '') }, [session?.ctx, onContext])
  if (!session) return <KubernetesConnect key={round} sid={sid} host={host} again={again} clusterId={clusterId} onReopen={onReopen} onAccess={onAccess} onReady={ready} />
  const switchTo = (ctx: string) => { remember(host.id, ctx); setAgain({detect: session.detect, contexts: session.contexts, ctx}); setSession(null); setRound(r => r + 1) }
  return <Main sid={sid} host={host} visible={visible} session={session} onSwitch={switchTo} onShell={onShell} onPodShell={onPodShell} onAccess={onAccess} onLogs={onLogs} onNamespace={onNamespace} />
}

function Main({sid, host, visible, session, onSwitch, onShell, onPodShell, onAccess, onLogs, onNamespace}: {onNamespace?(ns: string): void; sid: string; host: Host; visible: boolean; session: K8sSession; onSwitch(ctx: string): void; onShell?(host: Host, command: string, title: string): void; onPodShell?(hostId: string, title: string, prod: boolean): void; onAccess?(): void; onLogs(pod: {ns: string; name: string; container?: string; previous?: boolean}): void}) {
  const be = useBackend()
  const t = useT()
  const ctx = session.ctx
  const direct = !!session.detect.direct
  const [ns, setNs] = useState('')
  useEffect(() => { onNamespace?.(ns) }, [ns, onNamespace])
  const [view, setView] = useState<K8sView>('overview')
  const [seed, setSeed] = useState('')
  const [page, setPage] = useState<K8sPage | null>(null)
  const [kept, setKept] = useState<Record<string, unknown>>({})
  const keep = useCallback((key: string, value: unknown) => setKept(m => ({...m, [key]: value})), [])
  const [rev, setRev] = useState(0)
  const [rights, setRights] = useState<Record<string, boolean> | null>(session.rights)
  const {busy, run} = useRunner(useCallback(() => setRev(r => r + 1), []))
  const loadNs = useCallback(() => be.server.k8sNamespaces(sid, ctx), [be, sid, ctx])
  const {data: spaces, error} = useLoad(loadNs, null)
  // a login that may not list the namespaces of the cluster (one project of a Rancher cluster, say) has no list to choose from:
  // the person adds the namespaces they work in, and they are remembered for this cluster
  const noList = /forbidden/i.test(error)
  const [extra, setExtra] = useState<string[]>(() => savedNamespaces(host.id, ctx))
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState('')
  const [draftErr, setDraftErr] = useState('')
  const picked = useRef(false)
  // "all namespaces" is a question about the whole cluster, which such a login may not ask: what it can see is the namespaces it
  // added, asked one after the other and put together (a list separated by commas)
  const allAdded = noList && extra.length > 1 ? extra.join(',') : ''
  useEffect(() => { // start in what was added: all of it, or the only one
    if (noList && !picked.current && ns === '' && extra.length) { picked.current = true; setNs(extra.length > 1 ? extra.join(',') : extra[0]) }
  }, [noList, extra, ns])
  const changeExtra = (list: string[]) => { setExtra(list); saveNamespaces(host.id, ctx, list) }
  const addNs = () => {
    const n = draft.trim()
    if (!NS_RE.test(n)) { setDraftErr(t('That is not a namespace name: lowercase letters, digits and -.')); return }
    const next = [n, ...extra.filter(x => x !== n)].slice(0, 30)
    changeExtra(next)
    picked.current = true
    setNs(noList && next.length > 1 ? next.join(',') : n); setPage(null); setAdding(false); setDraft(''); setDraftErr('')
  }
  const forget = (n: string) => {
    const rest = extra.filter(x => x !== n)
    changeExtra(rest)
    setNs(noList ? (rest.length > 1 ? rest.join(',') : rest[0] ?? '') : ''); setPage(null)
  }
  const listed = spaces?.names ?? []
  const nsOptions = [...new Set([...listed, ...extra, ...(ns && !ns.includes(',') ? [ns] : [])])].sort()
  // what this login may do depends on the namespace: asked again when it changes
  useEffect(() => {
    if (ns === '') { setRights(session.rights); return }
    if (ns.includes(',')) { setRights(null); return } // several namespaces: a button is tried, and the cluster says no if it may not
    let off = false
    be.server.k8sAccess(sid, ctx, ns).then(a => { if (!off) setRights(a.rights) }, () => { if (!off) setRights(null) })
    return () => { off = true }
  }, [be, sid, ctx, ns, session.rights])

  const api: K8sApi = useMemo(() => ({
    be, sid, host, direct, ctx, ns, rights, visible, rev, busy, run, onShell, onPodShell, onAccess, onLogs, kept, keep,
    can: id => !(direct && id === 'portforward') && rights?.[id] !== false,
    go: (v, f = '') => { setView(v); setSeed(f); setPage(null) },
    open: setPage,
    setNs: (n: string) => { setNs(n); setPage(null) },
  }), [be, sid, host, direct, ctx, ns, rights, visible, rev, busy, run, onShell, onPodShell, onAccess, onLogs, kept, keep])

  const views: [K8sView, string][] = [['overview', t('Overview')], ['pods', t('Pods')], ['workloads', t('Workloads')], ['network', t('Network')], ['config', t('Config & storage')], ['nodes', t('Nodes')], ['events', t('Events')]]
  const key = ctx + '|' + ns
  return (
    <K8sProvider value={api}>
      <div className="sp-view k8s">
        <div className="sp-bar k8s-ctx">
          {session.contexts.contexts.length > 1
            ? <Select className="sp-pick" label={t('Context')} value={ctx} onChange={v => v !== ctx && onSwitch(v)} options={session.contexts.contexts.map(c => ({value: c.name, label: c.name, hint: c.server}))} />
            : <span className="k8s-pick"><small>{t('Context')}</small><b>{ctx}</b></span>}
          <Select className="sp-pick" label={t('Namespace')} value={ns} onChange={v => { picked.current = true; setNs(v); setPage(null) }} searchable
            options={[...(noList ? (allAdded ? [{value: allAdded, label: t('All added namespaces ({n})', {n: extra.length})}] : []) : [{value: '', label: t('All namespaces')}]), ...nsOptions.map(n => ({value: n, label: n}))]} />
          {adding
            ? <span className="k8s-add-ns">
                <input autoFocus aria-label={t('Namespace name')} value={draft} placeholder={t('namespace')} spellCheck={false} onChange={e => { setDraft(e.target.value); setDraftErr('') }}
                  onKeyDown={e => { if (e.key === 'Enter') addNs(); if (e.key === 'Escape') { setAdding(false); setDraft(''); setDraftErr('') } }} />
                <button onClick={addNs}>{t('Add')}</button>
                <button onClick={() => { setAdding(false); setDraft(''); setDraftErr('') }}>{t('Cancel')}</button>
              </span>
            : <button className="k8s-add-ns-btn" aria-label={t('Add namespace')} title={t('Add a namespace by name')} onClick={() => setAdding(true)}>+</button>}
          {ns !== '' && !ns.includes(',') && extra.includes(ns) && !listed.includes(ns) && <button className="k8s-add-ns-btn" aria-label={t('Forget namespace {n}', {n: ns})} title={t('Forget this namespace')} onClick={() => forget(ns)}>×</button>}
          <span className="sp-note">{direct ? t('server {v} · direct', {v: session.check.server_version}) : t('server {v} · kubectl {k}', {v: session.check.server_version, k: session.detect.client_version})}</span>
        </div>
        {draftErr && <div className="sp-note k8s-hint" role="alert">{draftErr}</div>}
        {noList ? <div className="sp-note k8s-hint">{extra.length ? t('This login may not list the namespaces of the cluster: choose one you added, or add another with +.') : t('This login may not list the namespaces of the cluster, so there is no list to choose from. Add the namespace you work in with +: only the ones you add are shown.')}</div> : <Err error={error} />}
        <div className="sp-tabs k8s-sub" role="tablist" aria-label={t('Kubernetes views')}>
          {views.map(([id, label]) => <button key={id} role="tab" aria-selected={view === id} onClick={() => { setView(id); setSeed(''); setPage(null) }}>{label}</button>)}
        </div>
        <div className="k8s-body" key={key}>
          {page ? <Page page={page} /> : (
            <>
              {view === 'overview' && <KubernetesOverview />}
              {view === 'pods' && <KubernetesPods seed={seed} />}
              {view === 'workloads' && <Workloads />}
              {view === 'network' && <KubernetesNetwork />}
              {view === 'config' && <KubernetesConfig />}
              {view === 'nodes' && <Nodes />}
              {view === 'events' && <Events />}
            </>
          )}
        </div>
      </div>
    </K8sProvider>
  )
}

function Page({page}: {page: K8sPage}) {
  if (page.k === 'pod') return <PodDetail ns={page.ns} name={page.name} />
  if (page.k === 'deploy') return <DeploymentDetail ns={page.ns} name={page.name} />
  if (page.k === 'yaml') return <YamlPage page={page} />
  return <Drain node={page.node} />
}

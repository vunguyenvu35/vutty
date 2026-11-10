import {useCallback, useEffect, useRef, useState} from 'react'
import {createPortal} from 'react-dom'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {usePref} from '../ui/ListTools'
import {Select} from '../ui/Select'
import {IconChevron} from '../ui/Icons'
import {Card, Err, Loading, Toolbar, FilterInput} from './shared'
import {ActBtn, age, Age, Crumb, lockText, matches, Pill, useK8s, useKept, useKLoad, useSort, useMenuAnchor} from './KubernetesShared'
import {CopyMenu} from './KubernetesCopy'
import {DebugButton} from './KubernetesDebug'
import type {K8sPod, K8sPodDetail} from '../types'

type PodSort = 'name' | 'status' | 'ready' | 'restarts' | 'node' | 'age'

/** How the pods are put in groups, as in Rancher: by the node they run on (its default), by namespace, by the workload that owns them, or not at all. */
type PodGroup = 'node' | 'namespace' | 'workload' | 'none'
const POD_GROUPS: PodGroup[] = ['node', 'namespace', 'workload', 'none']

/** the workload a pod belongs to, from its owner text ("ReplicaSet web-6b7d9 ← Deployment web" is Deployment web) */
const workloadOf = (owner: string) => (owner.includes('← ') ? owner.split('← ').pop()! : owner).trim()

/** Delete a pod: asks first, names the pod, namespace and cluster, and what happens next */
export function useDeletePod(after?: () => void) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  return async (p: K8sPod) => {
    if (!(await confirm(t('Delete the pod {name} in {ns} on {ctx}? A controller that owns it starts a new one; a pod without one is gone for good.', {name: p.name, ns: p.namespace, ctx: k.ctx}), {confirmLabel: t('Delete'), danger: true}))) return
    if (await k.run('delete:' + p.namespace + '/' + p.name, () => k.be.server.k8sDeletePod(k.sid, k.ctx, p.namespace, p.name), t('Deleting {name}', {name: p.name}))) after?.()
  }
}

/**
 * Opens a shell in a container of a pod, as the cluster allows: a cluster opened with a kubeconfig has no host, so its shell is a terminal
 * tab that talks to the cluster's API; a host types the kubectl line into an SSH tab. The Shell of a pod and the Execute shell of a
 * workload both go through here.
 */
export function useStartShell() {
  const k = useK8s()
  return (pod: {namespace: string; name: string}, container: string) => {
    void k.run('shell:' + pod.namespace + '/' + pod.name, async () => {
      if (k.direct) { // a cluster opened with a kubeconfig has no host: the shell is a terminal tab that talks to the cluster's API
        const sh = await k.be.server.k8sShellSession(k.sid, k.ctx, pod.namespace, pod.name, container)
        k.onPodShell?.(sh.host_id, sh.title, k.host.env === 'prod')
        return
      }
      const cmd = await k.be.server.k8sShellCommand(k.sid, k.ctx, pod.namespace, pod.name, container)
      k.onShell?.(k.host, cmd, `${k.host.label} · ${pod.name}`)
    })
  }
}

/** whether a shell can be opened from here at all (a terminal tab must be there to take it) */
export const useCanOpenShell = () => { const k = useK8s(); return !!(k.direct ? k.onPodShell : k.onShell) }

/** Shell: one button; a pod with several containers asks which one first (a small menu); init containers have nothing to enter */
export function ShellButton({pod}: {pod: K8sPod}) {
  const k = useK8s()
  const t = useT()
  const {btn, menu, at, setAt, toggle, style: menuStyle} = useMenuAnchor()
  const id = 'shell:' + pod.namespace + '/' + pod.name
  const own = pod.containers.filter(c => !c.init)
  const usable = own.filter(c => c.running)
  useEffect(() => {
    if (!at) return
    const away = (e: MouseEvent) => { if (!(e.target as Element).closest?.('.k8s-menu') && !btn.current?.contains(e.target as Node)) setAt(null) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setAt(null) }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc) }
  }, [at])
  const startShell = useStartShell()
  const start = (container: string) => { setAt(null); startShell(pod, container) }
  const click = () => {
    if (own.length <= 1) { start(usable[0]?.name ?? ''); return }
    toggle()
  }
  const none = usable.length === 0
  return (
    <>
      <button ref={btn} aria-haspopup={own.length > 1 ? 'menu' : undefined} aria-expanded={own.length > 1 ? !!at : undefined} aria-busy={k.busy(id)}
        disabled={none || !k.can('exec') || !(k.direct ? k.onPodShell : k.onShell) || k.busy(id)}
        title={!k.can('exec') ? t('This login may not open a shell in pods') : none ? t('Needs a running container: this pod has none') : !(k.direct ? k.onPodShell : k.onShell) ? t('A terminal cannot be opened from here') : undefined}
        className="k8s-shell" onClick={click}>{own.length > 1 ? t('Shell ▾') : t('Shell')}</button>
      {at && createPortal(
        <div ref={menu} className="k8s-menu" role="menu" aria-label={t('Open a shell in {name}', {name: pod.name})} style={menuStyle}>
          <h4>{t('Containers of {name}', {name: pod.name})}</h4>
          {pod.containers.map(c => (
            <button key={c.name} role="menuitem" className="k8s-mi" disabled={c.init || !c.running} onClick={() => start(c.name)}>
              <b>{c.name}</b>
              <small>{c.init ? t('{image} · init container, nothing to enter', {image: c.image}) : `${c.image} · ${c.running ? t('running') : t('not running')}`}</small>
            </button>
          ))}
          <div className="sp-note">{k.direct ? t('Opens a new terminal tab with a shell in the pod, over the cluster API. It tries bash, then sh.') : t('Opens a new terminal tab on {host} and runs kubectl exec -it. It tries bash, then sh.', {host: k.host.label})}</div>
        </div>, document.body)}
    </>
  )
}

/** Screen 6: the pods, with a filter, a "Problems only" switch and sortable columns. Asked again every 5 s while shown. */
export function KubernetesPods({seed}: {seed: string}) {
  const k = useK8s()
  const t = useT()
  const [filter, setFilter] = useKept('pods.filter', '')
  const [problems, setProblems] = useKept('pods.problems', false)
  const [status, setStatus] = useKept('pods.status', '') // '' = every status
  useEffect(() => { if (seed) setFilter(seed) }, [seed]) // eslint-disable-line react-hooks/exhaustive-deps -- coming from the Overview with a pod to look at
  const tn = useTn()
  const [groupBy, setGroupBy] = usePref<PodGroup>('vutty.k8s.pods.group', POD_GROUPS, 'node')
  const [folded, setFolded] = useKept<string[]>('pods.folded', []) // groups that were closed, by their key
  const load = useCallback(() => k.be.server.k8sPods(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sPod[]>(load, true)
  const {th, apply} = useSort<PodSort>(['name', 'status', 'node'])
  const del = useDeletePod()
  const statuses = [...new Set((data ?? []).map(p => p.status))].sort((a, b) => a.localeCompare(b))
  const list = (data ?? []).filter(p => matches(filter, p.name, p.namespace, p.node, p.status) && (!status || p.status === status) && (!problems || p.tone === 'bad' || p.tone === 'warn'))
  const shown = apply(list, (p, key) => key === 'ready' ? Number(p.ready.split('/')[0]) : key === 'age' ? p.age_sec : key === 'restarts' ? p.restarts : p[key])
  // the groups, by name (what has no node, no owner, ... last); the order of the pods inside each is the order of the columns
  const keyOf = (p: K8sPod) => groupBy === 'node' ? p.node : groupBy === 'namespace' ? p.namespace : groupBy === 'workload' ? workloadOf(p.owner) : ''
  const groups = (() => {
    if (groupBy === 'none') return [{key: '', pods: shown}]
    const by = new Map<string, K8sPod[]>()
    for (const p of shown) by.set(keyOf(p), [...(by.get(keyOf(p)) ?? []), p])
    return [...by.entries()].sort(([a], [b]) => (a === '' ? 1 : b === '' ? -1 : a.localeCompare(b))).map(([key, pods]) => ({key, pods}))
  })()
  const title = (key: string) => key === '' ? (groupBy === 'node' ? t('No node yet') : groupBy === 'workload' ? t('No workload') : '')
    : groupBy === 'node' ? t('Node: {name}', {name: key}) : groupBy === 'namespace' ? t('Namespace: {name}', {name: key}) : key
  const fold = (key: string) => setFolded(folded.includes(key) ? folded.filter(x => x !== key) : [...folded, key])
  const row = (p: K8sPod) => (
    <tr key={p.namespace + '/' + p.name}>
      <td className="k8s-name"><button className="sp-link" onClick={() => k.open({k: 'pod', ns: p.namespace, name: p.name})}><b>{p.name}</b></button><small>{p.namespace}</small>{p.why && <span className="k8s-why">{p.why}</span>}</td>
      <td><Pill tone={p.tone}>{p.status}</Pill></td>
      <td className="n m">{p.ready}</td><td className="n m">{p.restarts}</td><td className="m">{p.node || '–'}</td><td className="n"><Age sec={p.age_sec} /></td>
      <td><div className="sp-acts">
        <ShellButton pod={p} />
        <ActBtn right="logs" onClick={() => k.onLogs({ns: p.namespace, name: p.name})}>{t('Logs')}</ActBtn>
        <ActBtn id={'delete:' + p.namespace + '/' + p.name} right="delete_pod" className="danger" label={t('Delete pod {name}', {name: p.name})} onClick={() => void del(p)}>{t('Delete')}</ActBtn>
      </div></td>
    </tr>
  )
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading pods…')} /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter pods')} placeholder={t('Filter pods')} value={filter} onChange={setFilter} />
        <button aria-pressed={problems} className={problems ? 'primary' : ''} onClick={() => setProblems(!problems)}>{t('Problems only')}</button>
        <Select className="sp-pick" label={t('Status')} value={statuses.includes(status) ? status : ''} onChange={setStatus}
          options={[{value: '', label: t('All statuses')}, ...statuses.map(x => ({value: x, label: x, hint: String((data ?? []).filter(p => p.status === x).length)}))]} />
        <Select className="sp-pick" label={t('Group by')} value={groupBy} onChange={v => setGroupBy(v as PodGroup)}
          options={[{value: 'node', label: t('Group by node')}, {value: 'namespace', label: t('Group by namespace')}, {value: 'workload', label: t('Group by workload')}, {value: 'none', label: t('No groups')}]} />
        <span className="sp-note" role="status">{data ? t('{shown} of {total} pods', {shown: shown.length, total: data.length}) : ''}</span>
      </Toolbar>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr>{th('name', t('Pod'))}{th('status', t('Status'))}{th('ready', t('Ready'), 'n')}{th('restarts', t('Restarts'), 'n')}{th('node', t('Node'))}{th('age', t('Age'), 'n')}<th /></tr></thead>
        {groups.map(g => (
          <tbody key={g.key || '·'}>
            {groupBy !== 'none' && (
              <tr className="k8s-group" onClick={() => fold(g.key)}>{/* the whole row opens and closes the group; the button below is for the keyboard and the screen reader, and its click reaches this row */}
                <td colSpan={7}><button className="sp-link" aria-expanded={!folded.includes(g.key)} aria-label={title(g.key)}>
                  <span className={'k8s-chev' + (folded.includes(g.key) ? '' : ' open')}><IconChevron size={12} /></span> <span>{title(g.key)}</span> <small>{tn(g.pods.length, '{n} pod', '{n} pods')}</small></button></td>
              </tr>
            )}
            {!folded.includes(g.key) && g.pods.map(row)}
          </tbody>
        ))}
      </table>{data && shown.length === 0 && <div className="sp-empty">{t('No pod matches.')}</div>}</div>
      <div className="sp-note">{t('Click a pod name for its detail. Delete asks first; the Deployment that owns the pod starts a new one.')}</div>
    </div>
  )
}

const stateTone = (s: string) => (/^Running|Completed/.test(s) ? 'ok' : /Waiting|Terminated|Error/.test(s) ? 'bad' : '')

/** Screen 8: one pod, its containers and what happened to it */
export function PodDetail({ns, name}: {ns: string; name: string}) {
  const k = useK8s()
  const t = useT()
  const load = useCallback(() => k.be.server.k8sPod(k.sid, k.ctx, ns, name), [k.be, k.sid, k.ctx, ns, name])
  const {data: d, error, busy, reload} = useKLoad<K8sPodDetail>(load, true)
  const del = useDeletePod(() => k.open(null))
  const back = <Crumb path={[t('Pods'), ns, name]} onBack={() => k.open(null)} />
  if (!d) return <div className="sp-view"><Toolbar busy={busy} onReload={reload}>{back}</Toolbar><Err error={error} />{!error && <Loading kind="detail" />}</div>
  const p = d.pod
  const ownerOf = p.owner || '–'
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>{back}</Toolbar>
      <Err error={error} />
      <div className="k8s-title">
        <h2>{p.name}</h2><Pill tone={p.tone}>{p.status}</Pill>
        <span className="sp-acts k8s-grow">
          <ActBtn right="logs" onClick={() => k.onLogs({ns, name})}>{t('Logs')}</ActBtn>
          <button onClick={() => k.open({k: 'yaml', kind: 'pod', ns, name, label: t('Pod {name}', {name}), back: {k: 'pod', ns, name}})}>{t('YAML')}</button>
          <ShellButton pod={p} />
          <DebugButton pod={p} />
          <CopyMenu target={{kind: 'pod', ns, name}} />
          <ActBtn id={'delete:' + ns + '/' + name} right="delete_pod" className="danger" onClick={() => void del(p)}>{t('Delete')}</ActBtn>
        </span>
      </div>
      <div className="sp-grid sp-grid-2">
        <Card title={t('Summary')}>
          <table className="sp-kv"><tbody>
            <tr><td>{t('Namespace')}</td><td className="m">{p.namespace}</td></tr>
            <tr><td>{t('Node')}</td><td className="m">{p.node || '–'}</td></tr>
            <tr><td>{t('Pod IP')}</td><td className="m">{d.ip || '–'}</td></tr>
            <tr><td>{t('Owner')}</td><td className="m">{ownerOf}</td></tr>
            <tr><td>{t('QoS class')}</td><td className="m">{d.qos || '–'}</td></tr>
            <tr><td>{t('Service account')}</td><td className="m">{d.service_account || '–'}</td></tr>
            <tr><td>{t('Started')}</td><td>{t('{time} ago', {time: age(p.age_sec)})}</td></tr>
          </tbody></table>
          <div className="sp-chips">{d.labels.map(l => <span key={l} className="sp-chip">{l}</span>)}</div>
        </Card>
        <Card title={t('Conditions')}>
          <table className="sp-kv"><tbody>{d.conditions.map(c => <tr key={c.type}><td>{c.type}</td><td><Pill tone={c.status === 'True' ? 'ok' : 'bad'}>{c.status}</Pill></td></tr>)}</tbody></table>
          <h3>{t('Volumes')}</h3>
          <table className="sp-kv"><tbody>{d.volumes.map(v => <tr key={v.name}><td>{v.name}</td><td className="m">{v.source}</td></tr>)}</tbody></table>
          {d.volumes.length === 0 && <div className="sp-note">{t('No volumes.')}</div>}
        </Card>
      </div>
      <Card title={t('Containers')}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Name')}</th><th>{t('Image')}</th><th>{t('State')}</th><th className="n">{t('Restarts')}</th><th>{t('CPU / memory (request → limit)')}</th><th>{t('Probes')}</th><th /></tr></thead>
          <tbody>{p.containers.map(c => (
            <tr key={c.name}>
              <td className="m">{c.name}{c.init && <> <Pill>{t('init')}</Pill></>}</td><td className="m">{c.image}</td><td><Pill tone={stateTone(c.state)}>{c.state}</Pill></td><td className="n m">{c.restarts}</td><td className="m">{c.resources || '–'}</td><td className="m">{c.probes || '–'}</td>
              <td><div className="sp-acts">
                <ActBtn right="logs" label={t('Logs of {name}', {name: c.name})} onClick={() => k.onLogs({ns, name, container: c.name})}>{t('Logs')}</ActBtn>
                <ActBtn right="logs" label={t('Previous logs of {name}', {name: c.name})} disabled={c.restarts === 0} title={c.restarts === 0 ? t('This container has not restarted') : undefined} onClick={() => k.onLogs({ns, name, container: c.name, previous: true})}>{t('Previous logs')}</ActBtn>
              </div></td>
            </tr>))}</tbody>
        </table></div>
      </Card>
      <Card title={t('Events for this pod')}>
        {d.events.length === 0 && <div className="sp-note">{t('No events.')}</div>}
        {d.events.map((e, i) => <div key={i} className="k8s-ev"><time><Age sec={e.age_sec} /></time><span><Pill tone={e.type === 'Warning' ? 'warn' : ''}>{e.reason}</Pill> {e.message}</span></div>)}
      </Card>
      <div className="sp-note">{t('Environment variables are not shown here: they can hold secrets. Open YAML to see which ones the pod declares; values that come from a Secret stay hidden.')}</div>
    </div>
  )
}

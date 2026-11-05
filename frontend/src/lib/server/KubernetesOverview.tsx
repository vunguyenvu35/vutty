import {useCallback} from 'react'
import {useT} from '../i18n'
import {Err, Loading, RefreshButton} from './shared'
import {useK8s, useKLoad, type K8sView} from './KubernetesShared'
import type {K8sOverview} from '../types'

const WHERE: Record<K8sOverview['attention'][0]['kind'], K8sView> = {pod: 'pods', node: 'nodes', deployment: 'workloads', ingress: 'network', pvc: 'config'}

/** Screen 5: how the cluster is, and what needs attention first. Asked again every 5 s while the tab is shown. */
export function KubernetesOverview() {
  const k = useK8s()
  const t = useT()
  const load = useCallback(() => k.be.server.k8sOverview(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data: o, error, busy, reload} = useKLoad<K8sOverview>(load, true)
  if (!o) return <><Err error={error} />{!error && <Loading kind="overview" />}</>
  const open = (a: K8sOverview['attention'][0]) => {
    if (k.ns && a.namespace && !k.ns.split(',').includes(a.namespace)) k.setNs('')
    k.go(WHERE[a.kind], a.kind === 'pod' ? a.name : '')
  }
  const c = o.capacity
  const n2 = (v: number) => String(Math.round(v * 100) / 100)
  const pct = (used: number, all: number) => (all > 0 ? Math.round(used / all * 10000) / 100 : 0)
  /** one line of a Capacity box: what it is, how much of how much, and the bar (green, yellow from 75 %, red from 90 %) */
  const meter = (label: string, amount: string, value: number, name: string) => (
    <div className="k8s-meter">
      <div className="k8s-meter-row"><span>{label} <small>{amount}</small></span><b>{value.toFixed(2)}%</b></div>
      <div className={'sp-track ' + (value >= 90 ? 'bad' : value >= 75 ? 'warn' : 'ok')} role="meter" aria-label={name} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.min(100, value)}><i style={{width: Math.min(100, value) + '%'}} /></div>
    </div>
  )
  const track = (v: number | null) => v === null ? null : <div className={'sp-track ' + (v >= 90 ? 'bad' : v >= 75 ? 'warn' : 'ok')} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={v}><i style={{width: v + '%'}} /></div>
  return (
    <div className="sp-view">
      <Err error={error} />
      <div className="sp-grid">
        <div className="sp-card"><h3>{t('Nodes')}</h3>
          {o.nodes_hidden ? <>
            <div className="k8s-seen-head"><span className="sp-big">{(o.nodes_seen ?? []).length}</span><span className="sp-note">{t('nodes the pods run on')}</span></div>
            <div className="k8s-seen-hint">{t('This login may not read the nodes of the cluster.')}</div>
            <ul className="k8s-seen">
              {(o.nodes_seen ?? []).map(n => (
                <li key={n.name}><button className="k8s-seen-row" title={n.name} onClick={() => k.go('pods', n.name)}>
                  <span className="k8s-seen-name">{n.name.split('.')[0]}</span>
                  <span className="k8s-seen-bar"><i style={{width: Math.max(4, n.pods / Math.max(...(o.nodes_seen ?? []).map(x => x.pods), 1) * 100) + '%'}} /></span>
                  <b>{n.pods}</b>
                </button></li>
              ))}
            </ul>
          </> : <><div className="sp-big">{o.nodes.ready} / {o.nodes.total}</div><div className="sp-note">{t('ready · {n} cordoned', {n: o.nodes.cordoned})}</div></>}
        </div>
        <div className="sp-card"><h3>{t('Pods')}</h3><div className="sp-big">{o.pods.running} / {o.pods.total}</div><div className="sp-note">{t('running · {pending} pending · {failing} failing', {pending: o.pods.pending, failing: o.pods.failing})}</div></div>
      </div>
      <h3 className="sec-title">{t('Capacity')}</h3>
      {c.known ? (
        <div className="sp-grid">
          <div className="sp-card"><h3>{t('Pods')}</h3>{meter(t('Used'), `${c.pods_used} / ${c.pods_allocatable}`, pct(c.pods_used, c.pods_allocatable), t('Pods used {used} of {all}', {used: c.pods_used, all: c.pods_allocatable}))}</div>
          <div className="sp-card"><h3>{t('CPU')}</h3>
            {meter(t('Reserved'), t('{used} / {all} cores', {used: n2(c.cpu_reserved), all: n2(c.cpu_allocatable)}), pct(c.cpu_reserved, c.cpu_allocatable), t('CPU reserved'))}
            {c.cpu_used !== null && meter(t('Used'), t('{used} / {all} cores', {used: n2(c.cpu_used), all: n2(c.cpu_allocatable)}), pct(c.cpu_used, c.cpu_allocatable), t('CPU used'))}
          </div>
          <div className="sp-card"><h3>{t('Memory')}</h3>
            {meter(t('Reserved'), `${n2(c.mem_reserved_gib)} / ${n2(c.mem_allocatable_gib)} GiB`, pct(c.mem_reserved_gib, c.mem_allocatable_gib), t('Memory reserved'))}
            {c.mem_used_gib !== null && meter(t('Used'), `${n2(c.mem_used_gib)} / ${n2(c.mem_allocatable_gib)} GiB`, pct(c.mem_used_gib, c.mem_allocatable_gib), t('Memory used'))}
          </div>
        </div>
      ) : <div className="sp-note">{o.nodes_hidden ? t('Needs the nodes: this login may not read them.') : t('The nodes did not say what they can hold.')}</div>}
      {c.known && c.partial && <div className="sp-note">{t('Pods and reserved amounts are those of the namespaces shown; the capacity is the whole cluster’s.')}</div>}
      {c.known && c.cpu_used === null && <div className="sp-note">{t('What is used now needs a metrics-server, which this cluster does not have (or this login may not read).')}</div>}
      <div className="sp-grid sp-grid-2">
        <div className="sp-card">
          <div className="sp-card-head"><h3>{t('Needs attention')}</h3><RefreshButton busy={busy} onReload={reload} /></div>
          {o.attention.length === 0 && <div className="sp-note">{t('Nothing needs attention.')}</div>}
          {o.attention.map((a, i) => (
            <div key={i} className="sp-attn">
              <span><span className={'sp-pill ' + a.severity}>{a.severity === 'bad' ? t('Problem') : t('Warning')}</span> {a.text}</span>
              <button onClick={() => open(a)}>{t('Open')}</button>
            </div>
          ))}
        </div>
        <div className="sp-card">
          <h3>{t('Namespaces')}</h3>
          <div className="sp-table"><table>
            <thead><tr><th>{t('Name')}</th><th className="n">{t('Pods')}</th><th className="n">{t('Not ready')}</th><th className="n">{t('Restarts')}</th></tr></thead>
            <tbody>{o.namespaces.map(n => <tr key={n.name}><td className="m">{n.name}</td><td className="n">{n.pods}</td><td className="n">{n.not_ready}</td><td className="n">{n.restarts}</td></tr>)}</tbody>
          </table></div>
        </div>
      </div>
    </div>
  )
}

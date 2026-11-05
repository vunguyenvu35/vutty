import {useCallback, useEffect, useRef, useState} from 'react'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Err, Loading, Toolbar} from './shared'
import {ActBtn, age, Crumb, Meter, Pill, useK8s, useKLoad, useSort} from './KubernetesShared'
import {CopyMenu} from './KubernetesCopy'
import type {K8sDrainOpts, K8sDrainPlan, K8sNode} from '../types'

type NodeSort = 'name' | 'status' | 'role' | 'cpu' | 'mem' | 'pods' | 'age'

/** Cordon or uncordon: asks first, names the node and the cluster */
function useCordon() {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  return async (node: string, on: boolean) => {
    const msg = on ? t('Cordon the node {name} on {ctx}? New pods stop landing on it; the pods already there keep running.', {name: node, ctx: k.ctx}) : t('Uncordon the node {name} on {ctx}? New pods may land on it again.', {name: node, ctx: k.ctx})
    if (!(await confirm(msg, {confirmLabel: on ? t('Cordon') : t('Uncordon')}))) return false
    return k.run('cordon:' + node, () => k.be.server.k8sCordon(k.sid, k.ctx, node, on), on ? t('{name} is cordoned', {name: node}) : t('{name} is open for new pods', {name: node}))
  }
}

/** Screen 15 */
export function Nodes() {
  const k = useK8s()
  const t = useT()
  const cordon = useCordon()
  const load = useCallback(() => k.be.server.k8sNodes(k.sid, k.ctx), [k.be, k.sid, k.ctx])
  const {data, error, busy, reload} = useKLoad<K8sNode[]>(load, false)
  const {th, apply} = useSort<NodeSort>(['name', 'status', 'role'])
  const rows = apply(data ?? [], (n, key) => key === 'name' ? n.name : key === 'status' ? n.status : key === 'role' ? n.roles : key === 'cpu' ? (n.cpu_pct ?? -1) : key === 'mem' ? (n.mem_pct ?? -1) : key === 'pods' ? n.pods : n.age_sec)
  const noMetrics = !!data && data.length > 0 && data.every(n => n.cpu_pct === null && n.mem_pct === null)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading nodes…')} /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}><span className="sp-note">{noMetrics ? t('No metrics: metrics-server is not installed.') : t('CPU and memory come from metrics-server.')}</span></Toolbar>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr>{th('name', t('Node'))}{th('status', t('Status'))}{th('role', t('Role'))}{th('cpu', t('CPU'))}{th('mem', t('Memory'))}{th('pods', t('Pods'), 'n')}{th('age', t('Age'), 'n')}<th /></tr></thead>
        <tbody>{rows.map(n => (
          <tr key={n.name}>
            <td className="k8s-name"><b>{n.name}</b><small>{n.ip} · {n.version}</small></td>
            <td><Pill tone={n.status === 'Ready' ? 'ok' : n.status === 'NotReady' ? 'bad' : 'warn'}>{n.status === 'NotReady' ? t('NotReady') : n.status === 'Ready' ? t('Ready') : t('Unknown')}</Pill>{n.unschedulable && <> <Pill tone="warn">{t('Unschedulable')}</Pill></>}</td>
            <td>{n.roles || '–'}</td>
            <td><Meter value={n.cpu_pct} label={t('CPU of {name}', {name: n.name})} /></td><td><Meter value={n.mem_pct} label={t('Memory of {name}', {name: n.name})} /></td>
            <td className="n m">{n.pods} / {n.pods_max}</td><td className="n">{age(n.age_sec)}</td>
            <td><div className="sp-acts">
              <ActBtn id={'cordon:' + n.name} right="cordon" className={n.unschedulable ? 'primary' : ''} label={(n.unschedulable ? t('Uncordon {name}', {name: n.name}) : t('Cordon {name}', {name: n.name}))} onClick={() => void cordon(n.name, !n.unschedulable)}>{n.unschedulable ? t('Uncordon') : t('Cordon')}</ActBtn>
              <CopyMenu target={{kind: 'node', name: n.name, ip: n.ip || undefined, jump: k.direct ? undefined : `${k.host.username}@${k.host.hostname}`}} />
              <ActBtn id={'drain:' + n.name} right="drain" label={t('Drain {name}', {name: n.name})} onClick={() => k.open({k: 'drain', node: n.name})}>{t('Drain')}</ActBtn>
            </div></td>
          </tr>))}</tbody>
      </table>{data && data.length === 0 && <div className="sp-empty">{t('No nodes.')}</div>}</div>
      <div className="sp-note">{t('Cordon stops new pods from landing on a node; the pods already there keep running.')}</div>
    </div>
  )
}

/** Screen 16: what a drain would do (a dry run on the server), the options, and Drain or Cordon only */
export function Drain({node}: {node: string}) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const cordon = useCordon()
  const [ignoreDs, setIgnoreDs] = useState(true)
  const [emptydir, setEmptydir] = useState(false)
  const [force, setForce] = useState(false)
  const [grace, setGrace] = useState('30')
  const [wait, setWait] = useState('300')
  const num = (s: string) => (/^\d{1,5}$/.test(s) ? Number(s) : NaN)
  const opts: K8sDrainOpts = {ignore_daemonsets: ignoreDs, delete_emptydir: emptydir, force, grace_sec: num(grace), timeout_sec: num(wait)}
  const planOpts = {...opts, grace_sec: 30, timeout_sec: 300}
  const load = useCallback(() => k.be.server.k8sDrainPlan(k.sid, k.ctx, node, planOpts), [k.be, k.sid, k.ctx, node, ignoreDs, emptydir, force]) // eslint-disable-line react-hooks/exhaustive-deps
  const {data: plan, error, busy, reload} = useKLoad<K8sDrainPlan>(load, false)
  const first = useRef(true)
  useEffect(() => { if (first.current) { first.current = false; return } reload() }, [ignoreDs, emptydir, force]) // eslint-disable-line react-hooks/exhaustive-deps
  const back = <Crumb path={[t('Nodes'), node, t('Drain')]} onBack={() => k.open(null)} />
  const numbersOk = opts.grace_sec <= 3600 && opts.timeout_sec >= 1 && opts.timeout_sec <= 3600 // NaN fails every test: what is not a number is refused
  const blockers = plan ? [plan.daemonsets > 0 && !ignoreDs, plan.emptydir.length > 0 && !emptydir, plan.bare.length > 0 && !force].filter(Boolean).length : 1
  const drain = async () => {
    if (!plan) return
    if (!(await confirm(t('Drain the node {name} on {ctx}? It is cordoned first, then {n} pods are moved to other nodes. It can take until {sec} s.', {name: node, ctx: k.ctx, n: plan.moved, sec: opts.timeout_sec}), {confirmLabel: t('Drain'), danger: true}))) return
    if (await k.run('drain:' + node, () => k.be.server.k8sDrain(k.sid, k.ctx, node, opts), t('{name} is drained', {name: node}))) k.open(null)
  }
  const cordonOnly = async () => { if (await cordon(node, true)) k.open(null) }
  const opt = (checked: boolean, set: (v: boolean) => void, title: string, note: string) => (
    <label className="k8s-optrow"><input type="checkbox" checked={checked} onChange={e => set(e.target.checked)} /><span>{title}<br /><span className="sp-note">{note}</span></span></label>
  )
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>{back}</Toolbar>
      <Err error={error} />
      <h2 className="k8s-h2">{t('Drain {name}', {name: node})}</h2>
      <div className="sp-note">{t('Checked before anything runs, with a dry run: nothing has been changed yet. Draining first cordons the node, then moves its pods elsewhere.')}</div>
      <div className="sp-grid sp-grid-2">
        <section className="sp-card sp-wide">
          <h3>{plan ? t('What happens to its pods') : t('Checking …')}</h3>
          {plan && <div className="sp-table"><table>
            <thead><tr><th>{t('Pods')}</th><th>{t('Result')}</th><th>{t('Why')}</th></tr></thead>
            <tbody>
              <tr><td className="m">{t('{n} owned by controllers', {n: plan.moved})}</td><td><Pill tone="ok">{t('Moved')}</Pill></td><td>{t('A controller starts them again on another node')}</td></tr>
              {plan.daemonsets > 0 && <tr><td className="m">{t('{n} owned by a DaemonSet', {n: plan.daemonsets})}</td><td>{ignoreDs ? <Pill>{t('Left alone')}</Pill> : <Pill tone="bad">{t('Blocks')}</Pill>}</td><td>{ignoreDs ? t('They belong on every node (ignored)') : t('The drain stops at the first one')}</td></tr>}
              {plan.emptydir.length > 0 && <tr><td className="m">{t('{n} use emptyDir', {n: plan.emptydir.length})}<div className="sp-note">{plan.emptydir.join(', ')}</div></td><td>{emptydir ? <Pill tone="warn">{t('Removed')}</Pill> : <Pill tone="warn">{t('Needs your say')}</Pill>}</td><td>{t('Their scratch data is lost when they move')}</td></tr>}
              {plan.bare.length > 0 && <tr><td className="m">{t('{n} have no controller', {n: plan.bare.length})}<div className="sp-note">{plan.bare.join(', ')}</div></td><td>{force ? <Pill tone="bad">{t('Deleted')}</Pill> : <Pill tone="bad">{t('Blocks')}</Pill>}</td><td>{t('Nothing would start it again')}</td></tr>}
            </tbody>
          </table></div>}
          {plan?.pdb.map(b => <div key={b.namespace + '/' + b.name} className="k8s-warnbox" role="alert">{t('The PodDisruptionBudget of {name} in {ns}: {detail}. The drain will wait for it rather than break it, so it can take until the timeout.', {name: b.name, ns: b.namespace, detail: b.detail})}</div>)}
        </section>
        <section className="sp-card sp-wide">
          <h3>{t('Options')}</h3>
          {opt(ignoreDs, setIgnoreDs, t('Ignore DaemonSet pods'), t('needed, or the drain stops at the first one'))}
          {opt(emptydir, setEmptydir, t('Also remove pods that use emptyDir'), t('their local data is lost'))}
          {opt(force, setForce, t('Delete pods that have no controller'), t('they are gone for good'))}
          <div className="k8s-two">
            <label className="field"><span>{t('Grace period (s)')}</span><input inputMode="numeric" value={grace} onChange={e => setGrace(e.target.value)} /></label>
            <label className="field"><span>{t('Give up after (s)')}</span><input inputMode="numeric" value={wait} onChange={e => setWait(e.target.value)} /></label>
          </div>
          {blockers > 0 && <div className="sp-note">{t('Blocked by a pod that needs your say: tick its option, or move it yourself, to go on.')}</div>}
        </section>
      </div>
      <div className="k8s-foot">
        <button onClick={() => k.open(null)}>{t('Cancel')}</button>
        <ActBtn id={'cordon:' + node} right="cordon" onClick={() => void cordonOnly()}>{t('Cordon only')}</ActBtn>
        <ActBtn id={'drain:' + node} right="drain" className="danger" disabled={!plan || blockers > 0 || !numbersOk} onClick={() => void drain()}>{t('Drain {name}', {name: node})}</ActBtn>
      </div>
    </div>
  )
}

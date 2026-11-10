import {useCallback, useState} from 'react'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Modal} from '../ui/Modal'
import {useToast} from '../ui/Toast'
import {Card, Err, Loading, Toolbar, FilterInput} from './shared'
import {ActBtn, age, Crumb, matches, Pill, useK8s, useKept, useKLoad, useSort} from './KubernetesShared'
import {WorkloadMenu} from './KubernetesWorkloadMenu'
import {CopyMenu} from './KubernetesCopy'
import type {K8sDeployment, K8sWorkload, K8sWorkloads} from '../types'

type Kind = K8sWorkload['kind']
const KINDS: Kind[] = ['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob']
const rolloutKind = (k: Kind) => k.toLowerCase() as Parameters<ReturnType<typeof useK8s>['be']['server']['k8sRestart']>[3]
const rid = (w: Pick<K8sWorkload, 'namespace' | 'name'>) => w.namespace + '/' + w.name

/** the pod whose logs stand for a workload: the pods of its namespace that it owns (the owner text names it, or the pod name starts with it) */
function useWorkloadActions() {
  const k = useK8s()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const where = (w: K8sWorkload) => ({name: w.name, ns: w.namespace, ctx: k.ctx})
  return {
    logs: (w: K8sWorkload) => w.kind !== 'CronJob' ? Promise.resolve(k.onLogs({ns: w.namespace, name: w.name, kind: w.kind.toLowerCase() as 'deployment'})) : k.run('logs:' + rid(w), async () => {
      const pods = await k.be.server.k8sPods(k.sid, k.ctx, w.namespace)
      const mine = pods.filter(p => p.owner.includes(w.name) || p.name.startsWith(w.name + '-'))
      const p = mine.find(x => x.tone === 'ok') ?? mine[0]
      if (!p) throw new Error(t('No pod of {name} was found.', {name: w.name}))
      k.onLogs({ns: p.namespace, name: p.name})
    }),
    restart: async (w: K8sWorkload) => {
      if (!(await confirm(t('Restart the {kind} {name} in {ns} on {ctx}? Its pods are replaced one after another.', {kind: w.kind, ...where(w)}), {confirmLabel: t('Restart')}))) return
      void k.run('restart:' + w.kind + ':' + rid(w), () => k.be.server.k8sRestart(k.sid, k.ctx, w.namespace, rolloutKind(w.kind), w.name), t('{name}: restarted', {name: w.name}))
    },
    pause: async (w: K8sWorkload, pause: boolean) => {
      const msg = pause ? t('Pause the rollout of {name} in {ns} on {ctx}? Changes to it wait until you resume.', where(w)) : t('Resume the rollout of {name} in {ns} on {ctx}?', where(w))
      if (!(await confirm(msg, {confirmLabel: pause ? t('Pause') : t('Resume')}))) return
      void k.run('pause:' + rid(w), () => k.be.server.k8sRolloutPause(k.sid, k.ctx, w.namespace, w.name, pause), t('{name}: done', {name: w.name}))
    },
    cron: async (w: K8sWorkload, action: 'run' | 'suspend' | 'resume') => {
      const msg = action === 'run' ? t('Run the CronJob {name} in {ns} on {ctx} now? It starts one Job outside its schedule.', where(w))
        : action === 'suspend' ? t('Suspend the CronJob {name} in {ns} on {ctx}? No new Job starts until you resume it.', where(w))
        : t('Resume the CronJob {name} in {ns} on {ctx}? It starts Jobs on its schedule again.', where(w))
      if (!(await confirm(msg, {confirmLabel: action === 'run' ? t('Run now') : action === 'suspend' ? t('Suspend') : t('Resume')}))) return
      void k.run('cron:' + rid(w), () => k.be.server.k8sCronJob(k.sid, k.ctx, w.namespace, w.name, action), t('{name}: done', {name: w.name}))
    },
    deleteJob: async (w: K8sWorkload) => {
      if (!(await confirm(t('Delete the Job {name} in {ns} on {ctx}? Its pods and their logs are removed.', where(w)), {confirmLabel: t('Delete'), danger: true}))) return
      void k.run('deljob:' + rid(w), () => k.be.server.k8sDeleteJob(k.sid, k.ctx, w.namespace, w.name), t('{name}: deleted', {name: w.name}))
    },
    yaml: (w: K8sWorkload) => k.open({k: 'yaml', kind: w.kind.toLowerCase(), ns: w.namespace, name: w.name, label: t('{kind} {name}', {kind: w.kind, name: w.name}), back: w.kind === 'Deployment' ? {k: 'deploy', ns: w.namespace, name: w.name} : null}),
  }
}

/** Scale: the question is the dialog itself, with a stepper; it names the object, the namespace and the cluster */
function ScaleDialog({w, onClose}: {w: K8sWorkload; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const [n, setN] = useState(w.desired)
  const go = () => { onClose(); void k.run('scale:' + rid(w), () => k.be.server.k8sScale(k.sid, k.ctx, w.namespace, w.name, n), t('{name}: scaled to {n}', {name: w.name, n})) }
  return (
    <Modal title={t('Scale {name}', {name: w.name})} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" onClick={go}>{t('Scale to {n}', {n})}</button></>}>
      <h2 className="k8s-h2">{t('Scale the Deployment {name} in {ns} on {ctx}?', {name: w.name, ns: w.namespace, ctx: k.ctx})}</h2>
      <p>{t('It runs {n} pods now. Pods are added or stopped to match; the ones that stay keep running.', {n: w.desired})}</p>
      {w.hpa && <div className="k8s-warnbox" role="alert">{t('An autoscaler (HPA) owns the replicas of this Deployment: it will set them back to what it wants.')}</div>}
      <div className="k8s-step">
        <button aria-label={t('One fewer')} disabled={n <= 0} onClick={() => setN(x => Math.max(0, x - 1))}>−</button>
        <output aria-label={t('Replicas')}>{n}</output>
        <button aria-label={t('One more')} disabled={n >= 1000} onClick={() => setN(x => Math.min(1000, x + 1))}>+</button>
        <span className="sp-note">{t('replicas')}</span>
      </div>
    </Modal>
  )
}

type WSort = 'name' | 'ready' | 'up' | 'image' | 'age' | 'last' | 'schedule' | 'result'

/** Screens 9 and 11: Deployments, StatefulSets, DaemonSets, Jobs and CronJobs */
export function Workloads() {
  const k = useK8s()
  const t = useT()
  const [kind, setKind] = useKept<Kind>('workloads.kind', 'Deployment')
  const [filter, setFilter] = useKept('workloads.filter', '')
  const [scaling, setScaling] = useState<K8sWorkload | null>(null)
  const act = useWorkloadActions()
  const load = useCallback(() => k.be.server.k8sWorkloads(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sWorkloads>(load, true)
  const {th, apply} = useSort<WSort>(['name', 'image', 'schedule', 'result'])
  const all = data?.items ?? []
  const label: Record<Kind, string> = {Deployment: t('Deployments'), StatefulSet: t('StatefulSets'), DaemonSet: t('DaemonSets'), Job: t('Jobs'), CronJob: t('CronJobs')}
  const mine = all.filter(w => w.kind === kind && matches(filter, w.name, w.namespace, w.image))
  const rows = apply(mine, (w, key) => key === 'name' ? w.name : key === 'ready' ? w.available : key === 'up' ? w.up_to_date : key === 'image' ? w.image : key === 'age' ? w.age_sec : key === 'last' ? (w.last_run_sec ?? Infinity) : key === 'schedule' ? w.schedule : w.result)
  const name = (w: K8sWorkload) => <td className="k8s-name">{w.kind === 'Deployment' ? <button className="sp-link" onClick={() => k.open({k: 'deploy', ns: w.namespace, name: w.name})}><b>{w.name}</b></button> : <b>{w.name}</b>}<small>{w.namespace}{w.owner ? ` · ${w.owner}` : ''}</small></td>
  // Ready: one pill (a Deployment or StatefulSet scaled to 0 says so in the pill instead of "0 / 0" and a note that wraps), a thin bar of
  // how much is ready, and the reason on its own line under it
  const ready = (w: K8sWorkload) => {
    const off = w.desired === 0 && (w.kind === 'Deployment' || w.kind === 'StatefulSet')
    const [have, want] = w.ready.split('/').map(Number)
    const pct = want > 0 ? Math.min(100, Math.round((have / want) * 100)) : 0
    return (
      <td className="k8s-ready">
        <div className="k8s-rline">
          <Pill tone={w.tone}>{off ? t('Scaled to 0') : w.ready.replace('/', ' / ')}</Pill>
          {!off && want > 0 && <span className="k8s-rbar" aria-hidden="true"><i className={w.tone} style={{width: pct + '%'}} /></span>}
        </div>
        {w.note && !off && <span className={'k8s-rnote ' + w.tone} title={w.note}>{w.note}</span>}
      </td>
    )
  }
  // Up to date: how many replicas run the newest template, out of the wanted number; it turns amber while a rollout is behind
  const upToDate = (w: K8sWorkload) => (
    <td className="n m">{w.desired === 0 ? <span className="k8s-upd none">–</span>
      : <span className={'k8s-upd' + (w.up_to_date < w.desired ? ' lag' : '')}>{w.up_to_date}<small> / {w.desired}</small></span>}</td>
  )
  const yaml = (w: K8sWorkload) => <button aria-label={t('YAML of {name}', {name: w.name})} onClick={() => act.yaml(w)}>{t('YAML')}</button>
  const logs = (w: K8sWorkload) => <ActBtn id={'logs:' + rid(w)} right="logs" label={t('Logs of {name}', {name: w.name})} onClick={() => void act.logs(w)}>{t('Logs')}</ActBtn>
  const restart = (w: K8sWorkload) => <ActBtn id={'restart:' + w.kind + ':' + rid(w)} right="restart" label={t('Restart {name}', {name: w.name})} title={t('Redeploy: replaces the pods one after another')} onClick={() => void act.restart(w)}>{t('Restart')}</ActBtn>
  const head = {
    Deployment: <tr>{th('name', t('Deployment'))}{th('ready', t('Ready'))}{th('up', t('Up to date'), 'n')}{th('image', t('Image'))}{th('age', t('Age'), 'n')}<th /></tr>,
    StatefulSet: <tr>{th('name', t('StatefulSet'))}{th('ready', t('Ready'))}<th>{t('Volumes')}</th>{th('age', t('Age'), 'n')}<th /></tr>,
    DaemonSet: <tr>{th('name', t('DaemonSet'))}{th('ready', t('Ready'))}{th('age', t('Age'), 'n')}<th /></tr>,
    Job: <tr>{th('name', t('Job'))}{th('last', t('Last run'))}{th('result', t('Result'))}<th /></tr>,
    CronJob: <tr>{th('name', t('CronJob'))}{th('schedule', t('Schedule'))}{th('last', t('Last run'))}{th('result', t('Result'))}<th /></tr>,
  }[kind]
  const result = (w: K8sWorkload) => <td><Pill tone={w.tone}>{w.suspended ? t('Suspended') : w.result === 'Failed' ? t('Failed') : w.result === 'Completed' ? t('Completed') : w.result || '–'}</Pill>{w.note && <> <span className="sp-note">{w.note}</span></>}</td>
  const last = (w: K8sWorkload) => <td>{w.last_run_sec === null ? t('never') : t('{time} ago', {time: age(w.last_run_sec)})}</td>
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Loading workloads…')} /></div>
  return (
    <div className="sp-view">
      <div className="sp-tabs k8s-sub" role="tablist" aria-label={t('Kind of workload')}>
        {KINDS.map(x => <button key={x} role="tab" aria-selected={kind === x} onClick={() => setKind(x)}>{label[x]}<em>{all.filter(w => w.kind === x).length}</em></button>)}
      </div>
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter workloads')} placeholder={t('Filter workloads')} value={filter} onChange={setFilter} />
      </Toolbar>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead>{head}</thead>
        <tbody>
          {rows.map(w => (
            <tr key={w.kind + rid(w)}>
              {name(w)}
              {w.kind === 'Deployment' && <>{ready(w)}{upToDate(w)}<td className="m">{w.image}</td><td className="n">{age(w.age_sec)}</td>
                <td><div className="sp-acts"><ActBtn id={'scale:' + rid(w)} right="scale" label={t('Scale {name}', {name: w.name})} onClick={() => setScaling(w)}>{t('Scale')}</ActBtn>{restart(w)}{logs(w)}<WorkloadMenu w={w} /></div></td></>}
              {w.kind === 'StatefulSet' && <>{ready(w)}<td className="m">{w.volumes || '–'}</td><td className="n">{age(w.age_sec)}</td><td><div className="sp-acts">{restart(w)}{logs(w)}{yaml(w)}<WorkloadMenu w={w} /></div></td></>}
              {w.kind === 'DaemonSet' && <>{ready(w)}<td className="n">{age(w.age_sec)}</td><td><div className="sp-acts">{restart(w)}{logs(w)}{yaml(w)}<WorkloadMenu w={w} /></div></td></>}
              {w.kind === 'Job' && <>{last(w)}{result(w)}<td><div className="sp-acts">{logs(w)}{yaml(w)}
                <ActBtn id={'deljob:' + rid(w)} right="delete_pod" className="danger" label={t('Delete job {name}', {name: w.name})} onClick={() => void act.deleteJob(w)}>{t('Delete')}</ActBtn></div></td></>}
              {w.kind === 'CronJob' && <><td className="m">{w.schedule}</td>{last(w)}{result(w)}<td><div className="sp-acts">
                <ActBtn id={'cron:' + rid(w)} right="cronjob_run" label={t('Run {name} now', {name: w.name})} onClick={() => void act.cron(w, 'run')}>{t('Run now')}</ActBtn>
                <ActBtn id={'cron:' + rid(w)} right="cronjob_patch" label={w.suspended ? t('Resume {name}', {name: w.name}) : t('Suspend {name}', {name: w.name})} onClick={() => void act.cron(w, w.suspended ? 'resume' : 'suspend')}>{w.suspended ? t('Resume') : t('Suspend')}</ActBtn>
                {logs(w)}{yaml(w)}</div></td></>}
            </tr>
          ))}
        </tbody>
      </table>{data && rows.length === 0 && <div className="sp-empty">{t('No {kind} matches.', {kind: label[kind]})}</div>}</div>
      {kind === 'StatefulSet' && <div className="sp-note">{t('Scale is not offered here: scaling a database by a click is not safe. Restart goes one pod at a time, in order.')}</div>}
      {scaling && <ScaleDialog w={scaling} onClose={() => setScaling(null)} />}
    </div>
  )
}

/** Screen 10: one Deployment, its rollout, the autoscaler and the revisions it can go back to */
export function DeploymentDetail({ns, name}: {ns: string; name: string}) {
  const k = useK8s()
  const t = useT()
  const confirm = useConfirm()
  const act = useWorkloadActions()
  const [scaling, setScaling] = useState(false)
  const load = useCallback(() => k.be.server.k8sDeployment(k.sid, k.ctx, ns, name), [k.be, k.sid, k.ctx, ns, name])
  const {data: d, error, busy, reload} = useKLoad<K8sDeployment>(load, true)
  const back = <Crumb path={[t('Workloads'), t('Deployments'), `${ns} / ${name}`]} onBack={() => k.open(null)} />
  if (!d) return <div className="sp-view"><Toolbar busy={busy} onReload={reload}>{back}</Toolbar><Err error={error} />{!error && <Loading kind="detail" />}</div>
  const w = d.workload
  const good = Math.min(w.available, d.updated), starting = Math.max(0, d.updated - good), old = Math.max(0, w.desired - d.updated)
  const total = Math.max(1, good + starting + old)
  const rollback = async (r: K8sDeployment['revisions'][0]) => {
    if (!(await confirm(t('Roll back {name} in {ns} on {ctx} to revision {rev} ({image})? Its pods are replaced by ones that run that revision.', {name, ns, ctx: k.ctx, rev: r.revision, image: r.image}), {confirmLabel: t('Roll back'), danger: true}))) return
    void k.run('rollback:' + ns + '/' + name, () => k.be.server.k8sRollback(k.sid, k.ctx, ns, name, r.revision), t('{name}: rolled back to {rev}', {name, rev: r.revision}))
  }
  const h = d.hpa
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>{back}</Toolbar>
      <Err error={error} />
      <div className="k8s-title">
        <h2>{w.name}</h2>
        <Pill tone={w.tone}>{t('{ready} ready', {ready: w.ready.replace('/', ' / ')})}</Pill>
        {d.rolling && <Pill tone="warn">{t('Rolling out')}</Pill>}
        {d.paused && <Pill>{t('Paused')}</Pill>}
        <span className="sp-acts k8s-grow">
          <ActBtn id={'scale:' + rid(w)} right="scale" onClick={() => setScaling(true)}>{t('Scale')}</ActBtn>
          <ActBtn id={'restart:Deployment:' + rid(w)} right="restart" title={t('Redeploy: replaces the pods one after another')} onClick={() => void act.restart(w)}>{t('Restart')}</ActBtn>
          <ActBtn id={'pause:' + rid(w)} right="restart" title={d.paused ? t('Resume the orchestration: the changes that waited are rolled out') : t('Pause orchestration: changes to it wait until you resume')} onClick={() => void act.pause(w, !d.paused)}>{d.paused ? t('Resume') : t('Pause')}</ActBtn>
          <ActBtn id={'logs:' + rid(w)} right="logs" onClick={() => void act.logs(w)}>{t('Logs')}</ActBtn>
          <button onClick={() => act.yaml(w)}>{t('YAML')}</button>
          <CopyMenu target={{kind: 'deployment', ns: w.namespace, name: w.name}} />
          <WorkloadMenu w={w} />
        </span>
      </div>
      <Card title={t('Rollout')}>
        <div className="k8s-roll" role="img" aria-label={t('{good} of {n} updated and ready, {starting} updated and starting, {old} old', {good, n: w.desired, starting, old})}>
          <i className="g" style={{width: good / total * 100 + '%'}} /><i className="y" style={{width: starting / total * 100 + '%'}} /><i style={{width: old / total * 100 + '%'}} />
        </div>
        <div className="sp-note">{t('Revision {rev} · {good} updated and ready, {starting} updated and starting, {old} of the old revision still serving.', {rev: d.revision, good, starting, old})}</div>
      </Card>
      <div className="sp-grid sp-grid-2">
        <Card title={t('Spec')}>
          <table className="sp-kv"><tbody>
            <tr><td>{t('Replicas')}</td><td className="m">{t('{desired} desired · {updated} updated · {available} available', {desired: w.desired, updated: d.updated, available: w.available})}</td></tr>
            <tr><td>{t('Strategy')}</td><td className="m">{d.strategy}{d.strategy === 'RollingUpdate' && ` · maxSurge ${d.max_surge} · maxUnavailable ${d.max_unavailable}`}</td></tr>
            <tr><td>{t('Image')}</td><td className="m">{w.image}</td></tr>
            <tr><td>{t('Selector')}</td><td className="m">{d.selector}</td></tr>
          </tbody></table>
        </Card>
        <Card title={h ? t('Autoscaler (HPA {name})', {name: h.name}) : t('Autoscaler')}>
          {!h && <div className="sp-note">{t('No autoscaler owns the replicas of this Deployment.')}</div>}
          {h && <table className="sp-kv"><tbody>
            <tr><td>{t('Replicas')}</td><td className="m">{t('min {min} · max {max} · now {now}', {min: h.min, max: h.max, now: h.current})}</td></tr>
            <tr><td>{t('CPU')}</td><td className="m">{h.cpu_pct === null ? t('no data') : t('{pct}% of target {target}%', {pct: h.cpu_pct, target: h.target_cpu_pct ?? '?'})}</td></tr>
            <tr><td>{t('Last scaled')}</td><td>{h.last_scaled_sec === null ? t('never') : t('{time} ago', {time: age(h.last_scaled_sec)})}</td></tr>
          </tbody></table>}
          {h && <div className="sp-note">{t('While an autoscaler owns the replicas, Scale warns that it will set them back.')}</div>}
        </Card>
      </div>
      <Card title={t('Revision history')}>
        <div className="sp-table"><table>
          <thead><tr><th className="n">{t('Revision')}</th><th>{t('Image')}</th><th>{t('Change cause')}</th><th className="n">{t('Age')}</th><th /></tr></thead>
          <tbody>{d.revisions.map(r => (
            <tr key={r.revision}>
              <td className="n m">{r.revision}{r.current && <> <Pill tone="ok">{t('current')}</Pill></>}</td><td className="m">{r.image}</td>
              <td className={r.cause ? 'm' : 'sp-note'}>{r.cause || t('none recorded')}</td><td className="n">{age(r.age_sec)}</td>
              <td><div className="sp-acts">{!r.current && <ActBtn id={'rollback:' + ns + '/' + name} right="restart" className="danger" title={t('Rollback: put the pods back on this revision')} onClick={() => void rollback(r)}>{t('Roll back to {rev}', {rev: r.revision})}</ActBtn>}</div></td>
            </tr>))}</tbody>
        </table></div>
      </Card>
      {scaling && <ScaleDialog w={w} onClose={() => setScaling(false)} />}
    </div>
  )
}

import {useCallback} from 'react'
import {useT} from '../i18n'
import {Err, Loading, Toolbar, FilterInput} from './shared'
import {matches, Pill, useK8s, useKept, useKLoad, useSort} from './KubernetesShared'
import type {K8sFinding, K8sSecurity} from '../types'

type Sev = K8sFinding['severity']
type SecSort = 'severity' | 'rule' | 'object'
const SEVERITIES: Sev[] = ['high', 'medium', 'low']
const TONE: Record<Sev, string> = {high: 'bad', medium: 'warn', low: ''}

/** Security: what the pods, services and ingresses of the namespace expose. Read only; a hint to look at, not a verdict. */
export function KubernetesSecurity() {
  const k = useK8s()
  const t = useT()
  const [filter, setFilter] = useKept('security.filter', '')
  const [off, setOff] = useKept<Sev[]>('security.off', ['low'])
  const load = useCallback(() => k.be.server.k8sSecurity(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sSecurity>(load, false)
  const {th, apply} = useSort<SecSort>(['rule', 'object'])
  const label = (s: Sev) => (s === 'high' ? t('High') : s === 'medium' ? t('Medium') : t('Low'))
  const rule = (r: string): string => {
    switch (r) {
      case 'privileged': return t('Privileged container')
      case 'host-namespace': return t('Host namespace')
      case 'host-path': return t('Host path')
      case 'capability': return t('Dangerous capability')
      case 'run-as-root': return t('Runs as root')
      case 'plain-secret': return t('Secret in plain text')
      case 'image-tag': return t('Image not pinned')
      case 'node-port': return t('NodePort')
      case 'load-balancer': return t('LoadBalancer')
      case 'ingress-no-tls': return t('Ingress without TLS')
      default: return r
    }
  }
  const all = data?.findings ?? []
  const count = (s: Sev) => all.filter(f => f.severity === s).length
  const shown = apply(all.filter(f => !off.includes(f.severity) && matches(filter, f.name, f.namespace, f.kind, f.detail, rule(f.rule), f.container ?? '')),
    (f, key) => key === 'severity' ? SEVERITIES.indexOf(f.severity) : key === 'rule' ? rule(f.rule) : f.namespace + '/' + f.name)
  const toggle = (s: Sev) => setOff(off.includes(s) ? off.filter(x => x !== s) : [...off, s])
  const open = (f: K8sFinding) => {
    if (f.kind === 'Deployment') k.open({k: 'deploy', ns: f.namespace, name: f.name})
    else if (f.kind === 'Pod') k.open({k: 'pod', ns: f.namespace, name: f.name})
  }
  if (!data && !error) return <div className="sp-view"><Loading kind="table" what={t('Looking at the cluster…')} /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <FilterInput label={t('Filter findings')} placeholder={t('Filter findings')} value={filter} onChange={setFilter} />
        {SEVERITIES.map(s => <button key={s} aria-pressed={!off.includes(s)} className={off.includes(s) ? '' : 'primary'} onClick={() => toggle(s)}>{label(s)} · {count(s)}</button>)}
        <span className="sp-note" role="status">{data ? t('{pods} pods and {services} services looked at · read only', {pods: data.pods, services: data.services}) : ''}</span>
      </Toolbar>
      <Err error={error} />
      <div className="sp-table k8s-wrap"><table>
        <thead><tr>{th('severity', t('Severity'))}{th('rule', t('Finding'))}{th('object', t('Object'))}<th>{t('Detail')}</th></tr></thead>
        <tbody>{shown.map((f, i) => (
          <tr key={i}>
            <td><Pill tone={TONE[f.severity]}>{label(f.severity)}</Pill></td>
            <td>{rule(f.rule)}</td>
            <td className="k8s-name">
              {f.kind === 'Deployment' || f.kind === 'Pod' ? <button className="sp-link" onClick={() => open(f)}><b>{f.name}</b></button> : <b>{f.name}</b>}
              <small>{f.kind} · {f.namespace}{f.container ? ' · ' + f.container : ''}</small>
            </td>
            <td className="k8s-msg">{f.detail}{f.count > 1 && <> <small>({t('{n} pods', {n: f.count})})</small></>}</td>
          </tr>))}</tbody>
      </table>{data && shown.length === 0 && <div className="sp-empty">{all.length === 0 ? t('Nothing to report.') : t('No finding matches.')}</div>}</div>
      <div className="sp-note k8s-hint">{t('A hint to look at, not a verdict: a network plugin or a node agent has good reasons to be privileged. Values of variables are never shown, and no Secret is read.')}</div>
    </div>
  )
}

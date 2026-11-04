import {useCallback, useState} from 'react'
import {useT, useTn} from '../i18n'
import {Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {Card, Err, Loading, RefreshButton, Toolbar} from './shared'
import {certLevel} from './format'
import {ActBtn, matches, Pill, useK8s, useKept, useKLoad} from './KubernetesShared'
import type {K8sForward, K8sNetwork, K8sService} from '../types'

/** the service ports of a "80 → 3000/TCP, 443 → 8443/TCP" text: the leading number of each */
export const servicePorts = (text: string): number[] => text.split(',').map(p => parseInt(p.trim(), 10)).filter(n => n > 0 && n < 65536)

function ForwardDialog({s, onClose}: {s: K8sService; onClose(): void}) {
  const k = useK8s()
  const t = useT()
  const ports = servicePorts(s.ports)
  const [remote, setRemote] = useState(ports[0] ?? 80)
  const [local, setLocal] = useState(String(remote < 1024 ? remote + 8000 : remote))
  const n = Number(local)
  const ok = Number.isInteger(n) && n >= 1024 && n <= 65535
  const go = () => { onClose(); void k.run('forward:' + s.namespace + '/' + s.name, () => k.be.server.k8sPortForwardStart(k.sid, k.ctx, s.namespace, s.name, n, remote), t('localhost:{port} is open', {port: n})) }
  return (
    <Modal title={t('Port-forward {name}', {name: s.name})} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!ok} onClick={go}>{t('Start')}</button></>}>
      <h2 className="k8s-h2">{t('Port-forward the Service {name} in {ns} on {ctx}?', {name: s.name, ns: s.namespace, ctx: k.ctx})}</h2>
      <p>{t('Opens a port on this Mac that leads to the Service, through the SSH connection and kubectl on {host}.', {host: k.host.label})}</p>
      <div className="k8s-route" aria-label={t('Route')}><span>localhost:{ok ? n : '…'}</span><i>{t('→ SSH →')}</i><span>{k.host.label} 127.0.0.1:{t('a free port')}</span><i>{t('→ kubectl →')}</i><span>{s.namespace}/{s.name}:{remote}</span></div>
      <div className="k8s-two">
        <label className="field"><span>{t('Port on this Mac')}</span><input inputMode="numeric" aria-invalid={!ok} value={local} onChange={e => setLocal(e.target.value.replace(/\D/g, ''))} /></label>
        <div className="field"><span>{t('Port of the Service')}</span>
          {ports.length > 1 ? <Select label={t('Port of the Service')} value={String(remote)} onChange={v => setRemote(Number(v))} options={ports.map(p => ({value: String(p), label: String(p)}))} /> : <input aria-label={t('Port of the Service')} value={remote} disabled readOnly />}
        </div>
      </div>
      {!ok && <div className="k8s-warnbox" role="alert">{t('Use a port from 1024 to 65535.')}</div>}
      <div className="sp-note">{t('Listens on 127.0.0.1 only, so nobody else on your network can use it.')}</div>
    </Modal>
  )
}

function Forwards() {
  const k = useK8s()
  const t = useT()
  const load = useCallback(() => k.be.server.k8sPortForwards(k.sid), [k.be, k.sid])
  const {data, error, busy, reload} = useKLoad<K8sForward[]>(load, false)
  const tone = (f: K8sForward) => (f.status === 'running' ? 'ok' : f.status === 'failed' ? 'bad' : '')
  return (
    <Card title={t('Open port-forwards')} collapsible right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && data.length === 0 && <div className="sp-note">{t('No port-forward is open.')}</div>}
      {(data ?? []).map(f => (
        <div key={f.id} className="k8s-fw">
          <Pill tone={tone(f)}>{f.status === 'running' ? t('Running') : f.status === 'failed' ? t('Failed') : t('Stopped')}</Pill>
          <span className="m">localhost:{f.local_port} → {f.namespace}/{f.service}:{f.remote_port}</span>
          {f.error && <span className="sp-note">{f.error}</span>}
          <span className="sp-acts k8s-grow">
            {f.status === 'running' && <button onClick={() => void k.be.system.openUrl('http://localhost:' + f.local_port)}>{t('Open in browser')}</button>}
            <ActBtn id={'stop:' + f.id} label={t('Stop port-forward {port}', {port: f.local_port})} onClick={() => void k.run('stop:' + f.id, () => k.be.server.k8sPortForwardStop(k.sid, f.id), t('Port-forward {port} stopped', {port: f.local_port}))}>{t('Stop')}</ActBtn>
          </span>
        </div>
      ))}
      <div className="sp-note">{t('Stopped by Stop, or when you close the Panel or the SSH connection drops. Nothing is left running on the server.')}</div>
    </Card>
  )
}

/** Screen 12 and 14: Services (with Port-forward), Ingress with the days its certificate has left, and the open port-forwards */
export function KubernetesNetwork() {
  const k = useK8s()
  const t = useT()
  const tn = useTn()
  const [forwarding, setForwarding] = useState<K8sService | null>(null)
  const [fs, setFs] = useKept('network.services.filter', '') // what each box is searched for, and which are closed
  const [fi, setFi] = useKept('network.ingress.filter', '')
  const [closedS, setClosedS] = useKept('network.services.closed', false)
  const [closedI, setClosedI] = useKept('network.ingress.closed', false)
  const load = useCallback(() => k.be.server.k8sNetwork(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sNetwork>(load, false)
  const endpoints = (s: K8sService) => s.ready_endpoints === 0
    ? <Pill tone="bad">{t('0 ready')}</Pill>
    : s.not_ready_endpoints > 0 ? <Pill tone="warn">{t('{n} ready, {m} not', {n: s.ready_endpoints, m: s.not_ready_endpoints})}</Pill> : <Pill tone="ok">{t('{n} ready', {n: s.ready_endpoints})}</Pill>
  const address = data?.ingresses.find(i => i.address)?.address
  const services = (data?.services ?? []).filter(s => matches(fs, s.name, s.namespace, s.type, s.cluster_ip, s.ports))
  const ingresses = (data?.ingresses ?? []).filter(i => matches(fi, i.host, i.path, i.service, i.class, i.namespace, i.tls_secret))
  const counted = (shown: number, all: number, q: string) => (q.trim() ? `${shown} / ${all}` : all)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" /></div>
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('Services')} collapsible closed={closedS} onToggle={() => setClosedS(!closedS)} count={counted(services.length, data?.services.length ?? 0, fs)} filter={{value: fs, onChange: setFs, label: t('Search Services')}} right={<RefreshButton busy={busy} onReload={reload} />}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Name')}</th><th>{t('Type')}</th><th>{t('Cluster IP')}</th><th>{t('Ports')}</th><th>{t('Endpoints')}</th><th /></tr></thead>
          <tbody>{services.map(s => (
            <tr key={s.namespace + '/' + s.name}>
              <td className="k8s-name"><b>{s.name}</b><small>{s.namespace}</small></td><td>{s.type}</td><td className="m">{s.cluster_ip === 'None' ? t('None (headless)') : s.cluster_ip}</td><td className="m">{s.ports || '–'}</td><td>{endpoints(s)}</td>
              <td><div className="sp-acts">{s.ready_endpoints > 0 && servicePorts(s.ports).length > 0 && <ActBtn id={'forward:' + s.namespace + '/' + s.name} right="portforward" label={t('Port-forward {name}', {name: s.name})} onClick={() => setForwarding(s)}>{t('Port-forward')}</ActBtn>}</div></td>
            </tr>))}</tbody>
        </table>{data && data.services.length === 0 && <div className="sp-empty">{t('No Services.')}</div>}{data && data.services.length > 0 && services.length === 0 && <div className="sp-empty">{t('No Service matches.')}</div>}</div>
      </Card>
      <Card title={t('Ingress')} collapsible closed={closedI} onToggle={() => setClosedI(!closedI)} count={counted(ingresses.length, data?.ingresses.length ?? 0, fi)} filter={{value: fi, onChange: setFi, label: t('Search Ingress')}}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Host and path')}</th><th>{t('Goes to')}</th><th>{t('Class')}</th><th>{t('TLS certificate')}</th></tr></thead>
          <tbody>{ingresses.map((i, n) => (
            <tr key={n}>
              <td className="m">{i.host} {i.path}</td><td className="m">{i.service}</td><td>{i.class || '–'}</td>
              <td>{i.cert_days_left === null ? <span className="sp-note">{i.tls_secret ? t('not readable') : t('no TLS')}</span>
                : <><Pill tone={certLevel(i.cert_days_left)}>{i.cert_days_left < 0 ? t('expired') : certLevel(i.cert_days_left) === 'ok' ? tn(i.cert_days_left, 'valid {n} day', 'valid {n} days') : tn(i.cert_days_left, 'expires in {n} day', 'expires in {n} days')}</Pill> <span className="sp-note">{t('secret {name}', {name: i.tls_secret})}</span></>}</td>
            </tr>))}</tbody>
        </table>{data && data.ingresses.length === 0 && <div className="sp-empty">{t('No Ingress.')}</div>}{data && data.ingresses.length > 0 && ingresses.length === 0 && <div className="sp-empty">{t('No Ingress matches.')}</div>}</div>
        <div className="sp-note">{t('Certificate dates are read from the TLS secret without showing its contents.')}{address ? ' ' + t('Address of the ingress controller: {address}.', {address}) : ''}</div>
      </Card>
      <Forwards />
      {forwarding && <ForwardDialog s={forwarding} onClose={() => setForwarding(null)} />}
    </div>
  )
}

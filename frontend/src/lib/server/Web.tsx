import {useCallback, useState} from 'react'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Modal} from '../ui/Modal'
import {Card, Err, Toolbar, useLoad} from './shared'
import {certLevel} from './format'
import type {ServerCerts, ServerWeb, ServerWebResult} from '../types'

/** The Web tab: when the Let's Encrypt certificates run out, and the nginx or Caddy that serves them. */
export function Web({sid, onLogs}: {sid: string; onLogs(kind: 'web', name: string): void}) {
  return (
    <div className="sp-view">
      <Certificates sid={sid} />
      <WebServer sid={sid} onLogs={onLogs} />
    </div>
  )
}

function Certificates({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const load = useCallback(() => be.server.certificates(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerCerts>(load, null)
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const [said, setSaid] = useState<{name: string; text: string; dry: boolean} | null>(null)
  const when = (unix: number) => new Date(unix * 1000).toLocaleDateString()
  const renew = async (name: string, dry: boolean) => {
    if (!dry && !(await confirm(t('Renew the certificate {name} now? Let\'s Encrypt is asked for a new one and the web server may reload.', {name}), {confirmLabel: t('Renew')}))) return
    setWorking(name)
    try { setSaid({name, text: await be.server.certRenew(sid, name, dry), dry}); if (!dry) reload() } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
  }
  return (
    <Card title={t('Certificates')} right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && !data.found && <div className="sp-empty">{t("No Let's Encrypt certificates found, or this login cannot read them.")}</div>}
      {data?.found && (
        <div className="sp-table"><table>
          <thead><tr><th>{t('Domains')}</th><th>{t('Expires')}</th><th>{t('Time left')}</th><th /></tr></thead>
          <tbody>
            {data.certs.map(c => (
              <tr key={c.name}>
                <td><b>{c.domains[0] ?? c.name}</b>{c.domains.length > 1 && <div className="sp-note">{c.domains.slice(1).join(', ')}</div>}</td>
                <td className="m">{when(c.not_after)}</td>
                <td><span className={'sp-pill ' + certLevel(c.days_left)}>{c.days_left < 0 ? t('Expired') : tn(c.days_left, '{n} day', '{n} days', {n: c.days_left})}</span></td>
                <td><div className="sp-acts">
                  <button aria-busy={working === c.name} disabled={working === c.name} onClick={() => void renew(c.name, true)}>{t('Test renewal')}</button>
                  <button className="primary" aria-busy={working === c.name} disabled={working === c.name} onClick={() => void renew(c.name, false)}>{t('Renew')}</button>
                </div></td>
              </tr>
            ))}
          </tbody>
        </table>{data.certs.length === 0 && <div className="sp-empty">{t('No certificates.')}</div>}</div>
      )}
      {said && <Modal wide title={(said.dry ? t('Test of the renewal of {name}', {name: said.name}) : t('Renewal of {name}', {name: said.name}))} onClose={() => setSaid(null)} footer={<button onClick={() => setSaid(null)}>{t('Close')}</button>}><pre className="sp-log" style={{whiteSpace: "pre-wrap"}}>{said.text || "–"}</pre></Modal>}
    </Card>
  )
}

function WebServer({sid, onLogs}: {sid: string; onLogs(kind: 'web', name: string): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState<'' | 'test' | 'reload'>('')
  const [result, setResult] = useState<ServerWebResult | null>(null)
  const load = useCallback(() => be.server.web(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerWeb>(load, null)
  const kind = data?.kind || ''
  const name = kind === 'caddy' ? 'Caddy' : 'nginx'
  const test = async () => {
    if (!kind) return
    setWorking('test')
    try { setResult(await be.server.webTest(sid, kind)) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
  }
  const reloadServer = async () => {
    if (!kind) return
    if (!(await confirm(t('Reload {name}? It tests the configuration first and does nothing if the test fails.', {name}), {confirmLabel: t('Reload')}))) return
    setWorking('reload')
    try {
      const r = await be.server.webReload(sid, kind)
      setResult(r)
      if (r.ok) toast.info(t('{name} reloaded', {name}))
    } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <Card title={t('Web server')} right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && !kind && <div className="sp-empty">{t('Neither nginx nor Caddy is installed.')}</div>}
      {data && kind && (
        <>
          <div className="sp-bar">
            <b>{name}</b>
            <span className="sp-os">{data.version}</span>
            {data.running ? <span className="sp-pill ok">{t('Running')}</span> : <span className="sp-pill bad">{t('Stopped')}</span>}
            <button aria-busy={working === 'test'} disabled={working !== ''} onClick={() => void test()}>{t('Test configuration')}</button>
            <button className="primary" aria-busy={working === 'reload'} disabled={working !== ''} onClick={() => void reloadServer()}>{t('Reload')}</button>
            <button onClick={() => onLogs('web', kind)}>{t('Logs')}</button>
          </div>
          {result && <pre className={'sp-pre ' + (result.ok ? '' : 'bad')} role="status" aria-label={t('Result')}>{result.output || (result.ok ? t('The configuration is fine.') : t('The test failed.'))}</pre>}
          <div className="sp-table"><table>
            <thead><tr><th>{t('Site')}</th><th>{t('Listens on')}</th><th>{t('Goes to')}</th></tr></thead>
            <tbody>
              {data.sites.map((s, i) => (
                <tr key={i}>
                  <td><b>{s.names.join(', ') || '–'}</b></td>
                  <td className="m">{s.listen.join(', ') || '–'}</td>
                  <td className="m">{s.targets.join(', ') || '–'}</td>
                </tr>
              ))}
            </tbody>
          </table>{data.sites.length === 0 && <div className="sp-empty">{t('No sites found, or this login cannot read the configuration.')}</div>}</div>
        </>
      )}
    </Card>
  )
}

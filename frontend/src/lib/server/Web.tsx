import {useCallback, useState} from 'react'
import {Field, Modal} from '../ui/Modal'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import {certLevel} from './format'
import type {ServerCerts, ServerWeb, ServerWebResult, WebSiteFile} from '../types'

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
  const [out, setOut] = useState<{ok: boolean; text: string} | null>(null)
  const when = (unix: number) => new Date(unix * 1000).toLocaleDateString()
  const run = async (key: string, fn: () => Promise<string>, done: string) => {
    setWorking(key); setOut(null)
    try { setOut({ok: true, text: await fn()}); toast.info(done) } catch (e) { setOut({ok: false, text: errMsg(e)}) }
    setWorking(''); reload()
  }
  const renew = async (name: string) => {
    if (!(await confirm(t('Renew the certificate {name}? certbot renews it only when it is within 30 days of its end. The web server is reloaded afterwards.', {name}), {confirmLabel: t('Renew')}))) return
    await run('c' + name, () => be.server.certRenew(sid, name, false), t('{name}: renewal finished', {name}))
  }
  const force = async (name: string) => {
    if (!(await confirm(t('Renew {name} now, whatever its age? Let\'s Encrypt allows only a few of these a week.', {name}), {confirmLabel: t('Renew now'), danger: true}))) return
    await run('c' + name, () => be.server.certRenew(sid, name, true), t('{name}: renewed', {name}))
  }
  const trial = async () => {
    if (!(await confirm(t('Try renewing every certificate without changing anything?'), {confirmLabel: t('Try')}))) return
    await run('trial', () => be.server.certRenewTest(sid), t('The trial renewal finished.'))
  }
  return (
    <Card title={t('Certificates')} right={<><button aria-busy={working === 'trial'} disabled={working !== ''} onClick={() => void trial()}>{t('Test renewal')}</button><Toolbar busy={busy} onReload={reload} /></>}>
      <Err error={error} />
      {out && <pre className={'sp-pre ' + (out.ok ? '' : 'bad')} role="status" aria-label={t('Result')}>{out.text}</pre>}
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
                  <button disabled={working !== ''} aria-busy={working === 'c' + c.name} aria-label={t('Renew the certificate {name}', {name: c.name})} onClick={() => void renew(c.name)}>{t('Renew')}</button>
                  <button className="danger" disabled={working !== ''} aria-label={t('Renew {name} now', {name: c.name})} onClick={() => void force(c.name)}>{t('Renew now')}</button>
                </div></td>
              </tr>
            ))}
          </tbody>
        </table>{data.certs.length === 0 && <div className="sp-empty">{t('No certificates.')}</div>}</div>
      )}
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
  const loadFiles = useCallback(() => be.server.webSites(sid), [be, sid])
  const {data: files, reload: reloadFiles} = useLoad<WebSiteFile[]>(loadFiles, null)
  const [switching, setSwitching] = useState('')
  const [asking, setAsking] = useState(false)
  const kind = data?.kind || ''
  const toggle = async (f: WebSiteFile) => {
    if (!(await confirm(f.enabled ? t('Switch off the site {name}? Its file stays; nginx stops serving it after the next reload.', {name: f.name}) : t('Switch on the site {name}? The configuration is tested first and put back if it fails.', {name: f.name}), {confirmLabel: f.enabled ? t('Switch off') : t('Switch on'), danger: f.enabled}))) return
    setSwitching(f.name)
    try { await be.server.webSiteSet(sid, f.name, !f.enabled); toast.info(t('{name} changed. Reload {server} to apply it.', {name: f.name, server: 'nginx'})) } catch (e) { toast.error(errMsg(e)) }
    setSwitching(''); reloadFiles()
  }
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
            {kind === 'nginx' && <button onClick={() => setAsking(true)}>{t('Get certificate')}</button>}
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
          {kind === 'nginx' && files && files.length > 0 && (
            <>
              <h4 className="sp-sub">{t('Site files')}</h4>
              <div className="sp-table"><table>
                <thead><tr><th>{t('File')}</th><th>{t('Status')}</th><th /></tr></thead>
                <tbody>
                  {files.map(f => (
                    <tr key={f.name}>
                      <td className="m">{f.name}</td>
                      <td>{f.enabled ? <span className="sp-pill ok">{t('On')}</span> : <span className="sp-pill">{t('Off')}</span>}</td>
                      <td><div className="sp-acts"><button className={f.enabled ? 'danger' : ''} disabled={switching !== ''} aria-busy={switching === f.name} aria-label={f.enabled ? t('Switch off the site {name}', {name: f.name}) : t('Switch on the site {name}', {name: f.name})} onClick={() => void toggle(f)}>{f.enabled ? t('Switch off') : t('Switch on')}</button></div></td>
                    </tr>
                  ))}
                </tbody>
              </table></div>
            </>
          )}
        </>
      )}
      {asking && <CertForm sid={sid} onClose={() => { setAsking(false); reload() }} />}
    </Card>
  )
}

/** Ask Let's Encrypt for a certificate for one or more domains; certbot sets up HTTPS in the nginx site that serves them. */
function CertForm({sid, onClose}: {sid: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [domains, setDomains] = useState('')
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [out, setOut] = useState('')
  const list = domains.split(/[\s,]+/).filter(Boolean)
  const go = async () => {
    setBusy(true); setError('')
    try { setOut(await be.server.certRequest(sid, list, email.trim())); toast.info(t('Certificate received.')) } catch (e) { setError(errMsg(e)) }
    setBusy(false)
  }
  return (
    <Modal title={t('Get certificate')} onClose={onClose} footer={out ? <button className="primary" onClick={onClose}>{t('Close')}</button> : <><button onClick={onClose}>{t('Cancel')}</button><button className="primary" aria-busy={busy} disabled={busy || list.length === 0 || email.trim() === ''} onClick={go}>{t('Get certificate')}</button></>}>
      <Err error={error} />
      {out ? <pre className="sp-pre" role="status" aria-label={t('Result')}>{out}</pre> : (
        <>
          <Field label={t('Domains')}><input value={domains} spellCheck={false} onChange={e => setDomains(e.target.value)} placeholder="example.com www.example.com" /></Field>
          <Field label={t('E-mail')}><input value={email} type="email" spellCheck={false} onChange={e => setEmail(e.target.value)} placeholder="you@example.com" /></Field>
          <div className="sp-note">{t('The domains must already point at this server and an nginx site must serve them on port 80. certbot also sets up HTTPS and the redirect in that site. Wildcards are not supported here.')}</div>
        </>
      )}
    </Modal>
  )
}

import {useCallback, useState} from 'react'
import {Field, Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Err, Toolbar, useLoad} from './shared'
import type {FirewallRule, ServerPorts} from '../types'

/** the ports of a "To" column of ufw: "22/tcp", "80,443/tcp", "8000:8100/tcp" */
const rulePorts = (to: string): number[] => to.split(' ')[0].split('/')[0].split(',').flatMap(p => {
  const [lo, hi] = p.split(':').map(Number)
  return hi === undefined ? [lo] : hi >= lo && hi - lo < 65536 ? Array.from({length: hi - lo + 1}, (_, i) => lo + i) : []
})

/** The Ports & firewall tab: what listens, what ufw says about it, and the controls to change it (each change asks first). */
export function Ports({sid, sshPort}: {sid: string; sshPort: number}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const load = useCallback(() => be.server.ports(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerPorts>(load, null)
  const [working, setWorking] = useState('')
  const [form, setForm] = useState<{port?: number} | null>(null)
  const [err, setErr] = useState('')
  const exposed = (data?.ports ?? []).filter(p => !p.local && p.open && ![22, 80, 443, sshPort].includes(p.port))
  const manage = data?.firewall === 'active'

  const act = async (key: string, fn: () => Promise<void>, done: string) => {
    setWorking(key); setErr('')
    try { await fn(); toast.info(done); reload() } catch (e) { setErr(errMsg(e)) } finally { setWorking('') }
  }
  /** a change that could cut this very connection asks a second, harder question */
  const lockout = async (port: number[], what: string) => (!port.includes(sshPort) || confirm(t('Port {port} is the one this connection uses. {what} could lock you out of the server. Continue?', {port: sshPort, what}), {confirmLabel: t('Continue'), danger: true}))
  const addRule = async (action: 'allow' | 'deny', port: number, proto: string, from: string) => {
    if (action === 'deny' && !(await lockout([port], t('Denying it')))) return false
    await be.server.firewallAdd(sid, action, port, proto, from, port === sshPort)
    return true
  }
  const quick = async (action: 'allow' | 'deny', port: number) => {
    if (!(await confirm(action === 'allow' ? t('Allow port {port} through the firewall for everyone?', {port}) : t('Deny port {port} in the firewall?', {port}), {confirmLabel: action === 'allow' ? t('Allow') : t('Deny'), danger: action === 'deny'}))) return
    if (action === 'deny' && !(await lockout([port], t('Denying it')))) return
    await act('p' + port, () => be.server.firewallAdd(sid, action, port, 'tcp', '', port === sshPort), action === 'allow' ? t('Port {port} allowed.', {port}) : t('Port {port} denied.', {port}))
  }
  const drop = async (r: FirewallRule) => {
    if (!(await confirm(t('Delete the rule {rule}?', {rule: `${r.to} ${r.action} ${r.from}`}), {confirmLabel: t('Delete'), danger: true}))) return
    if (r.action === 'ALLOW' && !(await lockout(rulePorts(r.to), t('Deleting it')))) return
    await act('r' + r.num, () => be.server.firewallDelete(sid, r, r.action === 'ALLOW' && rulePorts(r.to).includes(sshPort)), t('Rule deleted.'))
  }
  const turnOn = async () => {
    if (!(await confirm(t('Turn the firewall on? Port {port} (this connection) is allowed first. Every other port that has no allow rule is shut.', {port: sshPort}), {confirmLabel: t('Turn on'), danger: true}))) return
    await act('on', () => be.server.firewallEnable(sid), t('Firewall is on.'))
  }
  const turnOff = async () => {
    if (!(await confirm(t('Turn the firewall off? Every port that something listens on is open to the outside.'), {confirmLabel: t('Turn off'), danger: true}))) return
    await act('off', () => be.server.firewallDisable(sid), t('Firewall is off.'))
  }

  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        {data && <span className="sp-os">{data.firewall === 'active' ? t('Firewall (ufw) is on') : data.firewall === 'inactive' ? t('Firewall (ufw) is off') : t('No ufw firewall found, or this login cannot read it')}</span>}
        {data && data.firewall !== 'unknown' && <span className="sp-acts">
          <button onClick={() => setForm({})}>{t('Add rule')}</button>
          {data.firewall === 'inactive' ? <button className="primary" disabled={working === 'on'} onClick={() => void turnOn()}>{t('Turn on')}</button> : <button className="danger" disabled={working === 'off'} onClick={() => void turnOff()}>{t('Turn off')}</button>}
        </span>}
      </Toolbar>
      <Err error={error || err} />
      <div className="sp-table"><table>
        <thead><tr><th>{t('Port')}</th><th>{t('Process')}</th><th>{t('Listens on')}</th><th>{t('Firewall')}</th>{manage && <th />}</tr></thead>
        <tbody>
          {(data?.ports ?? []).map(p => (
            <tr key={p.port}>
              <td className="m">{p.port}</td><td>{p.process || '–'}</td><td className="m">{p.local ? t('This server only') : t('Everywhere')}</td>
              <td>{p.open ? <span className="sp-pill ok">{t('Allowed')}</span> : <span className="sp-pill">{t('Shut')}</span>}</td>
              {manage && <td><div className="sp-acts">
                {p.open
                  ? <button className="danger" disabled={working === 'p' + p.port} aria-label={t('Deny port {port}', {port: p.port})} onClick={() => void quick('deny', p.port)}>{t('Deny')}</button>
                  : <button disabled={working === 'p' + p.port} aria-label={t('Allow port {port}', {port: p.port})} onClick={() => void quick('allow', p.port)}>{t('Allow')}</button>}
              </div></td>}
            </tr>
          ))}
        </tbody>
      </table>{data && data.ports.length === 0 && <div className="sp-empty">{t('No listening ports found.')}</div>}</div>
      {manage && (
        <>
          <h4 className="sp-sub">{t('Firewall rules')}</h4>
          <div className="sp-table"><table>
            <thead><tr><th>{t('To')}</th><th>{t('Action')}</th><th>{t('From')}</th><th /></tr></thead>
            <tbody>
              {data.rules.map(r => (
                <tr key={r.num}>
                  <td className="m">{r.to}</td>
                  <td><span className={'sp-pill ' + (r.action === 'ALLOW' ? 'ok' : 'bad')}>{r.action === 'ALLOW' ? t('Allow') : r.action === 'DENY' ? t('Deny') : r.action}</span></td>
                  <td className="m">{r.from}</td>
                  <td><div className="sp-acts"><button className="danger" disabled={working === 'r' + r.num} aria-label={t('Delete the rule {rule}', {rule: `${r.to} ${r.action} ${r.from}`})} onClick={() => void drop(r)}>{t('Delete')}</button></div></td>
                </tr>
              ))}
            </tbody>
          </table>{data.rules.length === 0 && <div className="sp-empty">{t('No rules.')}</div>}</div>
        </>
      )}
      {data && data.firewall === 'unknown'
        ? <div className="sp-note">{t('The firewall can be changed here once ufw is installed and this login may use sudo.')}</div>
        : <div className="sp-note">{t('Changes use ufw on the server. The port of this connection is guarded so that the panel cannot lock itself out.')}</div>}
      {exposed.length > 0 && <div className="sp-note">{t('Open to the outside: {ports}. Limit them by address or put them behind a web server if they should not be public.', {ports: exposed.map(p => p.port).join(', ')})}</div>}
      {form && <RuleForm port={form.port} onAdd={addRule} onClose={() => { setForm(null); reload() }} />}
    </div>
  )
}

/** Add an allow or deny rule for one port, for everyone or for one address or network. */
function RuleForm({port: first, onAdd, onClose}: {port?: number; onAdd(action: 'allow' | 'deny', port: number, proto: string, from: string): Promise<boolean>; onClose(): void}) {
  const t = useT()
  const toast = useToast()
  const [action, setAction] = useState<'allow' | 'deny'>('allow')
  const [port, setPort] = useState(first ? String(first) : '')
  const [proto, setProto] = useState('tcp')
  const [from, setFrom] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const n = Number(port)
  const valid = Number.isInteger(n) && n >= 1 && n <= 65535
  const go = async () => {
    setBusy(true); setError('')
    try {
      if (await onAdd(action, n, proto, from.trim())) { toast.info(t('Rule added.')); onClose() } else setBusy(false)
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={t('Add rule')} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={busy || !valid} onClick={go}>{t('Add rule')}</button></>}>
      <Err error={error} />
      <Field label={t('Action')}><Select label={t('Action')} value={action} onChange={v => setAction(v as 'allow' | 'deny')} options={[{value: 'allow', label: t('Allow')}, {value: 'deny', label: t('Deny')}]} /></Field>
      <Field label={t('Port')}><input value={port} inputMode="numeric" spellCheck={false} onChange={e => setPort(e.target.value)} placeholder="8080" /></Field>
      <Field label={t('Protocol')}><Select label={t('Protocol')} value={proto} onChange={setProto} options={[{value: 'tcp', label: 'TCP'}, {value: 'udp', label: 'UDP'}, {value: '', label: t('TCP and UDP')}]} /></Field>
      <Field label={t('From')}><input value={from} spellCheck={false} onChange={e => setFrom(e.target.value)} placeholder={t('Anyone, or an address such as 203.0.113.7 or 10.0.0.0/8')} /></Field>
    </Modal>
  )
}

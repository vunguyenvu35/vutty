import {useCallback, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {Field, Modal} from '../ui/Modal'
import {errMsg} from '../util'
import {RowMenu} from './RowMenu'
import {Card, Err, Toolbar, useLoad} from './shared'
import type {Host, ServerKey, ServerUsers} from '../types'

/**
 * The Users tab: who can log in to the server, who may use sudo, how many SSH keys each one has, and who is logged in now.
 * Accounts and keys can be added and removed; every change asks first, and the account the panel is logged in as cannot be cut off.
 */
export function Users({sid, host}: {sid: string; host: Host}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [adding, setAdding] = useState(false)
  const [keysOf, setKeysOf] = useState('')
  const [deleting, setDeleting] = useState('')
  const load = useCallback(() => be.server.users(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUsers>(load, null)
  const sudo = async (name: string, on: boolean) => {
    const msg = on ? t('Let {name} use sudo? The account can then run any command as root.', {name}) : t('Take sudo away from {name}?', {name})
    if (!(await confirm(msg, {confirmLabel: on ? t('Give sudo') : t('Take away'), danger: on}))) return
    try { await be.server.setSudo(sid, name, on); toast.info(t('{name}: done', {name})) } catch (e) { toast.error(errMsg(e)) }
    reload()
  }
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('Accounts that can log in')} right={<><button onClick={() => setAdding(true)}>{t('Add account')}</button><Toolbar busy={busy} onReload={reload} /></>}>
        {data && data.users.length === 0 ? <div className="sp-empty">{t('No accounts found.')}</div> : (
          <div className="sp-table"><table>
            <thead><tr><th>{t('User')}</th><th>{t('Sudo')}</th><th className="n">{t('SSH keys')}</th><th>{t('Last login')}</th><th>{t('Shell')}</th><th /></tr></thead>
            <tbody>{(data?.users ?? []).map(u => {
              const own = u.name === host.username
              return (
                <tr key={u.name}>
                  <td><b>{u.name}</b><div className="sp-note m">{u.home}</div></td>
                  <td>{u.sudo ? <span className="sp-pill warn">{t('Yes')}</span> : <span className="sp-note">{t('No')}</span>}</td>
                  <td className="n m" title={u.keys < 0 ? t('This login cannot read the file.') : undefined}>{u.keys < 0 ? '?' : u.keys}</td>
                  <td className="m">{u.login || '–'}</td>
                  <td className="m">{u.shell}</td>
                  <td><div className="sp-acts">
                    <button onClick={() => setKeysOf(u.name)}>{t('SSH keys')}</button>
                    {u.uid !== 0 && <RowMenu name={u.name} items={[
                      u.sudo ? {label: t('Take sudo away'), disabled: own, run: () => void sudo(u.name, false)} : {label: t('Give sudo'), run: () => void sudo(u.name, true)},
                      {label: t('Delete account'), danger: true, disabled: own, run: () => setDeleting(u.name)},
                    ]} />}
                  </div></td>
                </tr>
              )
            })}</tbody>
          </table></div>
        )}
      </Card>
      <Card title={t('Logged in now')}>
        {data && data.sessions.length === 0 ? <div className="sp-empty">{t('Nobody else is logged in.')}</div> : (
          <div className="sp-table"><table>
            <thead><tr><th>{t('User')}</th><th>{t('Terminal')}</th><th>{t('Since')}</th><th>{t('From')}</th></tr></thead>
            <tbody>{(data?.sessions ?? []).map(s => <tr key={s.tty + s.since}><td><b>{s.user}</b></td><td className="m">{s.tty}</td><td className="m">{s.since}</td><td className="m">{s.from || '–'}</td></tr>)}</tbody>
          </table></div>
        )}
      </Card>
      {adding && <AddAccount sid={sid} onClose={() => setAdding(false)} onDone={() => { setAdding(false); reload() }} />}
      {keysOf && <Keys sid={sid} user={keysOf} own={keysOf === host.username} onClose={() => { setKeysOf(''); reload() }} />}
      {deleting && <DeleteAccount sid={sid} name={deleting} onClose={() => setDeleting('')} onDone={() => { setDeleting(''); reload() }} />}
    </div>
  )
}

function AddAccount({sid, onClose, onDone}: {sid: string; onClose(): void; onDone(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [name, setName] = useState('')
  const [sudo, setSudo] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const ok = /^[a-z_][a-z0-9_-]{0,31}$/.test(name)
  const go = async () => {
    setBusy(true)
    try { await be.server.addUser(sid, name, sudo); toast.info(t('{name}: done', {name})); onDone() } catch (e) { setErr(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={t('Add account')} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!ok || busy} aria-busy={busy} onClick={() => void go()}>{t('Add account')}</button></>}>
      <Field label={t('Account name')}><input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="deploy" spellCheck={false} /></Field>
      {name !== '' && !ok && <div className="error">{t('Lowercase letters, digits, - and _, starting with a letter.')}</div>}
      <label className="check"><input type="checkbox" checked={sudo} onChange={e => setSudo(e.target.checked)} /><span>{t('Can use sudo')}</span></label>
      <div className="sp-note">{t('The account has a home folder and no password: add an SSH key next, or nobody can log in to it.')}</div>
      {err && <div className="error" role="alert">{err}</div>}
    </Modal>
  )
}

function DeleteAccount({sid, name, onClose, onDone}: {sid: string; name: string; onClose(): void; onDone(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [home, setHome] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    try { await be.server.deleteUser(sid, name, home); toast.info(t('{name}: done', {name})); onDone() } catch (e) { setErr(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={t('Delete {name}?', {name})} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="danger" disabled={busy} aria-busy={busy} onClick={() => void go()}>{t('Delete account')}</button></>}>
      <p>{t('The account is removed and its processes are killed.')}</p>
      <label className="check"><input type="checkbox" checked={home} onChange={e => setHome(e.target.checked)} /><span>{t('Also delete its home folder and files')}</span></label>
      {err && <div className="error" role="alert">{err}</div>}
    </Modal>
  )
}

/** the keys of one account: removing one asks first, and adding takes one line of a public key */
function Keys({sid, user, own, onClose}: {sid: string; user: string; own: boolean; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [key, setKey] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => be.server.sshKeys(sid, user), [be, sid, user])
  const {data, error, reload} = useLoad<ServerKey[]>(load, null)
  const add = async () => {
    setBusy(true); setErr('')
    try { await be.server.addSshKey(sid, user, key.trim()); setKey(''); toast.info(t('Key added.')); reload() } catch (e) { setErr(errMsg(e)) }
    setBusy(false)
  }
  const remove = async (k: ServerKey) => {
    const warn = own || (data?.length ?? 0) <= 1 ? ' ' + (own ? t('This is the account the panel logs in as: without a key it may not get back in.') : t('It is the last key of this account.')) : ''
    if (!(await confirm(t('Remove the key {name} from {user}?', {name: k.comment || k.fingerprint, user}) + warn, {confirmLabel: t('Remove'), danger: true}))) return
    try { await be.server.removeSshKey(sid, user, k.fingerprint); toast.info(t('Key removed.')) } catch (e) { toast.error(errMsg(e)) }
    reload()
  }
  return (
    <Modal wide title={t('SSH keys of {name}', {name: user})} onClose={onClose} footer={<button onClick={onClose}>{t('Close')}</button>}>
      <Err error={error} />
      {data && data.length === 0 && <div className="sp-empty">{t('No keys: nobody can log in to this account with a key.')}</div>}
      {data && data.length > 0 && (
        <div className="sp-table"><table>
          <thead><tr><th>{t('Name')}</th><th>{t('Type')}</th><th>{t('Fingerprint')}</th><th /></tr></thead>
          <tbody>{data.map(k => (
            <tr key={k.fingerprint}><td>{k.comment || '–'}</td><td className="m">{k.type}</td><td className="m cut" title={k.fingerprint}>{k.fingerprint}</td>
              <td><div className="sp-acts"><button className="danger" aria-label={t('Remove the key {name}', {name: k.comment || k.fingerprint})} onClick={() => void remove(k)}>{t('Remove')}</button></div></td></tr>
          ))}</tbody>
        </table></div>
      )}
      <Field label={t('Add a public key')}><textarea rows={3} value={key} onChange={e => setKey(e.target.value)} placeholder="ssh-ed25519 AAAA… name" spellCheck={false} /></Field>
      <div><button className="primary" disabled={!key.trim() || busy} aria-busy={busy} onClick={() => void add()}>{t('Add key')}</button></div>
      {err && <div className="error" role="alert">{err}</div>}
    </Modal>
  )
}

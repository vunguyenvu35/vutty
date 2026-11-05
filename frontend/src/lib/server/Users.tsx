import {useCallback, useEffect, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {Field, Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import type {ServerUsers, UserKey} from '../types'

type Dialog = {kind: 'add'} | {kind: 'keys'; name: string; me: boolean} | {kind: 'delete'; name: string; home: string} | null

/** The Users tab: who can log in to the server, who may use sudo, how many SSH keys each one has, and who is logged in now. An account can be added or deleted and its keys changed. */
export function Users({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const [dialog, setDialog] = useState<Dialog>(null)
  const load = useCallback(() => be.server.users(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUsers>(load, null)
  const close = () => { setDialog(null); reload() }
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('Accounts that can log in')} right={<Toolbar busy={busy} onReload={reload}><button onClick={() => setDialog({kind: 'add'})}>{t('Add user')}</button></Toolbar>}>
        {data && data.users.length === 0 ? <div className="sp-empty">{t('No accounts found.')}</div> : (
          <div className="sp-table"><table>
            <thead><tr><th>{t('User')}</th><th>{t('Sudo')}</th><th className="n">{t('SSH keys')}</th><th>{t('Last login')}</th><th>{t('Shell')}</th><th /></tr></thead>
            <tbody>{(data?.users ?? []).map(u => {
              const me = u.name === data?.me
              return (
                <tr key={u.name}>
                  <td><b>{u.name}</b>{me && <> <span className="sp-pill">{t('this connection')}</span></>}<div className="sp-note m">{u.home}</div></td>
                  <td>{u.sudo ? <span className="sp-pill warn">{t('Yes')}</span> : <span className="sp-note">{t('No')}</span>}</td>
                  <td className="n m" title={u.keys < 0 ? t('This login cannot read the file.') : undefined}>{u.keys < 0 ? '?' : u.keys}</td>
                  <td className="m">{u.login || '–'}</td>
                  <td className="m">{u.shell}</td>
                  <td><span className="sp-acts">
                    <button aria-label={t('SSH keys of {name}', {name: u.name})} onClick={() => setDialog({kind: 'keys', name: u.name, me})}>{t('Keys')}</button>
                    {u.uid !== 0 && !me && <button className="danger" aria-label={t('Delete {name}', {name: u.name})} onClick={() => setDialog({kind: 'delete', name: u.name, home: u.home})}>{t('Delete')}</button>}
                  </span></td>
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
      {dialog?.kind === 'add' && <AddUser sid={sid} onClose={close} />}
      {dialog?.kind === 'keys' && <Keys sid={sid} name={dialog.name} me={dialog.me} onClose={close} />}
      {dialog?.kind === 'delete' && <DeleteUser sid={sid} name={dialog.name} home={dialog.home} onClose={close} />}
    </div>
  )
}

const nameOk = (s: string) => /^[a-z_][a-z0-9_-]{0,31}$/.test(s)

function AddUser({sid, onClose}: {sid: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [name, setName] = useState('')
  const [shell, setShell] = useState('/bin/bash')
  const [sudo, setSudo] = useState(false)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const go = async () => {
    setBusy(true); setError('')
    try { await be.server.addUser(sid, name, shell, sudo, key.trim()); toast.info(t('{name}: done', {name})); onClose() } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={t('Add user')} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={busy || !nameOk(name)} onClick={go}>{t('Add user')}</button></>}>
      <Err error={error} />
      <Field label={t('User name')}><input autoFocus value={name} spellCheck={false} autoCapitalize="none" onChange={e => setName(e.target.value)} placeholder="deploy" aria-invalid={name !== '' && !nameOk(name)} /></Field>
      {name !== '' && !nameOk(name) && <div className="sp-note">{t('Lower case letters, digits, - and _, starting with a letter (32 at most).')}</div>}
      <Field label={t('Shell')}><Select label={t('Shell')} value={shell} onChange={setShell} options={['/bin/bash', '/bin/sh', '/bin/zsh'].map(v => ({value: v, label: v}))} /></Field>
      <Field label={t('Public key (optional)')}><textarea rows={3} value={key} spellCheck={false} onChange={e => setKey(e.target.value)} placeholder="ssh-ed25519 AAAA… me@laptop" /></Field>
      <label className="sp-check"><input type="checkbox" checked={sudo} onChange={e => setSudo(e.target.checked)} /> {t('May use sudo')}</label>
      <div className="sp-note">{t('The account has no password, so it logs in with a key only. A sudo that asks for a password will not work for it.')}</div>
    </Modal>
  )
}

function Keys({sid, name, me, onClose}: {sid: string; name: string; me: boolean; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [keys, setKeys] = useState<UserKey[] | null>(null)
  const [error, setError] = useState('')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const reload = useCallback(async () => { try { setKeys(await be.server.userKeys(sid, name)) } catch (e) { setError(errMsg(e)); setKeys([]) } }, [be, sid, name])
  useEffect(() => { void reload() }, [reload])
  const run = async (f: () => Promise<void>) => { setBusy(true); setError(''); try { await f(); await reload() } catch (e) { setError(errMsg(e)) } setBusy(false) }
  const add = () => run(async () => { await be.server.addUserKey(sid, name, key.trim()); setKey(''); toast.info(t('{name}: done', {name})) })
  const remove = async (k: UserKey) => {
    const warn = me ? ' ' + t('This is the account of this connection: do not remove the key it uses.') : ''
    if (!(await confirm(t('Remove the key {fp} ({comment}) of {name}? It can no longer log in with it.', {fp: k.fingerprint, comment: k.comment || '–', name}) + warn, {confirmLabel: t('Remove'), danger: true}))) return
    await run(() => be.server.removeUserKey(sid, name, k.fingerprint))
  }
  return (
    <Modal wide title={t('SSH keys of {name}', {name})} onClose={onClose} footer={<button onClick={onClose}>{t('Close')}</button>}>
      <Err error={error} />
      {keys && keys.length === 0 && <div className="sp-empty">{t('No keys.')}</div>}
      {keys && keys.length > 0 && (
        <div className="sp-table"><table>
          <thead><tr><th>{t('Key')}</th><th>{t('Comment')}</th><th /></tr></thead>
          <tbody>{keys.map(k => (
            <tr key={k.fingerprint}>
              <td className="m">{k.type}<div className="sp-note">{k.fingerprint}</div></td>
              <td>{k.comment || '–'}{k.options && <> <span className="sp-pill" title={t('The line has options, such as from= or command=.')}>{t('options')}</span></>}</td>
              <td><span className="sp-acts"><button className="danger" disabled={busy} aria-label={t('Remove the key {fp}', {fp: k.fingerprint})} onClick={() => remove(k)}>{t('Remove')}</button></span></td>
            </tr>
          ))}</tbody>
        </table></div>
      )}
      <Field label={t('Add a public key')}><textarea rows={3} value={key} spellCheck={false} onChange={e => setKey(e.target.value)} placeholder="ssh-ed25519 AAAA… me@laptop" /></Field>
      <div><button className="primary" disabled={busy || key.trim() === ''} onClick={add}>{t('Add key')}</button></div>
    </Modal>
  )
}

function DeleteUser({sid, name, home, onClose}: {sid: string; name: string; home: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [removeHome, setRemoveHome] = useState(false)
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const go = async () => {
    setBusy(true); setError('')
    try { await be.server.deleteUser(sid, name, removeHome); toast.info(t('{name}: done', {name})); onClose() } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  return (
    <Modal title={t('Delete {name}', {name})} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="danger" disabled={busy || typed !== name} onClick={go}>{t('Delete')}</button></>}>
      <Err error={error} />
      <p>{t('The account {name} is deleted and can no longer log in. It is refused while that account has a login open.', {name})}</p>
      <label className="sp-check"><input type="checkbox" checked={removeHome} onChange={e => setRemoveHome(e.target.checked)} /> {t('Also delete its home folder {home} and everything in it', {home})}</label>
      <Field label={t('Type {name} to confirm', {name})}><input value={typed} spellCheck={false} autoCapitalize="none" onChange={e => setTyped(e.target.value)} /></Field>
    </Modal>
  )
}

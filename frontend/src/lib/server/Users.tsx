import {useCallback} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {Card, Err, Toolbar, useLoad} from './shared'
import type {ServerUsers} from '../types'

/** The Users tab: who can log in to the server, who may use sudo, how many SSH keys each one has, and who is logged in now. Read only. */
export function Users({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const load = useCallback(() => be.server.users(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUsers>(load, null)
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('Accounts that can log in')} right={<Toolbar busy={busy} onReload={reload} />}>
        {data && data.users.length === 0 ? <div className="sp-empty">{t('No accounts found.')}</div> : (
          <div className="sp-table"><table>
            <thead><tr><th>{t('User')}</th><th>{t('Sudo')}</th><th className="n">{t('SSH keys')}</th><th>{t('Last login')}</th><th>{t('Shell')}</th></tr></thead>
            <tbody>{(data?.users ?? []).map(u => (
              <tr key={u.name}>
                <td><b>{u.name}</b><div className="sp-note m">{u.home}</div></td>
                <td>{u.sudo ? <span className="sp-pill warn">{t('Yes')}</span> : <span className="sp-note">{t('No')}</span>}</td>
                <td className="n m" title={u.keys < 0 ? t('This login cannot read the file.') : undefined}>{u.keys < 0 ? '?' : u.keys}</td>
                <td className="m">{u.login || '–'}</td>
                <td className="m">{u.shell}</td>
              </tr>
            ))}</tbody>
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
    </div>
  )
}

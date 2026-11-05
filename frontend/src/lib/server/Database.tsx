import {useCallback} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {formatBytes} from '../sftp/format'
import {Card, Err, Toolbar, useLoad} from './shared'
import type {ServerDatabases} from '../types'

const NAMES = {postgres: 'PostgreSQL', mysql: 'MySQL / MariaDB', redis: 'Redis'} as const

/** The Database tab: the PostgreSQL, MySQL/MariaDB and Redis servers on the host, asked a few fixed questions through the local socket. Read only. */
export function Database({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const load = useCallback(() => be.server.databases(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerDatabases>(load, null)
  return (
    <div className="sp-view">
      <Err error={error} />
      <Toolbar busy={busy} onReload={reload} />
      {data && data.engines.length === 0 && <div className="sp-empty">{t('No PostgreSQL, MySQL, MariaDB or Redis found on this server.')}</div>}
      {(data?.engines ?? []).map(e => (
        <Card key={e.kind} title={NAMES[e.kind]}>
          {!e.reachable ? <div className="sp-note">{t('Installed, but it did not answer through the local socket: it is stopped, or it wants a password this panel does not have.')}</div> : (
            <>
              <div className="sp-note">{t('Version {v}', {v: e.version})}{e.max_connections ? ' · ' + t('{n} of {max} connections', {n: e.connections, max: e.max_connections}) : e.connections ? ' · ' + t('{n} connections', {n: e.connections}) : ''}{e.memory ? ' · ' + e.memory : ''}</div>
              {e.databases.length > 0 && (
                <div className="sp-table"><table>
                  <thead><tr><th>{t('Database')}</th><th className="n">{t('Size')}</th></tr></thead>
                  <tbody>{e.databases.map(d => <tr key={d.name}><td><b>{d.name}</b></td><td className="n m">{formatBytes(d.bytes)}</td></tr>)}</tbody>
                </table></div>
              )}
            </>
          )}
        </Card>
      ))}
    </div>
  )
}

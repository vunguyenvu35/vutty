import {useCallback, useMemo, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {formatBytes} from '../sftp/format'
import {Card, Err, FilterInput, Toolbar, useLoad} from './shared'
import type {ServerNetwork} from '../types'

const POLL_MS = 5000

/** The Network tab: the interfaces with their addresses and traffic since boot, and the TCP connections open now. Read only. */
export function Network({sid, visible}: {sid: string; visible: boolean}) {
  const be = useBackend()
  const t = useT()
  const [filter, setFilter] = useState('')
  const load = useCallback(() => be.server.network(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerNetwork>(load, visible ? POLL_MS : null)
  const conns = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (data?.conns ?? []).filter(c => !q || (c.process + ' ' + c.local + ' ' + c.remote).toLowerCase().includes(q))
  }, [data, filter])
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('Interfaces')} right={<Toolbar busy={busy} onReload={reload} />}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Interface')}</th><th>{t('Addresses')}</th><th className="n">{t('Received')}</th><th className="n">{t('Sent')}</th></tr></thead>
          <tbody>{(data?.ifaces ?? []).map(i => (
            <tr key={i.name}><td><b>{i.name}</b></td><td className="m">{i.addrs.join(', ') || '–'}</td><td className="n m">{formatBytes(i.rx)}</td><td className="n m">{formatBytes(i.tx)}</td></tr>
          ))}</tbody>
        </table>{data && data.ifaces.length === 0 && <div className="sp-empty">{t('No interfaces found.')}</div>}</div>
        <div className="sp-note">{t('Traffic is counted since the server started.')}</div>
      </Card>
      <Card title={t('Open connections')} count={data ? (filter.trim() ? `${conns.length} / ${data.total}` : data.total) : undefined} right={<FilterInput label={t('Filter connections')} placeholder={t('Filter connections')} value={filter} onChange={setFilter} />}>
        <div className="sp-table"><table>
          <thead><tr><th>{t('Process')}</th><th>{t('Local')}</th><th>{t('Remote')}</th></tr></thead>
          <tbody>{conns.map((c, i) => <tr key={i}><td>{c.process || '–'}</td><td className="m">{c.local}</td><td className="m">{c.remote}</td></tr>)}</tbody>
        </table>{data && conns.length === 0 && <div className="sp-empty">{t('No open connections.')}</div>}</div>
        {data && data.total > data.conns.length && <div className="sp-note">{t('Showing the first {n}.', {n: data.conns.length})}</div>}
      </Card>
    </div>
  )
}

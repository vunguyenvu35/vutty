import {useCallback, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {formatBytes} from '../sftp/format'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import {backupLevel, formatUptime, validPath} from './format'
import type {Host, ServerBackup, ServerSchedule} from '../types'

/** The Schedule tab: what runs by itself (systemd timers, cron) and whether the backups it makes are still arriving. */
export function Schedule({sid, host, onLogs}: {sid: string; host: Host; onLogs(kind: 'service', name: string): void}) {
  return (
    <div className="sp-view">
      <BackupCheck sid={sid} host={host} />
      <Jobs sid={sid} onLogs={onLogs} />
    </div>
  )
}

/** "unit.service" -> "unit": the name ServiceAction wants. */
const unitName = (u: string) => u.replace(/\.service$/, '')

function Jobs({sid, onLogs}: {sid: string; onLogs(kind: 'service', name: string): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.schedule(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerSchedule>(load, null)
  const run = async (unit: string) => {
    const name = unitName(unit)
    if (!(await confirm(t('Run {name} now? It starts the job outside its schedule.', {name}), {confirmLabel: t('Run now')}))) return
    setWorking(unit)
    try { await be.server.serviceAction(sid, name, 'start'); toast.info(t('{name}: done', {name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <>
      <Card title={t('Timers')} right={<Toolbar busy={busy} onReload={reload} />}>
        <Err error={error} />
        {data && (
          <div className="sp-table"><table>
            <thead><tr><th>{t('Timer')}</th><th>{t('Next run')}</th><th>{t('Last run')}</th><th>{t('Result')}</th><th /></tr></thead>
            <tbody>
              {data.timers.map(x => (
                <tr key={x.name}>
                  <td><b>{x.name.replace(/\.timer$/, '')}</b><div className="sp-note">{x.description}</div></td>
                  <td className="m">{x.next || '–'}</td><td className="m">{x.last || '–'}</td>
                  <td>{x.result === '' ? '–' : x.result === 'success' ? <span className="sp-pill ok">{t('Succeeded')}</span> : <span className="sp-pill bad" title={x.result}>{t('Failed')}</span>}</td>
                  <td><div className="sp-acts">
                    {x.unit.endsWith('.service') && <button aria-busy={working === x.unit} disabled={working === x.unit} onClick={() => void run(x.unit)}>{t('Run now')}</button>}
                    {x.unit.endsWith('.service') && <button onClick={() => onLogs('service', unitName(x.unit))}>{t('Logs')}</button>}
                  </div></td>
                </tr>
              ))}
            </tbody>
          </table>{data.timers.length === 0 && <div className="sp-empty">{t('No timers.')}</div>}</div>
        )}
      </Card>
      <Card title={t('Cron jobs')}>
        {data && (
          <div className="sp-table"><table>
            <thead><tr><th>{t('Schedule')}</th><th>{t('Command')}</th><th>{t('From')}</th></tr></thead>
            <tbody>
              {data.cron.map((c, i) => (
                <tr key={i}>
                  <td className="m">{c.schedule}</td>
                  <td className="sp-cmd" title={c.command}>{c.command}</td>
                  <td className="sp-note">{c.source}{c.user ? ' · ' + c.user : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>{data.cron.length === 0 && <div className="sp-empty">{t('No cron jobs.')}</div>}</div>
        )}
        <div className="sp-note">{t('Times are the server\'s. Cron keeps no record of past runs; use a timer when you need one.')}</div>
      </Card>
    </>
  )
}

/** How long ago the newest file in a backup folder was written. The folder is kept on the host, so the check is the same every time. */
function BackupCheck({sid, host}: {sid: string; host: Host}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [path, setPath] = useState(host.backup_path ?? '')
  const [saved, setSaved] = useState(host.backup_path ?? '')
  const [error, setError] = useState('')
  const save = async () => {
    const p = path.trim()
    if (p !== '' && !validPath(p)) { setError(t('Type an absolute path with letters, digits and . _ - / only.')); return }
    setError('')
    try { await be.hosts.saveHost({...host, backup_path: p}); setSaved(p); toast.info(p ? t('Backup folder saved.') : t('Backup check removed.')) } catch (e) { setError(errMsg(e)) }
  }
  return (
    <Card title={t('Backups')}>
      <form className="sp-bar" onSubmit={e => { e.preventDefault(); void save() }}>
        <input className="sp-filter" aria-label={t('Backup folder on the server')} placeholder={t('Backup folder on the server, e.g. /var/backups')} value={path} onChange={e => setPath(e.target.value)} spellCheck={false} />
        <button type="submit" className="primary" disabled={path.trim() === saved}>{t('Save')}</button>
      </form>
      <Err error={error} />
      {!saved && <div className="sp-note">{t('Name the folder where your backups land. The panel then shows how old the newest file in it is.')}</div>}
      {saved && <BackupResult key={saved} sid={sid} path={saved} />}
    </Card>
  )
}

/** What is in the saved folder: asked again for a new path (the key), and when the refresh button is pressed. */
function BackupResult({sid, path}: {sid: string; path: string}) {
  const be = useBackend()
  const t = useT()
  const load = useCallback(() => be.server.backup(sid, path), [be, sid, path])
  const {data, error, busy, reload} = useLoad<ServerBackup>(load, null)
  const age = data?.found ? Math.max(0, data.now - data.mtime) : 0
  return (
    <>
      <Toolbar busy={busy} onReload={reload} />
      <Err error={error} />
      {data && !data.found && <div className="sp-empty"><span className="sp-pill bad">{t('No backup found')}</span> {t('The folder is empty, missing, or this login cannot read it.')}</div>}
      {data?.found && (
        <table className="sp-kv"><tbody>
          <tr><td>{t('Newest file')}</td><td className="m">{data.file}</td></tr>
          <tr><td>{t('Size')}</td><td>{formatBytes(data.size)}</td></tr>
          <tr><td>{t('Written')}</td><td><span className={'sp-pill ' + backupLevel(age)}>{t('{time} ago', {time: formatUptime(age)})}</span></td></tr>
        </tbody></table>
      )}
    </>
  )
}

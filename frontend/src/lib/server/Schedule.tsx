import {useCallback, useState} from 'react'
import {Field, Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {formatBytes} from '../sftp/format'
import {openInSftp} from '../sftp/openRequest'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import {backupLevel, formatUptime, validPath} from './format'
import type {Host, ServerBackup, ServerCron, ServerSchedule} from '../types'

/** The Schedule tab: what runs by itself (systemd timers, cron) and whether the backups it makes are still arriving. */
export function Schedule({sid, host, onLogs}: {sid: string; host: Host; onLogs(kind: 'service', name: string): void}) {
  return (
    <div className="sp-view">
      <BackupCheck sid={sid} host={host} />
      <Jobs sid={sid} onLogs={onLogs} />
    </div>
  )
}

/** a job of the crontab of the login or of root: the files of /etc are not edited */
const editable = (c: ServerCron) => c.source === 'crontab' || c.source === 'root'
const ownerName = (source: string, me?: string) => (source === 'crontab' ? me || source : source)

const PRESETS: [string, string][] = [['Every minute', '* * * * *'], ['Every 5 minutes', '*/5 * * * *'], ['Every hour', '0 * * * *'], ['Every day at 03:00', '0 3 * * *'], ['Every Sunday at 03:00', '0 3 * * 0'], ['The 1st of the month at 03:00', '0 3 1 * *'], ['At every start of the server', '@reboot']]

/** "unit.service" -> "unit": the name ServiceAction wants. */
const unitName = (u: string) => u.replace(/\.service$/, '')

function Jobs({sid, onLogs}: {sid: string; onLogs(kind: 'service', name: string): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const [form, setForm] = useState<{job?: ServerCron} | null>(null)
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
  const dropJob = async (c: ServerCron) => {
    if (!(await confirm(t('Delete the job {job} from the crontab of {owner}?', {job: c.schedule + ' ' + c.command, owner: ownerName(c.source, data?.me)}), {confirmLabel: t('Delete'), danger: true}))) return
    try { await be.server.cronDelete(sid, c.source, c.schedule, c.command); toast.info(t('Job deleted.')) } catch (e) { toast.error(errMsg(e)) }
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
      <Card title={t('Cron jobs')} right={<button onClick={() => setForm({})}>{t('Add job')}</button>}>
        {data && (
          <div className="sp-table"><table>
            <thead><tr><th>{t('Schedule')}</th><th>{t('Command')}</th><th>{t('From')}</th><th /></tr></thead>
            <tbody>
              {data.cron.map((c, i) => (
                <tr key={i}>
                  <td className="m">{c.schedule}</td>
                  <td className="sp-cmd" title={c.command}>{c.command}</td>
                  <td className="sp-note">{c.source}{c.user ? ' · ' + c.user : ''}</td>
                  <td>{editable(c) && <div className="sp-acts">
                    <button aria-label={t('Edit the job {job}', {job: c.command})} onClick={() => setForm({job: c})}>{t('Edit')}</button>
                    <button className="danger" aria-label={t('Delete the job {job}', {job: c.command})} onClick={() => void dropJob(c)}>{t('Delete')}</button>
                  </div>}</td>
                </tr>
              ))}
            </tbody>
          </table>{data.cron.length === 0 && <div className="sp-empty">{t('No cron jobs.')}</div>}</div>
        )}
        <div className="sp-note">{t('Times are the server\'s. Cron keeps no record of past runs; use a timer when you need one.')}</div>
        <div className="sp-note">{t('Only the crontab of the login and of root can be edited here; the files of /etc are shown and left alone.')}</div>
      </Card>
      {form && <CronForm sid={sid} job={form.job} me={data?.me ?? ''} onClose={() => { setForm(null); reload() }} />}
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
        {saved && <button type="button" onClick={() => openInSftp(host, saved)}>{t('Open in SFTP')}</button>}
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

/** Add a job, or change one (the job is found again by its old schedule and command). */
function CronForm({sid, job, me, onClose}: {sid: string; job?: ServerCron; me: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const owners = me === 'root' ? [{value: 'root', label: 'root'}] : [{value: 'crontab', label: me || t('This login')}, {value: 'root', label: 'root (sudo)'}]
  const [owner, setOwner] = useState(job?.source ?? owners[0].value)
  const [schedule, setSchedule] = useState(job?.schedule ?? '0 3 * * *')
  const [command, setCommand] = useState(job?.command ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const go = async () => {
    setBusy(true); setError('')
    try {
      if (job) await be.server.cronUpdate(sid, owner, job.schedule, job.command, schedule.trim(), command.trim())
      else await be.server.cronAdd(sid, owner, schedule.trim(), command.trim())
      toast.info(t('Job saved.')); onClose()
    } catch (e) { setError(errMsg(e)); setBusy(false) }
  }
  const title = job ? t('Edit job') : t('Add job')
  return (
    <Modal title={title} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={busy || schedule.trim() === '' || command.trim() === ''} onClick={go}>{t('Save')}</button></>}>
      <Err error={error} />
      {!job && <Field label={t('Crontab of')}><Select label={t('Crontab of')} value={owner} onChange={setOwner} options={owners} /></Field>}
      <Field label={t('Ready-made times')}><Select label={t('Ready-made times')} value="" onChange={v => v && setSchedule(v)} options={[{value: '', label: t('Pick one to fill the schedule')}, ...PRESETS.map(([l, v]) => ({value: v, label: t(l) + ' · ' + v}))]} /></Field>
      <Field label={t('Schedule')}><input value={schedule} spellCheck={false} onChange={e => setSchedule(e.target.value)} placeholder="0 3 * * *" /></Field>
      <div className="sp-note">{t('Minute, hour, day of the month, month, day of the week; or @reboot, @daily …')}</div>
      <Field label={t('Command')}><input value={command} spellCheck={false} onChange={e => setCommand(e.target.value)} placeholder="/usr/local/bin/backup.sh >> /var/log/backup.log 2>&1" /></Field>
      <div className="sp-note">{t('One line. A % in a command must be written \\%. The server checks the whole crontab and refuses a wrong one.')}</div>
    </Modal>
  )
}

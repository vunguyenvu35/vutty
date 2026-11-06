import {useCallback, useState} from 'react'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {formatBytes} from '../sftp/format'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import {formatUptime, level, pct, validPath} from './format'
import {openInSftp} from '../sftp/openRequest'
import type {Host, ServerCleanup, ServerFolder, ServerReclaim, ServerUpdates} from '../types'

/** The Maintenance tab: what is waiting to be updated, where the disk went, and what can be cleaned without losing data. */
export function Maintenance({sid, host}: {sid: string; host: Host}) {
  return (
    <div className="sp-view">
      <Updates sid={sid} />
      <Cleanup sid={sid} />
      <Folders sid={sid} host={host} />
    </div>
  )
}

function Updates({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState(false)
  const load = useCallback(() => be.server.updates(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUpdates>(load, null)
  const refresh = async () => {
    if (!(await confirm(t('Refresh the package lists? This downloads the lists of packages and installs nothing.'), {confirmLabel: t('Refresh lists')}))) return
    setWorking(true)
    try { await be.server.updatesRefresh(sid); toast.info(t('The package lists are up to date.')) } catch (e) { toast.error(errMsg(e)) }
    setWorking(false)
    reload()
  }
  return (
    <Card title={t('Updates')} right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && !data.manager && <div className="sp-empty">{t('No package manager that the panel knows (apt, dnf, yum) was found.')}</div>}
      {data && data.manager && (
        <>
          <div className="sp-bar">
            <span className={'sp-pill ' + (data.security > 0 ? 'bad' : data.count > 0 ? 'warn' : 'ok')}>{data.count === 0 ? t('Up to date') : tn(data.count, '{n} update waiting', '{n} updates waiting', {n: data.count})}</span>
            {data.security > 0 && <span className="sp-pill bad">{tn(data.security, '{n} security update', '{n} security updates', {n: data.security})}</span>}
            {data.reboot_required && <span className="sp-pill warn">{t('Needs a reboot')}</span>}
            <span className="sp-os">{data.lists_age_sec >= 0 ? t('Package lists refreshed {time} ago.', {time: formatUptime(data.lists_age_sec)}) : t('Age of the package lists unknown.')}</span>
            <button aria-busy={working} disabled={working} onClick={() => void refresh()}>{t('Refresh lists')}</button>
          </div>
          {data.packages.length > 0 && (
            <div className="sp-table"><table>
              <thead><tr><th>{t('Package')}</th><th>{t('New version')}</th><th /></tr></thead>
              <tbody>
                {data.packages.map(p => (
                  <tr key={p.name}><td><b>{p.name}</b></td><td className="m">{p.version}</td><td>{p.security && <span className="sp-pill bad">{t('Security')}</span>}</td></tr>
                ))}
              </tbody>
            </table></div>
          )}
          <div className="sp-note">{t('Updates are installed in the terminal; the panel only shows what is waiting.')}</div>
        </>
      )}
    </Card>
  )
}

function Cleanup({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.cleanup(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerCleanup>(load, null)
  const names: Record<ServerReclaim['id'], [string, string]> = {
    'docker-dangling': [t('Unused Docker layers'), t('Images without a name and the build cache. Nothing that runs or is tagged.')],
    'docker-images': [t('All unused Docker images'), t('Every image no container uses. They are downloaded again when needed.')],
    journal: [t('System journal'), t('Old log entries, keeping the newest 200 MB.')],
    'apt-cache': [t('Package cache'), t('Downloaded package files that are already installed.')],
    'docker-volumes': [t('Docker volumes'), t('Volumes no container uses. They can hold data, so the panel never removes them: check them in the terminal.')],
  }
  const clean = async (it: ServerReclaim) => {
    const [name] = names[it.id]
    const message = it.size > 0 ? t('Clean {what}? This frees about {size}.', {what: name, size: formatBytes(it.size)}) : t('Clean {what}?', {what: name})
    if (!(await confirm(message, {confirmLabel: t('Clean'), danger: true}))) return
    setWorking(it.id)
    try { await be.server.cleanupRun(sid, it.id); toast.info(t('{name}: done', {name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <Card title={t('Disk and cleanup')} right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && (
        <>
          <div className="sp-table"><table>
            <thead><tr><th>{t('Disk')}</th><th>{t('Used')}</th><th className="n">{t('Free')}</th></tr></thead>
            <tbody>
              {data.disks.map(d => {
                const p = pct(d.used, d.total)
                return (
                  <tr key={d.mount}>
                    <td><b className="m">{d.mount}</b><div className="sp-note m">{d.device}</div></td>
                    <td className="sp-meter"><div className={'sp-track ' + level(p)} role="meter" aria-label={t('Used space of {mount}', {mount: d.mount})} aria-valuemin={0} aria-valuemax={100} aria-valuenow={p}><i style={{width: p + '%'}} /></div><span className="sp-note">{p}% · {formatBytes(d.used)} / {formatBytes(d.total)}</span></td>
                    <td className="n">{formatBytes(Math.max(0, d.total - d.used))}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>{data.disks.length === 0 && <div className="sp-empty">{t('No disks found.')}</div>}</div>
          {data.items.length > 0 && (
            <div className="sp-table"><table>
              <thead><tr><th>{t('Can be cleaned')}</th><th className="n">{t('Frees about')}</th><th /></tr></thead>
              <tbody>
                {data.items.map(it => (
                  <tr key={it.id}>
                    <td><b>{names[it.id]?.[0] ?? it.id}</b><div className="sp-note">{names[it.id]?.[1] ?? it.note}</div></td>
                    <td className="n">{it.size > 0 ? formatBytes(it.size) : '–'}</td>
                    <td><div className="sp-acts">{it.id !== 'docker-volumes' && <button className="danger" aria-busy={working === it.id} disabled={working === it.id} onClick={() => void clean(it)}>{t('Clean')}</button>}</div></td>
                  </tr>
                ))}
              </tbody>
            </table></div>
          )}
        </>
      )}
    </Card>
  )
}

function Folders({sid, host}: {sid: string; host: Host}) {
  const be = useBackend()
  const t = useT()
  const [path, setPath] = useState('/var')
  const [shown, setShown] = useState('')
  const [rows, setRows] = useState<ServerFolder[] | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const scan = async (p: string) => {
    if (!validPath(p)) { setError(t('Type an absolute path with letters, digits and . _ - / only.')); return }
    setBusy(true)
    setError('')
    setPath(p)
    try { setRows(await be.server.diskUsage(sid, p)); setShown(p) } catch (e) { setError(errMsg(e)) }
    setBusy(false)
  }
  const top = Math.max(1, ...(rows ?? []).map(r => r.size))
  const parent = shown.replace(/\/[^/]*\/?$/, '') || '/'
  return (
    <Card title={t('Where the space went')}>
      <form className="sp-bar" onSubmit={e => { e.preventDefault(); void scan(path) }}>
        <input className="sp-filter" aria-label={t('Folder to look into')} value={path} onChange={e => setPath(e.target.value)} spellCheck={false} />
        <button type="submit" className="primary" disabled={busy}>{busy ? t('Scanning…') : t('Scan')}</button>
        {rows && shown !== '/' && <button type="button" disabled={busy} onClick={() => void scan(parent)}>{t('Up')}</button>}
        {validPath(path) && <button type="button" onClick={() => openInSftp(host, path)}>{t('Open in SFTP')}</button>}
      </form>
      <Err error={error} />
      {rows && (
        <div className="sp-table"><table>
          <thead><tr><th>{t('Folder')}</th><th>{t('Size')}</th></tr></thead>
          <tbody>
            {rows.map(r => (
              <tr key={r.path}>
                <td className="m"><button className="sp-link" onClick={() => void scan(r.path)} disabled={busy}>{r.path}</button></td>
                <td className="sp-meter"><div className="sp-track"><i style={{width: Math.round((r.size / top) * 100) + '%'}} /></div><span className="sp-note">{formatBytes(r.size)}</span></td>
              </tr>
            ))}
          </tbody>
        </table>{rows.length === 0 && <div className="sp-empty">{t('Nothing here, or this login cannot read it.')}</div>}</div>
      )}
      <div className="sp-note">{t('Looking into big folders can take a while. The panel reads sizes only.')}</div>
    </Card>
  )
}

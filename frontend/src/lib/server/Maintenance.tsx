import {useCallback, useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {formatBytes} from '../sftp/format'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import {formatUptime, level, pct, validPath} from './format'
import {openInSftp} from '../sftp/openRequest'
import {Modal} from '../ui/Modal'
import type {Host, ServerCleanup, ServerFolder, ServerReclaim, ServerSystem, ServerUpdates, ServerUpgrade} from '../types'

/** The Maintenance tab: what is waiting to be updated, where the disk went, and what can be cleaned without losing data. */
export function Maintenance({sid, host, visible = true}: {sid: string; host: Host; visible?: boolean}) {
  return (
    <div className="sp-view">
      <System sid={sid} />
      <Updates sid={sid} />
      <Cleanup sid={sid} />
      <Folders sid={sid} host={host} visible={visible} />
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
  const [installing, setInstalling] = useState(false)
  const load = useCallback(() => be.server.updates(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUpdates>(load, null)
  // an upgrade that was started before (and runs on) is picked up again when the tab is opened
  useEffect(() => { void be.server.updatesInstallStatus(sid).then(s => { if (s.running) setInstalling(true) }).catch(() => {}) }, [be, sid])
  const install = async () => {
    if (!(await confirm(t('Install {n} updates? It runs on the server in the background, takes a few minutes, and goes on if you close this panel. Services may restart; connections to them can drop for a moment. A reboot may be needed afterwards.', {n: data?.count ?? 0}), {confirmLabel: t('Install updates'), danger: true}))) return
    try { await be.server.updatesInstall(sid); setInstalling(true) } catch (e) { toast.error(errMsg(e)) }
  }
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
      {installing && <UpgradeProgress sid={sid} onClose={() => { setInstalling(false); reload() }} />}
      {data && !data.manager && <div className="sp-empty">{t('No package manager that the panel knows (apt, dnf, yum) was found.')}</div>}
      {data && data.manager && (
        <>
          <div className="sp-bar">
            <span className={'sp-pill ' + (data.security > 0 ? 'bad' : data.count > 0 ? 'warn' : 'ok')}>{data.count === 0 ? t('Up to date') : tn(data.count, '{n} update waiting', '{n} updates waiting', {n: data.count})}</span>
            {data.security > 0 && <span className="sp-pill bad">{tn(data.security, '{n} security update', '{n} security updates', {n: data.security})}</span>}
            {data.reboot_required && <span className="sp-pill warn">{t('Needs a reboot')}</span>}
            <span className="sp-os">{data.lists_age_sec >= 0 ? t('Package lists refreshed {time} ago.', {time: formatUptime(data.lists_age_sec)}) : t('Age of the package lists unknown.')}</span>
            <button aria-busy={working} disabled={working} onClick={() => void refresh()}>{t('Refresh lists')}</button>
            {data.count > 0 && <button className="primary" onClick={() => void install()}>{t('Install updates')}</button>}
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

function Folders({sid, host, visible}: {sid: string; host: Host; visible: boolean}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const seen = useRef(visible) // whether the section is in front, read when the scan ends (the scan goes on while another section is shown)
  seen.current = visible
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
    try {
      setRows(await be.server.diskUsage(sid, p)); setShown(p)
      if (!seen.current) toast.info(t('{host}: the scan of {path} is done.', {host: host.label, path: p}))
    } catch (e) {
      setError(errMsg(e))
      if (!seen.current) toast.error(t('{host}: the scan of {path} failed: {error}', {host: host.label, path: p, error: errMsg(e)}))
    }
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

/** The System box: the name of the machine, its time zone and its swap. Each change asks first. */
function System({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const load = useCallback(() => be.server.system(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerSystem>(load, null)
  const [host, setHost] = useState('')
  const [zone, setZone] = useState('')
  const [mb, setMb] = useState('')
  const [working, setWorking] = useState('')
  useEffect(() => { if (data) { setHost(data.hostname); setZone(data.timezone) } }, [data?.hostname, data?.timezone])
  const act = async (key: string, fn: () => Promise<void>, done: string) => {
    setWorking(key)
    try { await fn(); toast.info(done) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  const saveHost = async () => {
    if (!(await confirm(t('Rename the server to {name}? Programs that read the old name keep using it until they restart.', {name: host.trim()}), {confirmLabel: t('Rename')}))) return
    await act('host', () => be.server.setHostname(sid, host.trim()), t('The hostname is {name}.', {name: host.trim()}))
  }
  const saveZone = async () => {
    if (!(await confirm(t('Set the time zone to {zone}? Logs and cron jobs follow the server\'s time.', {zone: zone.trim()}), {confirmLabel: t('Change')}))) return
    await act('zone', () => be.server.setTimezone(sid, zone.trim()), t('The time zone is {zone}.', {zone: zone.trim()}))
  }
  const size = Number(mb)
  const sizeOk = Number.isInteger(size) && size >= 256 && size <= 32768
  const createSwap = async () => {
    if (!(await confirm(t('Create a swap file of {size}? It takes that much disk and is turned on at every boot.', {size: formatBytes(size * 1024 ** 2)}), {confirmLabel: t('Create')}))) return
    await act('swap', () => be.server.swapCreate(sid, size), t('The swap file is on.'))
  }
  const removeSwap = async () => {
    if (!(await confirm(t('Remove the swap file? Memory in it moves back to RAM first; the server refuses if that does not fit.'), {confirmLabel: t('Remove'), danger: true}))) return
    await act('swap', () => be.server.swapRemove(sid), t('The swap file is removed.'))
  }
  return (
    <Card title={t('System')} right={<Toolbar busy={busy} onReload={reload} />}>
      <Err error={error} />
      {data && (
        <table className="sp-kv"><tbody>
          <tr><td>{t('Hostname')}</td><td>
            {data.systemd ? <form className="sp-bar" onSubmit={e => { e.preventDefault(); void saveHost() }}>
              <input className="sp-filter" aria-label={t('Hostname')} value={host} spellCheck={false} onChange={e => setHost(e.target.value)} />
              <button type="submit" disabled={working === 'host' || host.trim() === data.hostname || host.trim() === ''}>{t('Rename')}</button>
            </form> : <span className="m">{data.hostname}</span>}
          </td></tr>
          <tr><td>{t('Time zone')}</td><td>
            {data.systemd ? <form className="sp-bar" onSubmit={e => { e.preventDefault(); void saveZone() }}>
              <input className="sp-filter" aria-label={t('Time zone')} value={zone} spellCheck={false} placeholder="Asia/Ho_Chi_Minh" onChange={e => setZone(e.target.value)} />
              <button type="submit" disabled={working === 'zone' || zone.trim() === data.timezone || zone.trim() === ''}>{t('Change')}</button>
            </form> : <span className="m">{data.timezone || '–'}</span>}
          </td></tr>
          <tr><td>{t('Swap')}</td><td>
            <div className="sp-bar">
              <span className={'sp-pill ' + (data.swap_total > 0 ? 'ok' : 'warn')}>{data.swap_total > 0 ? formatBytes(data.swap_total) : t('None')}</span>
              {data.swap_other && <span className="sp-note">{t('Includes swap that is not a file; it is left alone.')}</span>}
              {data.swap_file > 0
                ? <button className="danger" disabled={working === 'swap'} onClick={() => void removeSwap()}>{t('Remove swap file')}</button>
                : <>
                  <input className="sp-filter" aria-label={t('Swap file size in MB')} inputMode="numeric" placeholder={t('Size in MB, e.g. {mb}', {mb: String(Math.max(256, Math.round(data.ram / 1024 ** 2 / 256) * 256))})} value={mb} onChange={e => setMb(e.target.value)} />
                  <button disabled={working === 'swap' || !sizeOk} onClick={() => void createSwap()}>{t('Create swap file')}</button>
                </>}
            </div>
            <div className="sp-note">{t('RAM {ram}, free disk {disk}.', {ram: formatBytes(data.ram), disk: formatBytes(data.disk_free)})}</div>
          </td></tr>
        </tbody></table>
      )}
      <div className="sp-note">{t('The swap file is /swapfile: 256 MB to 32 GB, and 1 GB of disk must stay free.')}</div>
    </Card>
  )
}

/** How far the upgrade is: the log of the server read every 2 seconds, until it has ended. It runs on the server, so closing this does not stop it. */
function UpgradeProgress({sid, onClose}: {sid: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const [s, setS] = useState<ServerUpgrade | null>(null)
  const [error, setError] = useState('')
  const pre = useRef<HTMLPreElement>(null)
  useEffect(() => {
    let alive = true
    let timer = 0
    const tick = async () => {
      try {
        const r = await be.server.updatesInstallStatus(sid)
        if (!alive) return
        setS(r); setError('')
        if (r.running || !r.done) timer = window.setTimeout(() => void tick(), 2000)
      } catch (e) {
        if (!alive) return
        setError(errMsg(e)) // the connection dropped: the upgrade itself goes on, so keep asking
        timer = window.setTimeout(() => void tick(), 4000)
      }
    }
    void tick()
    return () => { alive = false; window.clearTimeout(timer) }
  }, [be, sid])
  useEffect(() => { if (pre.current) pre.current.scrollTop = pre.current.scrollHeight }, [s?.log])
  const done = !!s?.done
  return (
    <Modal wide title={t('Installing updates')} onClose={onClose} footer={<button className={done ? 'primary' : ''} onClick={onClose}>{done ? t('Close') : t('Hide: it goes on')}</button>}>
      <div className="sp-bar sp-upgrade-head">
        {!s && <span className="sp-pill">{t('Starting…')}</span>}
        {s?.running && <span className="sp-pill warn">{t('Running')}</span>}
        {done && (s!.ok ? <span className="sp-pill ok">{t('Finished')}</span> : <span className="sp-pill bad">{t('Failed (exit code {code})', {code: s!.code})}</span>)}
      </div>
      <Err error={error} />
      <pre ref={pre} className={'sp-pre' + (done && !s!.ok ? ' bad' : '')} role="log" aria-label={t('Upgrade log')} style={{maxHeight: 340, overflow: 'auto'}}>{s?.log || t('Waiting for the first line…')}</pre>
      {done && s!.ok && <div className="sp-note">{t('Done. If the Updates box says a reboot is needed, use Reboot at the top of the panel when it suits you.')}</div>}
      {done && !s!.ok && <div className="sp-note">{t('The package manager stopped. Read the end of the log above; it can be started again from the Updates box.')}</div>}
      {!done && <div className="sp-note">{t('It runs on the server: you can hide this, close the panel, and come back to see how far it is.')}</div>}
    </Modal>
  )
}

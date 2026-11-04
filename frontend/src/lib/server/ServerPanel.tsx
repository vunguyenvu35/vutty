import {useCallback, useEffect, useMemo, useRef, useState, type ReactNode} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {IconRefresh, IconTerminal} from '../ui/Icons'
import {formatBytes} from '../sftp/format'
import {errMsg} from '../util'
import {actionQuestion, useLoad, useServerConnection, type ServerAction} from './shared'
import {formatRate, formatUptime, level, logLevel, pct} from './format'
import type {Host, ServerContainers, ServerOverview, ServerPorts, ServerProcess, ServerUnit} from '../types'

type Section = 'overview' | 'processes' | 'services' | 'docker' | 'logs' | 'ports'
interface LogSource {kind: '' | 'service' | 'container'; name: string}

const POLL_MS = 5000
const HISTORY = 40

/**
 * A Server tab: the whole of a server in one page (load, processes, services, Docker, logs, ports and the firewall), over an SSH
 * connection of its own. It is what "Panel" opens from the protocol screen. The same data and actions as the Server section of
 * the sidebar, laid out in tables for the width of a tab.
 */
export function ServerPanel({host, visible, onAccess}: {host: Host; visible: boolean; onAccess?(): void}) {
  const t = useT()
  const {phase, sid, notice} = useServerConnection(host)
  const [section, setSection] = useState<Section>('overview')
  const [source, setSource] = useState<LogSource>({kind: '', name: ''})
  const sections: [Section, string][] = [['overview', t('Overview')], ['processes', t('Processes')], ['services', t('Services')], ['docker', t('Docker')], ['logs', t('Logs')], ['ports', t('Ports & firewall')]]
  const showLogs = (s: LogSource) => { setSource(s); setSection('logs') }
  return (
    <div className="sp" role="region" aria-label={t('Server panel of {name}', {name: host.label})}>
      <div className="sp-head">
        <h1>{host.label}</h1>
        <span>{host.username ? host.username + '@' : ''}{host.hostname}{host.port !== 22 ? ':' + host.port : ''}</span>
        {onAccess && <button className="sp-access" onClick={onAccess} title={t('Open a terminal on this host in a new tab')}><IconTerminal size={14} /> {t('Access host')}</button>}
      </div>
      {phase === 'ready' && sid ? (
        <>
          <div className="sp-tabs" role="tablist" aria-label={t('Server sections')}>
            {sections.map(([id, label]) => (
              <button key={id} role="tab" aria-selected={section === id} onClick={() => setSection(id)}>{label}</button>
            ))}
          </div>
          <div className="sp-body">
            {section === 'overview' && <Overview sid={sid} visible={visible} onGo={setSection} />}
            {section === 'processes' && <Processes sid={sid} visible={visible} />}
            {section === 'services' && <Services sid={sid} onLogs={name => showLogs({kind: 'service', name})} />}
            {section === 'docker' && <Docker sid={sid} onLogs={name => showLogs({kind: 'container', name})} />}
            {section === 'logs' && <Logs sid={sid} source={source} onSource={setSource} />}
            {section === 'ports' && <Ports sid={sid} sshPort={host.port} />}
          </div>
        </>
      ) : <div className="sp-notice">{notice}</div>}
    </div>
  )
}

function Bar({label, value, note}: {label: string; value: number; note: string}) {
  return (
    <div className="sp-card">
      <h3>{label}</h3>
      <div className="sp-big">{value}<small>%</small></div>
      <div className={'sp-track ' + level(value)} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}><i style={{width: value + '%'}} /></div>
      <div className="sp-note">{note}</div>
    </div>
  )
}

function Spark({data, max}: {data: number[]; max: number}) {
  if (data.length < 2) return <div className="sp-spark" aria-hidden="true" />
  const w = 240, h = 44
  const top = Math.max(max, ...data, 1)
  const pts = data.map((v, i) => [(i / (data.length - 1)) * w, h - 3 - (v / top) * (h - 8)] as const)
  const line = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join(' ')
  const last = pts[pts.length - 1]
  return (
    <svg className="sp-spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      <path d={`${line} L${last[0].toFixed(1)} ${h} L${pts[0][0].toFixed(1)} ${h}Z`} fill="currentColor" fillOpacity=".12" />
      <path d={line} fill="none" stroke="currentColor" strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

function Bar2({busy, onReload, children}: {busy: boolean; onReload(): void; children?: ReactNode}) {
  const t = useT()
  return (
    <div className="sp-bar">
      {children}
      <button className="icon ghost" aria-label={t('Refresh')} title={t('Refresh')} aria-busy={busy} onClick={onReload}><IconRefresh size={14} /></button>
    </div>
  )
}

const Err = ({error}: {error: string}) => (error ? <div className="error" role="alert">{error}</div> : null)

function Overview({sid, visible, onGo}: {sid: string; visible: boolean; onGo(s: Section): void}) {
  const be = useBackend()
  const t = useT()
  const [cpu, setCpu] = useState<number[]>([])
  const [net, setNet] = useState<number[]>([])
  const load = useCallback(async () => {
    const o = await be.server.overview(sid)
    setCpu(h => [...h, o.cpu_pct].slice(-HISTORY))
    setNet(h => [...h, o.net_rx + o.net_tx].slice(-HISTORY))
    return o
  }, [be, sid])
  const {data: o, error, busy, reload} = useLoad<ServerOverview>(load, visible ? POLL_MS : null)
  const loadUnits = useCallback(() => be.server.services(sid), [be, sid])
  const {data: units} = useLoad<ServerUnit[]>(loadUnits, null)
  if (!o) return <><Err error={error} /><div className="muted">{t('Loading…')}</div></>
  const ram = pct(o.mem_used, o.mem_total), disk = pct(o.disk_used, o.disk_total)
  const failed = (units ?? []).filter(u => u.active === 'failed')
  const attention: {key: string; tone: 'bad' | 'warn'; text: string; go: Section}[] = [
    ...failed.map(u => ({key: 'u' + u.name, tone: 'bad' as const, text: t('{name} has failed', {name: u.name}), go: 'services' as const})),
    ...(disk >= 75 ? [{key: 'disk', tone: disk >= 90 ? 'bad' as const : 'warn' as const, text: t('The disk is {n}% full', {n: disk}), go: 'overview' as const}] : []),
    ...(ram >= 90 ? [{key: 'ram', tone: 'warn' as const, text: t('Memory is {n}% used', {n: ram}), go: 'processes' as const}] : []),
  ]
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><span className="sp-os">{o.os} · {t('up {time}', {time: formatUptime(o.uptime_sec)})}</span></Bar2>
      <Err error={error} />
      <div className="sp-grid">
        <div className="sp-card"><h3>{t('CPU')} · {t('{n} cores', {n: o.cores})}</h3><div className="sp-big">{Math.round(o.cpu_pct)}<small>%</small></div><Spark data={cpu} max={100} /></div>
        <Bar label={t('Memory')} value={ram} note={`${formatBytes(o.mem_used)} / ${formatBytes(o.mem_total)}${o.swap_total ? ' · ' + t('Swap') + ' ' + formatBytes(o.swap_used) + ' / ' + formatBytes(o.swap_total) : ''}`} />
        <Bar label={t('Disk /')} value={disk} note={`${formatBytes(o.disk_used)} / ${formatBytes(o.disk_total)}`} />
        <div className="sp-card"><h3>{t('Network')}</h3><div className="sp-big sp-big-sm">↓ {formatRate(o.net_rx)}</div><div className="sp-note">↑ {formatRate(o.net_tx)}</div><Spark data={net} max={1024} /></div>
      </div>
      <div className="sp-grid sp-grid-2">
        <div className="sp-card">
          <h3>{t('Needs attention')}</h3>
          {attention.length === 0 && <div className="sp-note">{units ? t('Nothing needs attention.') : t('Loading…')}</div>}
          {attention.map(a => (
            <div key={a.key} className="sp-attn">
              <span><span className={'sp-pill ' + a.tone}>{a.tone === 'bad' ? t('Problem') : t('Warning')}</span> {a.text}</span>
              {a.go !== 'overview' && <button onClick={() => onGo(a.go)}>{t('Open')}</button>}
            </div>
          ))}
        </div>
        <div className="sp-card">
          <h3>{t('Details')}</h3>
          <table className="sp-kv"><tbody>
            <tr><td>{t('System')}</td><td>{o.os}</td></tr>
            <tr><td>{t('Kernel')}</td><td>{o.kernel}</td></tr>
            <tr><td>{t('Load average')}</td><td>{o.load.map(x => x.toFixed(2)).join(' · ')}</td></tr>
            <tr><td>{t('Uptime')}</td><td>{formatUptime(o.uptime_sec)}</td></tr>
          </tbody></table>
          {!o.root && <div className="sp-note">{t('Changes use sudo without a password. If the login cannot, they fail with the server\'s message.')}</div>}
        </div>
      </div>
    </div>
  )
}

function Processes({sid, visible}: {sid: string; visible: boolean}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [filter, setFilter] = useState('')
  const [killing, setKilling] = useState(0)
  const load = useCallback(() => be.server.processes(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerProcess[]>(load, visible ? POLL_MS : null)
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (data ?? []).filter(p => !q || p.command.toLowerCase().includes(q) || p.user.toLowerCase().includes(q) || String(p.pid) === q)
  }, [data, filter])
  const kill = async (p: ServerProcess, signal: 'TERM' | 'KILL') => {
    setKilling(0)
    try { await be.server.kill(sid, p.pid, signal); toast.info(t('Sent {signal} to {pid}', {signal, pid: p.pid})); reload() } catch (e) { toast.error(errMsg(e)) }
  }
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><input className="sp-filter" aria-label={t('Filter processes')} placeholder={t('Filter processes')} value={filter} onChange={e => setFilter(e.target.value)} /></Bar2>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr><th>{t('PID')}</th><th>{t('Process')}</th><th>{t('User')}</th><th className="n">{t('CPU')}</th><th className="n">{t('RAM')}</th><th /></tr></thead>
        <tbody>
          {shown.map(p => (
            <tr key={p.pid}>
              <td className="m">{p.pid}</td><td className="sp-cmd" title={p.command}>{p.command}</td><td className="m">{p.user}</td>
              <td className="n">{p.cpu.toFixed(1)}%</td><td className="n">{p.mem.toFixed(1)}%</td>
              <td><div className="sp-acts">
                {killing === p.pid ? (
                  <>
                    <button className="danger" onClick={() => void kill(p, 'TERM')}>{t('End process')}</button>
                    <button className="danger" onClick={() => void kill(p, 'KILL')} title={t('It cannot clean up.')}>{t('Force kill')}</button>
                    <button onClick={() => setKilling(0)}>{t('Cancel')}</button>
                  </>
                ) : <button className="danger" aria-label={t('End process {pid} ({cmd})?', {pid: p.pid, cmd: p.command.split(' ')[0]})} onClick={() => setKilling(p.pid)}>{t('End')}</button>}
              </div></td>
            </tr>
          ))}
        </tbody>
      </table>{data && shown.length === 0 && <div className="sp-empty">{t('No process matches.')}</div>}</div>
    </div>
  )
}

function Services({sid, onLogs}: {sid: string; onLogs(name: string): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [filter, setFilter] = useState('')
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.services(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerUnit[]>(load, null)
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (data ?? []).filter(u => !q || u.name.toLowerCase().includes(q) || u.description.toLowerCase().includes(q))
  }, [data, filter])
  const act = async (u: ServerUnit, action: ServerAction) => {
    const q = actionQuestion(t, 'service', action, u.name)
    if (!(await confirm(q.message, {confirmLabel: q.label, danger: action !== 'start'}))) return
    setWorking(u.name)
    try { await be.server.serviceAction(sid, u.name, action); toast.info(t('{name}: done', {name: u.name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><input className="sp-filter" aria-label={t('Filter services')} placeholder={t('Filter services')} value={filter} onChange={e => setFilter(e.target.value)} /></Bar2>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr><th>{t('Service')}</th><th>{t('Status')}</th><th>{t('Startup')}</th><th /></tr></thead>
        <tbody>
          {shown.map(u => {
            const running = u.active === 'active' && u.sub === 'running'
            return (
              <tr key={u.name}>
                <td><b>{u.name}</b><div className="sp-note">{u.description}</div></td>
                <td>{u.active === 'failed' ? <span className="sp-pill bad">{t('Failed')}</span> : running ? <span className="sp-pill ok">{t('Running')}</span> : <span className="sp-pill">{t('Stopped')}</span>}</td>
                <td className="sp-note">{u.enabled === 'enabled' ? t('Starts on boot') : t('Does not start on boot')}</td>
                <td><div className="sp-acts">
                  {running ? (
                    <>
                      <button disabled={working === u.name} onClick={() => void act(u, 'restart')}>{t('Restart')}</button>
                      <button className="danger" disabled={working === u.name} onClick={() => void act(u, 'stop')}>{t('Stop')}</button>
                    </>
                  ) : <button className="primary" disabled={working === u.name} onClick={() => void act(u, 'start')}>{t('Start')}</button>}
                  <button onClick={() => onLogs(u.name)}>{t('Logs')}</button>
                </div></td>
              </tr>
            )
          })}
        </tbody>
      </table>{data && shown.length === 0 && <div className="sp-empty">{t('No service matches.')}</div>}</div>
    </div>
  )
}

function Docker({sid, onLogs}: {sid: string; onLogs(name: string): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.containers(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerContainers>(load, null)
  const act = async (name: string, action: ServerAction) => {
    const q = actionQuestion(t, 'container', action, name)
    if (!(await confirm(q.message, {confirmLabel: q.label, danger: action !== 'start'}))) return
    setWorking(name)
    try { await be.server.containerAction(sid, name, action); toast.info(t('{name}: done', {name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload} />
      <Err error={error} />
      {data && !data.installed && <div className="sp-empty">{t('Docker is not installed on this server.')}</div>}
      {data?.installed && (
        <div className="sp-table"><table>
          <thead><tr><th>{t('Container')}</th><th>{t('Status')}</th><th>{t('Ports')}</th><th /></tr></thead>
          <tbody>
            {data.containers.map(c => (
              <tr key={c.id}>
                <td><b>{c.name}</b><div className="sp-note m">{c.image}</div></td>
                <td>{c.state === 'running' ? <span className="sp-pill ok" title={c.status}>{t('Running')}</span> : <span className="sp-pill" title={c.status}>{t('Stopped')}</span>}</td>
                <td className="m">{c.ports || '–'}</td>
                <td><div className="sp-acts">
                  {c.state === 'running' ? (
                    <>
                      <button disabled={working === c.name} onClick={() => void act(c.name, 'restart')}>{t('Restart')}</button>
                      <button className="danger" disabled={working === c.name} onClick={() => void act(c.name, 'stop')}>{t('Stop')}</button>
                    </>
                  ) : <button className="primary" disabled={working === c.name} onClick={() => void act(c.name, 'start')}>{t('Start')}</button>}
                  <button onClick={() => onLogs(c.name)}>{t('Logs')}</button>
                </div></td>
              </tr>
            ))}
          </tbody>
        </table>{data.containers.length === 0 && <div className="sp-empty">{t('No containers.')}</div>}</div>
      )}
    </div>
  )
}

function Logs({sid, source, onSource}: {sid: string; source: LogSource; onSource(s: LogSource): void}) {
  const be = useBackend()
  const t = useT()
  const [filter, setFilter] = useState('')
  const [lv, setLv] = useState<'all' | 'err' | 'warn' | 'info'>('all')
  const boxRef = useRef<HTMLDivElement>(null)
  const load = useCallback(() => be.server.logs(sid, source.kind, source.name), [be, sid, source.kind, source.name])
  const {data, error, busy, reload} = useLoad<string>(load, null)
  const lines = useMemo(() => {
    const q = filter.trim().toLowerCase()
    return (data ?? '').split('\n').filter(l => l && (!q || l.toLowerCase().includes(q)) && (lv === 'all' || (lv === 'info' ? logLevel(l) === '' : logLevel(l) === lv)))
  }, [data, filter, lv])
  useEffect(() => { const b = boxRef.current; if (b) b.scrollTop = b.scrollHeight }, [lines])
  const levels: ['all' | 'err' | 'warn' | 'info', string][] = [['all', t('All')], ['err', t('Errors')], ['warn', t('Warnings')], ['info', t('Info')]]
  return (
    <div className="sp-view fill">
      <Bar2 busy={busy} onReload={reload}>
        <input className="sp-filter" aria-label={t('Filter log lines')} placeholder={t('Filter log lines')} value={filter} onChange={e => setFilter(e.target.value)} />
        {levels.map(([k, n]) => <button key={k} aria-pressed={lv === k} className={lv === k ? 'primary' : ''} onClick={() => setLv(k)}>{n}</button>)}
      </Bar2>
      <div className="sp-bar">
        <span className="sp-source">{t('Showing')}: <b>{source.name || t('Whole system')}</b></span>
        {source.name !== '' && <button onClick={() => onSource({kind: '', name: ''})}>{t('Whole system')}</button>}
      </div>
      <Err error={error} />
      <div ref={boxRef} className="sp-log" role="log" aria-label={t('Logs')} aria-busy={busy}>
        {lines.map((l, i) => <div key={i} className={'sp-log-line ' + logLevel(l)}>{l}</div>)}
        {data !== null && lines.length === 0 && <div className="muted">{t('No log lines.')}</div>}
      </div>
    </div>
  )
}

function Ports({sid, sshPort}: {sid: string; sshPort: number}) {
  const be = useBackend()
  const t = useT()
  const load = useCallback(() => be.server.ports(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerPorts>(load, null)
  const exposed = (data?.ports ?? []).filter(p => !p.local && p.open && ![22, 80, 443, sshPort].includes(p.port))
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}>
        {data && <span className="sp-os">{data.firewall === 'active' ? t('Firewall (ufw) is on') : data.firewall === 'inactive' ? t('Firewall (ufw) is off') : t('No ufw firewall found, or this login cannot read it')}</span>}
      </Bar2>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr><th>{t('Port')}</th><th>{t('Process')}</th><th>{t('Listens on')}</th><th>{t('Firewall')}</th></tr></thead>
        <tbody>
          {(data?.ports ?? []).map(p => (
            <tr key={p.port}>
              <td className="m">{p.port}</td><td>{p.process || '–'}</td><td className="m">{p.local ? t('This server only') : t('Everywhere')}</td>
              <td>{p.open ? <span className="sp-pill ok">{t('Allowed')}</span> : <span className="sp-pill">{t('Shut')}</span>}</td>
            </tr>
          ))}
        </tbody>
      </table>{data && data.ports.length === 0 && <div className="sp-empty">{t('No listening ports found.')}</div>}</div>
      <div className="sp-note">{t('The firewall is only shown here. To change it, use the terminal.')}</div>
      {exposed.length > 0 && <div className="sp-note">{t('Open to the outside: {ports}. Limit them by address or put them behind a web server if they should not be public.', {ports: exposed.map(p => p.port).join(', ')})}</div>}
    </div>
  )
}

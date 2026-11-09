import {useCallback, useEffect, useMemo, useRef, useState, type ReactNode} from 'react'
import {useBackend} from '../backend'
import {useT, useTn, type TFn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {IconDownload, IconFolder, IconPower, IconTerminal} from '../ui/Icons'
import {openInSftp} from '../sftp/openRequest'
import {Select} from '../ui/Select'
import {formatBytes} from '../sftp/format'
import {errMsg} from '../util'
import {actionQuestion, Card, Err, stackQuestion, Toolbar, useLoad, useServerConnection, type ServerAction, type ServiceAction, type StackAction, FilterInput} from './shared'
import {ansiSpans, stripAnsi} from '../ansi'
import {backupLevel, certLevel, formatRate, formatUptime, level, logLevel, pct} from './format'
import {Docker, RowMenu} from './Docker'
import {addSample, bucketSeries, loadHistory, MINUTE, saveHistory, type HistPoint, type Series} from './history'
import {Web} from './Web'
import {Maintenance} from './Maintenance'
import {Security} from './Security'
import {Users} from './Users'
import {Schedule} from './Schedule'
import {Ports} from './Firewall'
import {Kubernetes} from './Kubernetes'
import {Incident} from './Incident'
import {SECTION_GROUPS, SECTION_ICONS, type Section} from './sections'
import {useAttention, type AttentionState} from './attention'
import {LogRange, useServerClock, type LogWindow} from './LogRange'
import {useLogStream} from './logStream'
import {around, lineTime, withContext} from './logTime'
import type {K8sLogTarget} from './KubernetesShared'
import type {Host, K8sMultiLogs, K8sPod, K8sPodDetail, ServerBackup, ServerCerts, ServerContainer, ServerContainers, ServerOverview, ServerProcess, ServerUnit, ServerUpdates} from '../types'


/** kind 'pod' is a pod of the Kubernetes tab: name is `namespace/pod`; container and previous say which log of it */
/** kind 'workload': every pod of a workload or Service, the name being ns/kind/name */
export interface LogSource {kind: '' | 'service' | 'container' | 'stack' | 'web' | 'pod' | 'workload'; name: string; container?: string; previous?: boolean; /** a time range (RFC 3339) to open the log on */ range?: LogWindow}

/** the log source of what the Kubernetes tab asked to open */
export const logSourceOf = (p: K8sLogTarget): LogSource => (p.kind ? {kind: 'workload', name: p.ns + '/' + p.kind + '/' + p.name} : {kind: 'pod', name: p.ns + '/' + p.name, container: p.container, previous: p.previous})

const POLL_MS = 5000
/** where the panel remembers that the section list shows icons only */
const RAIL_KEY = 'vutty.panel.rail'
const FOLLOW_MS = 2000
/** how many samples of the overview are kept: 5 s apart, so half an hour */
const HISTORY = 360

/** The overview of the server, asked again every few seconds while the tab is shown (whatever section is open) and the samples kept for the charts. */
function useOverviewFeed(sid: string, hostId: string, visible: boolean) {
  const be = useBackend()
  const points = useRef<HistPoint[]>(loadHistory(hostId)) // a point for each minute of the last day, kept on this computer
  const [saved, setSaved] = useState(points.current)
  const [hist, setHist] = useState<{cpu: number[]; mem: number[]; disk: number[]; net: number[]}>({cpu: [], mem: [], disk: [], net: []})
  const load = useCallback(async () => {
    const o = await be.server.overview(sid)
    const keep = (h: number[], v: number) => [...h, v].slice(-HISTORY)
    const prev = points.current[points.current.length - 1]
    points.current = addSample(points.current, {cpu: o.cpu_pct, mem: pct(o.mem_used, o.mem_total), disk: pct(o.disk_used, o.disk_total), net: o.net_rx + o.net_tx})
    if (!prev || prev.t !== points.current[points.current.length - 1].t) { saveHistory(hostId, points.current); setSaved(points.current) } // once a minute
    setHist(h => ({cpu: keep(h.cpu, o.cpu_pct), mem: keep(h.mem, pct(o.mem_used, o.mem_total)), disk: keep(h.disk, pct(o.disk_used, o.disk_total)), net: keep(h.net, o.net_rx + o.net_tx)}))
    return o
  }, [be, sid, hostId])
  return {...useLoad<ServerOverview>(load, visible ? POLL_MS : null), hist, saved}
}
type Feed = ReturnType<typeof useOverviewFeed>

/**
 * A Server tab: the whole of a server in one page (load, processes, services, Docker, logs, ports and the firewall), over an SSH
 * connection of its own. It is what "Panel" opens from the protocol screen. The same data and actions as the Server section of
 * the sidebar, laid out in tables for the width of a tab.
 */
export function ServerPanel({host, visible, onAccess, onShell}: {host: Host; visible: boolean; onAccess?(): void; onShell?(host: Host, command: string, title: string): void}) {
  const t = useT()
  const {phase, sid, notice} = useServerConnection(host)
  const be = useBackend()
  const confirm = useConfirm()
  const toast = useToast()
  const reboot = async () => {
    if (!(await confirm(t('Restart {name}? Every connection to it, this panel included, drops until it is up again. Programs that are running stop.', {name: host.label}), {confirmLabel: t('Reboot'), danger: true}))) return
    try { await be.server.reboot(sid); toast.info(t('{name} is restarting', {name: host.label})) } catch (e) { toast.error(errMsg(e)) }
  }
  const [section, setSection] = useState<Section>('overview')
  const [source, setSource] = useState<LogSource>({kind: '', name: ''})
  const sections: [Section, string][] = [['overview', t('Overview')], ['processes', t('Processes')], ['services', t('Services')], ['docker', t('Docker')], ['kubernetes', t('Kubernetes')], ['web', t('Web')], ['logs', t('Logs')], ['incident', t('Incident')], ['ports', t('Ports & firewall')], ['maintenance', t('Maintenance')], ['security', t('Security')], ['users', t('Users')], ['schedule', t('Schedule')]]
  const showLogs = (s: LogSource) => { setSource(s); setSection('logs') }
  return (
    <div className="sp" role="region" aria-label={t('Server panel of {name}', {name: host.label})}>
      <div className="sp-head">
        <h1>{host.label}</h1>
        <span>{host.username ? host.username + '@' : ''}{host.hostname}{host.port !== 22 ? ':' + host.port : ''}</span>
        {phase === 'ready' && sid && <button className="sp-reboot" onClick={reboot} title={t('Restart the server')}><IconPower size={14} /> {t('Reboot')}</button>}
        {phase === 'ready' && <button className="sp-access" onClick={() => openInSftp(host)} title={t('Browse the files of this host in the SFTP tab')}><IconFolder size={14} /> {t('Files')}</button>}
        {onAccess && <button className="sp-access" onClick={onAccess} title={t('Open a terminal on this host in a new tab')}><IconTerminal size={14} /> {t('Access host')}</button>}
      </div>
      {phase === 'ready' && sid ? (
        <Ready sid={sid} host={host} visible={visible} onAccess={onAccess} onShell={onShell} section={section} setSection={setSection} source={source} setSource={setSource} sections={sections} showLogs={showLogs} />
      ) : <div className="sp-notice">{notice}</div>}
    </div>
  )
}

/** The part of the panel that needs the connection: the section tabs and the open section. */
function Ready({sid, host, visible, onAccess, onShell, section, setSection, source, setSource, sections, showLogs}: {sid: string; host: Host; visible: boolean; onAccess?(): void; onShell?(host: Host, command: string, title: string): void; section: Section; setSection(s: Section): void; source: LogSource; setSource(s: LogSource): void; sections: [Section, string][]; showLogs(s: LogSource): void}) {
  const t = useT()
  const tn = useTn()
  const feed = useOverviewFeed(sid, host.id, visible)
  const attn = useAttention(sid, host, feed.data)
  // what the Overview listed is read again when it is opened again: it was read when the panel opened
  const seen = useRef(section)
  useEffect(() => { if (section === 'overview' && seen.current !== 'overview') attn.reload(); seen.current = section }, [section]) // eslint-disable-line react-hooks/exhaustive-deps
  const [slim, setSlim] = useState(() => { try { return localStorage.getItem(RAIL_KEY) === '1' } catch { return false } }) // the list shows icons only
  const toggleSlim = () => setSlim(v => { try { localStorage.setItem(RAIL_KEY, v ? '0' : '1') } catch { /* not kept */ } return !v })
  const label = Object.fromEntries(sections) as Record<Section, string>
  const groupName: Record<string, string> = {monitor: t('Monitor'), run: t('Run'), manage: t('Manage')}
  const [k8sCtx, setK8sCtx] = useState('') // the cluster the Kubernetes tab uses: the Logs tab lists its pods
  const [k8sNs, setK8sNs] = useState('') // and the namespaces it shows: a login that may not list the cluster has pods only there
  // once opened, the Kubernetes tab stays (hidden) while another section is open: its connection steps, view and filters are not done again
  const [k8sOpened, setK8sOpened] = useState(false)
  if (section === 'kubernetes' && !k8sOpened) setK8sOpened(true)
  // Maintenance stays (hidden) once opened: a folder scan can take minutes and must not be lost by looking at another section
  const [maintOpened, setMaintOpened] = useState(false)
  if (section === 'maintenance' && !maintOpened) setMaintOpened(true)
  return (
        <div className={'sp-shell' + (slim ? ' slim' : '')}>
          <nav className="sp-rail" aria-label={t('Server sections')}>
            {SECTION_GROUPS.map(g => {
              const total = g.sections.reduce((n, id) => n + (attn.marks[id]?.n ?? 0), 0)
              return (
                <div key={g.id} className="sp-grp" role="tablist" aria-orientation="vertical" aria-label={groupName[g.id]}>
                  <h3 aria-hidden="true"><span>{groupName[g.id]}</span>{total > 0 && <small>{total}</small>}</h3>
                  {g.sections.map(id => {
                    const m = attn.marks[id]
                    const note = m ? tn(m.n, '{n} item needs attention', '{n} items need attention', {}) : ''
                    return (
                      <button key={id} role="tab" aria-selected={section === id} aria-label={label[id]} aria-describedby={m ? 'sp-note-' + id : undefined} title={slim ? label[id] + (note ? ' · ' + note : '') : note || undefined} className="sp-item" onClick={() => setSection(id)}>
                        {SECTION_ICONS[id]}
                        <span className="lab">{label[id]}</span>
                        {m && <span className={'sp-badge ' + m.tone} aria-hidden="true">{m.n}</span>}
                        {m && <span id={'sp-note-' + id} hidden>{note}</span>}
                      </button>
                    )
                  })}
                </div>
              )
            })}
            <button className="sp-slim" onClick={toggleSlim} aria-pressed={slim} aria-label={slim ? t('Show section names') : t('Show icons only')} title={slim ? t('Show section names') : t('Show icons only')}>{slim ? '»' : '«'}</button>
          </nav>
          <div className="sp-body">
            {section === 'overview' && <Overview host={host} feed={feed} attn={attn} onGo={setSection} />}
            {section === 'processes' && <Processes sid={sid} visible={visible} />}
            {section === 'services' && <Services sid={sid} onLogs={name => showLogs({kind: 'service', name})} />}
            {section === 'docker' && <Docker sid={sid} host={host} onLogs={showLogs} onShell={onShell} />}
            {k8sOpened && <div className="sp-keep" hidden={section !== 'kubernetes'}><Kubernetes sid={sid} host={host} visible={visible && section === 'kubernetes'} onShell={onShell} onAccess={onAccess} onContext={setK8sCtx} onNamespace={setK8sNs} onLogs={p => showLogs(logSourceOf(p))} /></div>}
            {section === 'web' && <Web sid={sid} onLogs={(kind, name) => showLogs({kind, name})} />}
            {section === 'logs' && <Logs sid={sid} visible={visible} source={source} onSource={setSource} k8sCtx={k8sCtx} k8sNs={k8sNs} />}
            {section === 'incident' && <Incident sid={sid} onLogs={showLogs} />}
            {section === 'ports' && <Ports sid={sid} sshPort={host.port} />}
            {maintOpened && <div className="sp-keep" hidden={section !== 'maintenance'}><Maintenance sid={sid} host={host} visible={visible && section === 'maintenance'} /></div>}
            {section === 'security' && <Security sid={sid} />}
            {section === 'users' && <Users sid={sid} />}
            {section === 'schedule' && <Schedule sid={sid} host={host} onLogs={(kind, name) => showLogs({kind, name})} />}
          </div>
        </div>
  )
}

function Bar({label, value, note, data}: {label: string; value: number; note: string; data?: (number | null)[]}) {
  return (
    <div className="sp-card">
      <h3>{label}</h3>
      <div className="sp-big">{value}<small>%</small></div>
      <div className={'sp-track ' + level(value)} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={value}><i style={{width: value + '%'}} /></div>
      <div className="sp-note">{note}</div>
      {data && <Spark data={data} max={100} />}
    </div>
  )
}

/** a chart of readings; a null is a time nothing was read, and the line stops there */
function Spark({data, max}: {data: (number | null)[]; max: number}) {
  const known = data.filter((v): v is number => v !== null)
  if (known.length < 2) return <div className="sp-spark" aria-hidden="true" />
  const w = 240, h = 44
  const top = Math.max(max, ...known, 1)
  const xy = (v: number, i: number) => [(i / (data.length - 1)) * w, h - 3 - (v / top) * (h - 8)] as const
  const runs: (readonly [number, number])[][] = []
  data.forEach((v, i) => { if (v === null) return; if (i > 0 && data[i - 1] !== null) runs[runs.length - 1].push(xy(v, i)); else runs.push([xy(v, i)]) })
  const line = (r: (readonly [number, number])[]) => r.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join(' ')
  return (
    <svg className="sp-spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden="true">
      {runs.map((r, i) => r.length > 1 ? (
        <g key={i}>
          <path d={`${line(r)} L${r[r.length - 1][0].toFixed(1)} ${h} L${r[0][0].toFixed(1)} ${h}Z`} fill="currentColor" fillOpacity=".12" />
          <path d={line(r)} fill="none" stroke="currentColor" strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
        </g>
      ) : <circle key={i} cx={r[0][0]} cy={r[0][1]} r="1.5" fill="currentColor" />)}
    </svg>
  )
}

const Bar2 = Toolbar

function Overview({host, feed, attn, onGo}: {host: Host; feed: Feed; attn: AttentionState; onGo(s: Section): void}) {
  const t = useT()
  const {data: o, error, busy, reload: reloadFeed, hist: live, saved} = feed
  const reload = () => { reloadFeed(); attn.reload() }
  const [span, setSpan] = useState<'30m' | '6h' | '24h'>('30m')
  const hist: {cpu: (number | null)[]; mem: (number | null)[]; disk: (number | null)[]; net: (number | null)[]} = useMemo<Series>(() => (span === '30m' ? live : bucketSeries(saved, (span === '6h' ? 6 : 24) * 60 * MINUTE, 120)), [span, live, saved])
  void host
  if (!o) return <><Err error={error} /><div className="muted">{t('Loading…')}</div></>
  const ram = pct(o.mem_used, o.mem_total), disk = pct(o.disk_used, o.disk_total)
  const minutes = Math.max(1, Math.round((live.cpu.length * POLL_MS) / 60000))
  const attention = attn.list
  const units = attn.loaded
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><span className="sp-os">{o.os} · {t('up {time}', {time: formatUptime(o.uptime_sec)})}</span></Bar2>
      <Err error={error} />
      <div className="sp-grid">
        <div className="sp-card"><h3>{t('CPU')} · {t('{n} cores', {n: o.cores})}</h3><div className="sp-big">{Math.round(o.cpu_pct)}<small>%</small></div><Spark data={hist.cpu} max={100} /></div>
        <Bar label={t('Memory')} value={ram} data={hist.mem} note={`${formatBytes(o.mem_used)} / ${formatBytes(o.mem_total)}${o.swap_total ? ' · ' + t('Swap') + ' ' + formatBytes(o.swap_used) + ' / ' + formatBytes(o.swap_total) : ''}`} />
        <Bar label={t('Disk /')} value={disk} data={hist.disk} note={`${formatBytes(o.disk_used)} / ${formatBytes(o.disk_total)}`} />
        <div className="sp-card"><h3>{t('Network')}</h3><div className="sp-big sp-big-sm">↓ {formatRate(o.net_rx)}</div><div className="sp-note">↑ {formatRate(o.net_tx)}</div><Spark data={hist.net} max={1024} /></div>
      </div>
      <div className="sp-bar">
        <div className="segmented" role="group" aria-label={t('Chart range')}>
          {([['30m', t('30 min')], ['6h', t('6 hours')], ['24h', t('24 hours')]] as const).map(([k, label]) => <button key={k} aria-pressed={span === k} className={span === k ? 'on' : ''} onClick={() => setSpan(k)}>{label}</button>)}
        </div>
        <div className="sp-note">{span === '30m' ? t('The charts show the last {n} min while this tab is open.', {n: minutes}) : t('Read while this tab is open; a gap is a time it was closed.')}</div>
      </div>
      <div className="sp-grid sp-grid-2">
        <Card title={t('Needs attention')}>
          {attention.length === 0 && <div className="sp-note">{units ? t('Nothing needs attention.') : t('Loading…')}</div>}
          {attention.map(a => (
            <div key={a.key} className="sp-attn">
              <span><span className={'sp-pill ' + a.tone}>{a.tone === 'bad' ? t('Problem') : t('Warning')}</span> {a.text}</span>
              {a.go !== 'overview' && <button onClick={() => onGo(a.go)}>{t('Open')}</button>}
            </div>
          ))}
        </Card>
        <Card title={t('Details')}>
          <table className="sp-kv"><tbody>
            <tr><td>{t('System')}</td><td>{o.os}</td></tr>
            <tr><td>{t('Kernel')}</td><td>{o.kernel}</td></tr>
            <tr><td>{t('Load average')}</td><td>{o.load.map(x => x.toFixed(2)).join(' · ')}</td></tr>
            <tr><td>{t('Uptime')}</td><td>{formatUptime(o.uptime_sec)}</td></tr>
          </tbody></table>
          {!o.root && <div className="sp-note">{t('Changes use sudo without a password. If the login cannot, they fail with the server\'s message.')}</div>}
        </Card>
      </div>
    </div>
  )
}

type ProcSort = 'pid' | 'command' | 'user' | 'cpu' | 'mem'

function Processes({sid, visible}: {sid: string; visible: boolean}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [filter, setFilter] = useState('')
  const [killing, setKilling] = useState(0)
  const [ending, setEnding] = useState<number[]>([])
  const load = useCallback(() => be.server.processes(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerProcess[]>(load, visible ? POLL_MS : null)
  const [sort, setSort] = useState<{key: ProcSort; dir: 1 | -1} | null>(null)
  const shown = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const list = (data ?? []).filter(p => !q || p.command.toLowerCase().includes(q) || p.user.toLowerCase().includes(q) || String(p.pid) === q)
    if (!sort) return list
    const {key, dir} = sort
    return [...list].sort((a, b) => {
      const x = a[key], y = b[key]
      return dir * (typeof x === 'string' && typeof y === 'string' ? x.localeCompare(y) : Number(x) - Number(y))
    })
  }, [data, filter, sort])
  // numbers start with the biggest, text with A to Z; a second click turns it round
  const sortBy = (key: ProcSort) => setSort(s => (s?.key === key ? {key, dir: s.dir === 1 ? -1 : 1} : {key, dir: key === 'command' || key === 'user' ? 1 : -1}))
  const th = (key: ProcSort, label: string, cls?: string) => (
    <th className={cls} aria-sort={sort?.key === key ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
      <button className="sp-sort" title={t('Sort by {column}', {column: label})} onClick={() => sortBy(key)}>{label}{sort?.key === key && <span aria-hidden="true">{sort.dir === 1 ? ' ▲' : ' ▼'}</span>}</button>
    </th>
  )
  const kill = async (p: ServerProcess, signal: 'TERM' | 'KILL') => {
    setKilling(0)
    setEnding(e => [...e, p.pid])
    try { await be.server.kill(sid, p.pid, signal); toast.info(t('Sent {signal} to {pid}', {signal, pid: p.pid})); reload() } catch (e) { toast.error(errMsg(e)) }
    setEnding(e => e.filter(x => x !== p.pid))
  }
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><FilterInput label={t('Filter processes')} placeholder={t('Filter processes')} value={filter} onChange={setFilter} /></Bar2>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr>{th('pid', t('PID'))}{th('command', t('Process'))}{th('user', t('User'))}{th('cpu', t('CPU'), 'n')}{th('mem', t('RAM'), 'n')}<th /></tr></thead>
        <tbody>
          {shown.map(p => (
            <tr key={p.pid}>
              <td className="m">{p.pid}</td><td className="sp-cmd" title={p.command}>{p.command}</td><td className="m">{p.user}</td>
              <td className="n">{p.cpu.toFixed(1)}%</td><td className="n">{p.mem.toFixed(1)}%</td>
              <td><div className="sp-acts">
                {killing === p.pid ? (
                  <>
                    <button className="danger" aria-busy={ending.includes(p.pid)} disabled={ending.includes(p.pid)} onClick={() => void kill(p, 'TERM')}>{t('End process')}</button>
                    <button className="danger" aria-busy={ending.includes(p.pid)} disabled={ending.includes(p.pid)} onClick={() => void kill(p, 'KILL')} title={t('It cannot clean up.')}>{t('Force kill')}</button>
                    <button onClick={() => setKilling(0)}>{t('Cancel')}</button>
                  </>
                ) : <button className="danger" aria-busy={ending.includes(p.pid)} disabled={ending.includes(p.pid)} aria-label={t('End process {pid} ({cmd})?', {pid: p.pid, cmd: p.command.split(' ')[0]})} onClick={() => setKilling(p.pid)}>{t('End')}</button>}
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
  const act = async (u: ServerUnit, action: ServiceAction) => {
    const q = actionQuestion(t, 'service', action, u.name)
    if (!(await confirm(q.message, {confirmLabel: q.label, danger: action !== 'start' && action !== 'enable'}))) return
    setWorking(u.name)
    try { await be.server.serviceAction(sid, u.name, action); toast.info(t('{name}: done', {name: u.name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  return (
    <div className="sp-view">
      <Bar2 busy={busy} onReload={reload}><FilterInput label={t('Filter services')} placeholder={t('Filter services')} value={filter} onChange={setFilter} /></Bar2>
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
                  {running
                    ? <button aria-busy={working === u.name} disabled={working === u.name} onClick={() => void act(u, 'restart')}>{t('Restart')}</button>
                    : <button className="primary" aria-busy={working === u.name} disabled={working === u.name} onClick={() => void act(u, 'start')}>{t('Start')}</button>}
                  <RowMenu name={u.name} items={[
                    ...(running ? [{label: t('Stop'), danger: true, disabled: working === u.name, run: () => void act(u, 'stop')}] : []),
                    ...(u.enabled === 'enabled' || u.enabled === 'disabled' ? [{label: u.enabled === 'enabled' ? t('Disable on boot') : t('Enable on boot'), disabled: working === u.name, run: () => void act(u, u.enabled === 'enabled' ? 'disable' : 'enable')}] : []),
                    {label: t('Logs'), run: () => onLogs(u.name)},
                  ]} />
                </div></td>
              </tr>
            )
          })}
        </tbody>
      </table>{data && shown.length === 0 && <div className="sp-empty">{t('No service matches.')}</div>}</div>
    </div>
  )
}

const sourceKey = (s: LogSource) => (s.name ? s.kind + ':' + s.name : '')
const parseSource = (v: string): LogSource => {
  const i = v.indexOf(':')
  return i < 0 ? {kind: '', name: ''} : {kind: v.slice(0, i) as LogSource['kind'], name: v.slice(i + 1)}
}

/** every log the panel can read, by kind: the whole system first, then what the server runs; the one open is always among them */
/** a colour for a pod name: the same pod has the same colour in every merged log */
const podColor = (name: string) => { let h = 0; for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 360; return `hsl(${h} 70% 68%)` }

function logSources(t: TFn, units: ServerUnit[], boxes: ServerContainer[], pods: K8sPod[], now: LogSource, podsOnly = false) {
  const group = (label: string, kind: LogSource['kind'], names: string[]) => names.map(n => ({value: kind + ':' + n, label: n, hint: label}))
  const list = podsOnly ? pods.map(p => ({value: 'pod:' + p.namespace + '/' + p.name, label: p.name, hint: t('Pods') + ' · ' + p.namespace})) : [
    {value: '', label: t('Whole system')},
    ...group(t('Web server'), 'web', ['nginx', 'caddy']),
    ...group(t('Services'), 'service', units.map(u => u.name)),
    ...group(t('Containers'), 'container', boxes.map(c => c.name)),
    ...group(t('Stacks'), 'stack', [...new Set(boxes.map(c => c.project).filter(Boolean))]),
    ...pods.map(p => ({value: 'pod:' + p.namespace + '/' + p.name, label: p.name, hint: t('Pods') + ' · ' + p.namespace})),
  ]
  const key = sourceKey(now)
  if (now.kind === 'workload') { const [ns, , name] = now.name.split('/'); return list.some(o => o.value === key) ? list : [...list, {value: key, label: t('{name} (all pods)', {name}), hint: t('Pods') + ' · ' + ns}] }
  return list.some(o => o.value === key) || (podsOnly && !now.name) ? list : [...list, {value: key, label: now.name, hint: now.kind}]
}

/** the filter as a test on a line, and as the pattern to mark in it: plain text is found anywhere and in any case */
function lineMatcher(filter: string, regex: boolean): {test: ((l: string) => boolean) | null; mark: RegExp | null; bad: boolean} {
  const q = filter.trim()
  if (!q) return {test: null, mark: null, bad: false}
  try {
    const mark = new RegExp(regex ? q : q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
    return {test: l => { mark.lastIndex = 0; return mark.test(l) }, mark, bad: false}
  } catch {
    return {test: () => false, mark: null, bad: true}
  }
}

/** a line with what matched in <mark>; a pattern that can match nothing marks nothing */
function highlight(line: string, re: RegExp): ReactNode {
  const out: ReactNode[] = []
  let at = 0
  re.lastIndex = 0
  for (let m = re.exec(line); m; m = re.exec(line)) {
    if (m[0] === '') { re.lastIndex++; continue }
    if (m.index > at) out.push(line.slice(at, m.index))
    out.push(<mark key={m.index}>{m[0]}</mark>)
    at = m.index + m[0].length
  }
  if (at === 0) return line
  if (at < line.length) out.push(line.slice(at))
  return out
}

/** podsOnly: a cluster opened with a kubeconfig, where there is no server to read system, service or container logs from: only the pods are offered */
export function Logs({sid, visible, source, onSource, k8sCtx, k8sNs = '', podsOnly = false}: {sid: string; visible: boolean; source: LogSource; onSource(s: LogSource): void; k8sCtx: string; /** the namespaces the Kubernetes tab shows: a login that may not list the whole cluster has pods to choose from only there */ k8sNs?: string; podsOnly?: boolean}) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const [filter, setFilter] = useState('')
  const [lv, setLv] = useState<'all' | 'err' | 'warn' | 'info'>('all')
  const [follow, setFollow] = useState(false)
  const [regex, setRegex] = useState(false)
  const [lineCount, setLineCount] = useState('300')
  const [since, setSince] = useState<'' | '15m' | '1h' | '24h'>('')
  const [range, setRange] = useState<LogWindow | null>(source.range ?? null) // a time range of its own, in place of "the last N minutes"
  const [boot, setBoot] = useState(false) // the boot before this one
  const [picked, setPicked] = useState('') // the line clicked: the range around it can be asked for
  const [context, setContext] = useState(false) // keep the lines around each match
  const clock = useServerClock(sid)
  useEffect(() => { setRange(source.range ?? null); setBoot(false); setPicked('') }, [source.kind, source.name, source.range?.from, source.range?.until]) // eslint-disable-line react-hooks/exhaustive-deps
  const boxRef = useRef<HTMLDivElement>(null)
  const stuck = useRef(true) // the view is at the bottom: new lines scroll into sight, but never pull away from what is being read
  // what the source list offers: asked once, and left out quietly when the login cannot ask
  const loadUnits = useCallback(() => (podsOnly ? Promise.resolve([] as ServerUnit[]) : be.server.services(sid)), [be, sid, podsOnly])
  const {data: units} = useLoad<ServerUnit[]>(loadUnits, null)
  const loadBoxes = useCallback(() => (podsOnly ? Promise.resolve({installed: false, containers: []} as ServerContainers) : be.server.containers(sid)), [be, sid, podsOnly])
  const {data: boxes} = useLoad<ServerContainers>(loadBoxes, null)
  // the pods of the cluster the Kubernetes tab uses, and what is asked of one of them: its container, the one that ran before, timestamps
  const loadPods = useCallback(() => (k8sCtx ? be.server.k8sPods(sid, k8sCtx, k8sNs) : Promise.resolve([] as K8sPod[])), [be, sid, k8sCtx, k8sNs])
  const {data: pods, reload: reloadPods} = useLoad<K8sPod[]>(loadPods, null)
  const podScope = useRef(k8sCtx + '|' + k8sNs)
  useEffect(() => { const now = k8sCtx + '|' + k8sNs; if (podScope.current !== now) { podScope.current = now; reloadPods() } }, [k8sCtx, k8sNs, reloadPods])
  const [container, setContainer] = useState(source.container ?? '')
  const [previous, setPrevious] = useState(!!source.previous)
  const [timestamps, setTimestamps] = useState(false)
  useEffect(() => { setContainer(source.container ?? ''); setPrevious(!!source.previous) }, [source.kind, source.name, source.container, source.previous])
  const sources = useMemo(() => logSources(t, units ?? [], boxes?.containers ?? [], pods ?? [], source, podsOnly), [t, units, boxes, pods, source, podsOnly])
  const isPod = source.kind === 'pod'
  const isMulti = source.kind === 'workload' // every pod of a workload or Service: its name is ns/kind/name
  const multiOf = isMulti ? source.name.split('/') : []
  const podOf = isPod ? source.name.split('/') : []
  // the containers of the pod being read come from that pod, not from a list of the whole cluster (which a login of one namespace may not read)
  const loadDetail = useCallback(() => (isPod && k8sCtx ? be.server.k8sPod(sid, k8sCtx, podOf[0], podOf[1]) : Promise.resolve(null)), [be, sid, k8sCtx, source.kind, source.name]) // eslint-disable-line react-hooks/exhaustive-deps
  const {data: detail, reload: reloadDetail} = useLoad<K8sPodDetail | null>(loadDetail, null)
  const detailFor = useRef(`${k8sCtx}:${source.kind}:${source.name}`)
  useEffect(() => { const now = `${k8sCtx}:${source.kind}:${source.name}`; if (detailFor.current !== now) { detailFor.current = now; reloadDetail() } }, [k8sCtx, source.kind, source.name, reloadDetail])
  const podInfo = isPod ? (detail && detail.pod.namespace === podOf[0] && detail.pod.name === podOf[1] ? detail.pod : (pods ?? []).find(p => p.namespace === podOf[0] && p.name === podOf[1])) : undefined
  const containers = [{value: '', label: t('Default container')}, ...(podInfo?.containers ?? []).map(c => ({value: c.name, label: c.init ? t('{name} (init)', {name: c.name}) : c.name}))]
  const opts = lineCount === '300' && since === '' ? undefined : {lines: Number(lineCount), since}
  const podKey = isPod || isMulti ? `${k8sCtx}:${container}:${previous}:${timestamps}` : ''
  const [multi, setMulti] = useState<K8sMultiLogs | null>(null)
  const byTime = !isPod && !isMulti && !podsOnly && (!!range || boot) // a time range of its own, or the boot before: read once, never followed
  const streamable = isPod ? !previous && !!k8sCtx : !isMulti && !podsOnly && !byTime // a pod is followed too; the log of an earlier run and the merged log of a workload are read again
  const kindOf = source.kind as Exclude<LogSource['kind'], 'pod' | 'workload'>
  const stream = useLogStream(sid, follow && visible && streamable, isPod
    ? {type: 'pod', ctx: k8sCtx, ns: podOf[0], pod: podOf[1], container, timestamps, lines: Number(lineCount), since}
    : {type: 'host', kind: kindOf, name: source.name, lines: Number(lineCount), since})
  const streaming = follow && visible && streamable && !stream.failed // else Follow reads the log again every 2 seconds
  const load = useCallback(() => (podsOnly && !isPod && !isMulti ? Promise.resolve('') : isMulti
    ? (k8sCtx ? be.server.k8sLogsMulti(sid, k8sCtx, multiOf[0], multiOf[1], multiOf[2], {container: '', lines: Number(lineCount) as 100, since, previous: false, timestamps}).then(r => { setMulti(r); return r.text }) : Promise.reject(t('Open the Kubernetes tab first to choose a cluster.')))
    : isPod
    ? (k8sCtx ? be.server.k8sLogs(sid, k8sCtx, podOf[0], podOf[1], {container, lines: Number(lineCount) as 100, since, previous, timestamps}) : Promise.reject(t('Open the Kubernetes tab first to choose a cluster.')))
    : byTime
    ? be.server.logsAt(sid, kindOf, source.name, Number(lineCount), range?.from ?? '', range?.until ?? '', boot)
    : be.server.logs(sid, source.kind as Exclude<LogSource['kind'], 'pod' | 'workload'>, source.name, opts)), [be, sid, source.kind, source.name, lineCount, since, podKey, podsOnly, range?.from, range?.until, boot]) // eslint-disable-line react-hooks/exhaustive-deps
  const {data, error, busy, reload} = useLoad<string>(load, follow && visible && !streaming ? FOLLOW_MS : null)
  // useLoad reads again by itself only on a timer: another source, number of lines or time is asked for here
  const asked = useRef(`${source.kind}:${source.name}:${lineCount}:${since}:${podKey}:${range?.from}:${range?.until}:${boot}`)
  useEffect(() => {
    const now = `${source.kind}:${source.name}:${lineCount}:${since}:${podKey}:${range?.from}:${range?.until}:${boot}`
    if (asked.current !== now) { asked.current = now; reload() }
  }, [source.kind, source.name, lineCount, since, podKey, range?.from, range?.until, boot, reload])
  // Follow turned off (or the tab hidden): the screen has what was streamed; the last reading is older than that, so read again
  const wasStreaming = useRef(false)
  useEffect(() => { if (wasStreaming.current && !streaming) reload(); wasStreaming.current = streaming }, [streaming, reload])
  const toast = useToast()
  const save = async () => {
    const d = new Date(), p2 = (n: number) => String(n).padStart(2, '0')
    const name = `${source.name.replace(/\//g, '-') || 'system'}-${d.getFullYear()}${p2(d.getMonth() + 1)}${p2(d.getDate())}-${p2(d.getHours())}${p2(d.getMinutes())}.log`
    try {
      const path = await be.system.saveText(t('Save log'), name, lines.map(stripAnsi).join('\n') + '\n')
      if (path) toast.info(t('Saved to {path}', {path}))
    } catch (e) { toast.error(errMsg(e)) }
  }
  const matcher = useMemo(() => lineMatcher(filter, regex), [filter, regex])
  // what the programs print can hold colour codes: they are read for the colours, but what is searched, counted as an error or saved is the text
  const text = streaming ? stream.rows.join('\n') : data
  const loaded = streaming ? stream.rows.length > 0 || stream.ended : data !== null
  const all = useMemo(() => (text ?? '').split('\n').filter(l => stripAnsi(l) !== ''), [text])
  const passes = useCallback((l: string) => { const p = stripAnsi(l); return (!matcher.test || matcher.test(p)) && (lv === 'all' || (lv === 'info' ? logLevel(p) === '' : logLevel(p) === lv)) }, [matcher, lv])
  const lines = useMemo(() => all.filter(passes), [all, passes])
  const filtering = !!matcher.test || lv !== 'all'
  // with Context on, each match comes with the lines around it (a hole between two groups is a null)
  const rows = useMemo(() => (context && filtering ? withContext(all, passes, 3) : lines), [context, filtering, all, passes, lines])
  useEffect(() => { const b = boxRef.current; if (b && stuck.current) b.scrollTop = b.scrollHeight }, [rows])
  const onScroll = () => { const b = boxRef.current; if (b) stuck.current = b.scrollHeight - b.scrollTop - b.clientHeight < 40 }
  const levels: ['all' | 'err' | 'warn' | 'info', string][] = [['all', t('All')], ['err', t('Errors')], ['warn', t('Warnings')], ['info', t('Info')]]
  const timed = !isPod && !isMulti && !podsOnly // a log with times to choose from (the nginx error file has none, the backend says so)
  const journal = timed && (source.kind === '' || source.kind === 'service' || (source.kind === 'web' && source.name === 'caddy'))
  const pickedAt = picked && timed ? lineTime(picked) : null
  const showRange = (r: LogWindow | null) => { setRange(r); setBoot(false); setFollow(false); setPicked('') }
  const timeChoice = async (v: string) => {
    if (v !== 'custom') { setRange(null); setSince(v as typeof since); return }
    try {
      const c = await clock.get()
      const now = Date.parse(c.now)
      showRange({from: new Date(now - 3600000).toISOString().replace(/\.\d+Z$/, 'Z'), until: new Date(now).toISOString().replace(/\.\d+Z$/, 'Z')})
    } catch (e) { toast.error(errMsg(e)) }
  }
  return (
    <div className="sp-view fill">
      <Bar2 busy={busy} onReload={reload}>
        <FilterInput label={t('Filter log lines')} placeholder={t('Filter log lines')} invalid={matcher.bad} value={filter} onChange={setFilter} />
        <button aria-pressed={regex} className={regex ? 'primary' : ''} title={matcher.bad ? t('Not a valid regular expression') : t('Treat the filter as a regular expression')} onClick={() => setRegex(r => !r)}>.*</button>
        {levels.map(([k, n]) => <button key={k} aria-pressed={lv === k} className={lv === k ? 'primary' : ''} onClick={() => setLv(k)}>{n}</button>)}
        <button aria-pressed={context} className={context ? 'primary' : ''} disabled={!filtering} title={filtering ? t('Show 3 lines before and after each match') : t('Filter the log first: Context keeps the lines around each match')} onClick={() => setContext(c => !c)}>{t('Context')}</button>
        <button aria-pressed={follow} className={follow ? 'primary' : ''} disabled={byTime} title={byTime ? t('A time range or an earlier boot is read once: choose Any time to follow') : streamable ? t('Shows new lines as they arrive and keeps the newest in view') : t('Reads the log again every 2 seconds and keeps the newest line in view')} onClick={() => { stuck.current = true; setFollow(f => !f) }}>{t('Follow')}</button>
      </Bar2>
      <div className="sp-bar">
        <Select className="sp-pick" label={t('Log source')} value={sourceKey(source)} onChange={v => onSource({...parseSource(v), range: range ?? undefined})} options={sources} placeholder={podsOnly ? t('Choose a pod') : undefined} searchable />
        <Select className="sp-pick" label={t('Number of lines')} value={lineCount} onChange={setLineCount} options={['100', '300', '1000', '5000'].map(n => ({value: n, label: t('Last {n} lines', {n})}))} />
        <Select className="sp-pick" label={t('Time range')} value={range ? 'custom' : since} onChange={v => void timeChoice(v)}
          options={[{value: '', label: t('Any time')}, {value: '15m', label: t('Last 15 minutes')}, {value: '1h', label: t('Last hour')}, {value: '24h', label: t('Last 24 hours')}, ...(timed ? [{value: 'custom', label: t('Choose a range…')}] : [])]} />
        {journal && <button aria-pressed={boot} className={boot ? 'primary' : ''} title={t('The log of the boot before this one: what happened before the last restart')} onClick={() => { setBoot(b => !b); setRange(null); setFollow(false); setPicked('') }}>{t('Previous boot')}</button>}
        {range && timed && <LogRange range={range} clock={clock.clock} onRange={showRange} />}
        {isPod && <Select className="sp-pick" label={t('Container')} value={container} onChange={setContainer} options={container && !containers.some(c => c.value === container) ? [...containers, {value: container, label: container}] : containers} />}
        {isPod && previous && <button aria-pressed className="primary" aria-label={t('Previous container')} title={t('The container that ran before the last restart: press to read the current one')} onClick={() => setPrevious(false)}>{t('Previous container')} ×</button>}
        {(isPod || isMulti) && <button aria-pressed={timestamps} className={timestamps ? 'primary' : ''} onClick={() => setTimestamps(v => !v)}>{t('Timestamps')}</button>}
        <button disabled={lines.length === 0} title={t('Save the lines shown to a file')} onClick={() => void save()}><IconDownload size={14} /> {t('Download')}</button>
        <span className="sp-source" role="status">{loaded && filtering ? t('{shown} of {total} lines', {shown: lines.length, total: all.length}) : ''}</span>
      </div>
      {boot && <div className="sp-note k8s-hint" role="status">{t('Showing the boot before this one. Times are the time of the server.')}</div>}
      {stream.failed && follow && streamable && <div className="sp-note k8s-hint" role="status">{t('Could not follow the log live ({reason}): it is read again every 2 seconds.', {reason: stream.failed})}</div>}
      {streaming && stream.ended && <div className="sp-note k8s-hint" role="status">{t('The command on the server ended: nothing more will arrive.')}</div>}
      {streaming && stream.skipped > 0 && <div className="sp-note k8s-hint" role="status">{tn(stream.skipped, '{n} line was skipped: the log is faster than the screen', '{n} lines were skipped: the log is faster than the screen')}</div>}
      {pickedAt && <div className="sp-picked" role="group" aria-label={t('Around the picked line')}>
        <span>{t('Line of {time}:', {time: pickedAt.replace('T', ' ').replace('Z', ' UTC')})}</span>
        {[1, 5, 30].map(m => <button key={m} onClick={() => showRange(around(pickedAt, m))}>{t('Show ±{n} min around it', {n: m})}</button>)}
        <button className="ghost" onClick={() => setPicked('')}>{t('Clear')}</button>
      </div>}
      {isMulti && multi && <div className="sp-note k8s-hint" role="status">{tn(multi.pods, '{n} pod read', '{n} pods read')}{multi.skipped > 0 && ' · ' + t('{n} more pods are not shown', {n: multi.skipped})}{multi.failed.length > 0 && ' · ' + t('Could not read: {list}', {list: multi.failed.join('; ')})}</div>}
      <Err error={error} />
      <div ref={boxRef} className="sp-log" role="log" aria-label={t('Logs')} aria-busy={busy && !streaming} onScroll={onScroll}>
        {rows.map((l, i) => {
          if (l === null) return <div key={i} className="sp-log-gap" aria-hidden>···</div>
          const p = stripAnsi(l)
          // the colours of the program, unless a search is on (a match is marked on the plain text) or the line is an error or a warning (its own colour)
          const pre = isMulti ? /^\[([^\]]+)\] /.exec(p) : null // a merged log: the pod each line came from, in a colour of its own
          const rest = pre ? l.replace(/^\[[^\]]+\] /, '') : l
          const text = pre ? p.slice(pre[0].length) : p
          const inner = text === rest || logLevel(p) ? text : ansiSpans(rest).map((s, k) => (s.style ? <span key={k} style={s.style}>{s.text}</span> : s.text))
          const body = matcher.mark ? highlight(p, matcher.mark) : pre ? <><span className="sp-pod" style={{color: podColor(pre[1])}}>[{pre[1]}]</span> {inner}</> : inner
          const clickable = timed && lineTime(p) !== null
          return <div key={i} className={'sp-log-line ' + logLevel(p) + (l === picked ? ' picked' : '')} onClick={clickable ? () => setPicked(l === picked ? '' : l) : undefined} style={clickable ? {cursor: 'pointer'} : undefined}>{body}</div>
        })}
        {loaded && rows.length === 0 && <div className="muted">{podsOnly && !isPod ? t('Choose a pod above, or press Logs on one in the Pods view.') : t('No log lines.')}</div>}
      </div>
    </div>
  )
}


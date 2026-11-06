import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {IconRefresh} from '../ui/Icons'
import {useMinBusy} from '../ui/useMinBusy'
import {errMsg} from '../util'
import {Card, FilterInput} from './shared'
import {type FleetAlert, fleetAlertConfig, alertText, DEFAULT_ALERT_CONFIG, reportFleet, setFleetAlertConfig, useFleetAlertConfig, useFleetAlerts} from './fleetAlerts'
import {useToast} from '../ui/Toast'
import {formatUptime, level, pct} from './format'
import type {Host, ServerFleetHost} from '../types'

const WORKERS = 4

type Row = {state: 'waiting'} | {state: 'checking'} | {state: 'done'; r: ServerFleetHost}

/** How bad a row is, to put the servers that need a look first: down, then anything over its limits. */
function trouble(r: Row): number {
  if (r.state !== 'done') return 0
  if (r.r.error) return 100
  const o = r.r.overview
  const worst = Math.max(pct(o.disk_used, o.disk_total), pct(o.mem_used, o.mem_total), Math.round(o.cpu_pct))
  return (r.r.failed_units > 0 ? 50 : 0) + (worst >= 90 ? 30 : worst >= 75 ? 10 : 0)
}

/**
 * Fleet: one line for every saved host (up or down, load, memory, disk, failed services), each from its own short SSH
 * connection that never asks a question: a host whose key is not trusted yet says so instead. Nothing is kept running.
 */
const FLEET_POLL_MS = 30000

export function FleetPage({hosts, onPanel}: {hosts: Host[]; onPanel(h: Host): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const alerts = useFleetAlerts()
  const cfg = useFleetAlertConfig()
  const [rows, setRows] = useState<Record<string, Row>>({})
  const [q, setQ] = useState('')
  const [running, setRunning] = useState(false)
  const alive = useRef(true)
  const round = useRef(0)
  const busyNow = useRef(false)
  const list = useMemo(() => hosts.filter(h => h.hostname), [hosts])

  // quiet: the table stays as it is while the hosts answer again (the timer), instead of going through waiting and checking
  const check = useCallback(async (targets: Host[], quiet = false) => {
    const mine = ++round.current
    busyNow.current = true
    if (!quiet) setRunning(true)
    setRows(prev => Object.fromEntries(targets.map(h => [h.id, prev[h.id]?.state === 'done' ? prev[h.id] : {state: 'waiting'} as Row])))
    let next = 0
    const worker = async () => {
      while (alive.current && mine === round.current && next < targets.length) {
        const h = targets[next++]
        if (!quiet) setRows(p => ({...p, [h.id]: {state: 'checking'}}))
        let r: ServerFleetHost
        try { r = await be.server.fleetHost(h.id) } catch (e) { r = {host_id: h.id, error: errMsg(e), overview: {} as ServerFleetHost['overview'], failed_units: 0, latency_ms: 0} }
        if (alive.current && mine === round.current) { setRows(p => ({...p, [h.id]: {state: 'done', r}})); reportFleet(h, r) }
      }
    }
    await Promise.all(Array.from({length: Math.min(WORKERS, targets.length)}, worker))
    if (alive.current && mine === round.current) { setRunning(false); busyNow.current = false }
  }, [be])

  // checked when the page opens and when the set of hosts changes, not each time the host list is merely reloaded
  const listRef = useRef(list)
  listRef.current = list
  const ids = list.map(h => h.id).join(',')
  useEffect(() => {
    alive.current = true
    void check(listRef.current)
    // asked again by itself: only while the page is in sight, and never on top of a round that is still going
    const timer = window.setInterval(() => { if ((!document.hidden || fleetAlertConfig().notify) && !busyNow.current) void check(listRef.current, true) }, FLEET_POLL_MS)
    return () => { alive.current = false; window.clearInterval(timer) }
  }, [check, ids])

  const shown = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return list
      .filter(h => !needle || (h.label + ' ' + h.hostname).toLowerCase().includes(needle))
      .map(h => ({h, row: rows[h.id] ?? ({state: 'waiting'} as Row)}))
      .sort((a, b) => trouble(b.row) - trouble(a.row) || a.h.label.localeCompare(b.h.label))
  }, [list, rows, q])
  const spinning = useMinBusy(running)
  const done = Object.values(rows).filter(r => r.state === 'done') as Array<{state: 'done'; r: ServerFleetHost}>
  const down = done.filter(r => r.r.error).length

  const alertHosts = useMemo(() => {
    const by = new Map<string, FleetAlert[]>()
    for (const a of alerts) by.set(a.hostId, [...(by.get(a.hostId) ?? []), a])
    return [...by.entries()].sort((x, y) => x[1][0].label.localeCompare(y[1][0].label))
  }, [alerts])
  // a limit is taken when the box is left, not on every key: typing 80 must not pass through 8
  const [resetN, setResetN] = useState(0)
  const setNum = (key: 'disk' | 'mem' | 'certDays', min: number, max: number) => (e: {target: {value: string}}) => {
    const n = Number(e.target.value)
    if (e.target.value.trim() !== '' && Number.isFinite(n) && n >= min && n <= max) setFleetAlertConfig({...cfg, [key]: Math.round(n)})
    else setResetN(x => x + 1) // not a good number: the box shows the limit that is in force
  }
  const toggleNotify = async (on: boolean) => {
    setFleetAlertConfig({...cfg, notify: on})
    if (!on) return
    try { await be.server.notify('Vutty', t('Notifications are on: you are told when a server needs a look.')) } catch (e) { toast.error(t('This system could not show a notification: {error}', {error: errMsg(e)})) }
  }
  const cell = (p: number) => <span className={'sp-pill ' + level(p)}>{p}%</span>
  return (
    <div className="hosts-page" aria-label={t('Fleet')}>
      <div className="sp-bar">
        <FilterInput label={t('Filter servers')} placeholder={t('Filter servers')} value={q} onChange={setQ} />
        <span className="sp-os">{list.length === 0 ? '' : t('{up} of {n} servers answered', {up: done.length - down, n: list.length})}</span>
        <button className="ghost-btn" aria-label={t('Check all servers again')} aria-busy={spinning} disabled={running} onClick={() => void check(list)}><IconRefresh size={14} /> {t('Check again')}</button>
      </div>
      <Card title={t('Alerts')} right={alerts.length > 0 ? <span className="sp-pill bad">{alerts.length}</span> : <span className="sp-pill ok">{t('None')}</span>}>
        {alerts.length > 0 && (
          <ul className="sp-alerts">
            {alertHosts.map(([id, items]) => (
              <li key={id}>
                {items.map(a => <span key={a.key} className={'sp-pill ' + (a.kind === 'down' ? 'bad' : 'warn')}>{a.label + ' · ' + alertText(t, a)}</span>)}
                <button aria-label={t('Open the panel of {name}', {name: items[0].label})} onClick={() => { const h = list.find(x => x.id === id); if (h) onPanel(h) }}>{t('Open panel')}</button>
              </li>
            ))}
          </ul>
        )}
        <div className="sp-bar">
          <label className="sp-os"><input type="checkbox" checked={cfg.notify} onChange={e => void toggleNotify(e.target.checked)} /> {t('Desktop notifications')}</label>
          <label className="sp-os">{t('Disk over')} <input className="sp-num" aria-label={t('Disk alert limit in percent')} inputMode="numeric" defaultValue={cfg.disk} key={'d' + resetN} onBlur={setNum('disk', 1, 100)} />%</label>
          <label className="sp-os">{t('Memory over')} <input className="sp-num" aria-label={t('Memory alert limit in percent')} inputMode="numeric" defaultValue={cfg.mem} key={'m' + resetN} onBlur={setNum('mem', 1, 100)} />%</label>
          <label className="sp-os">{t('Certificate under')} <input className="sp-num" aria-label={t('Certificate alert limit in days')} inputMode="numeric" defaultValue={cfg.certDays} key={'c' + resetN} onBlur={setNum('certDays', 1, 90)} /> {t('days')}</label>
          <button onClick={() => { setFleetAlertConfig({...DEFAULT_ALERT_CONFIG, notify: cfg.notify}); setResetN(x => x + 1) }}>{t('Default limits')}</button>
        </div>
        <div className="sp-note">{t('A server counts as down after two failed checks in a row. With desktop notifications on, the servers are checked every minute while another page or another app is in front.')}</div>
      </Card>
      <h3 className="sec-title">{t('Fleet')}</h3>
      <div className="sp-table"><table>
        <thead><tr><th>{t('Server')}</th><th>{t('Status')}</th><th className="n">{t('CPU')}</th><th className="n">{t('Memory')}</th><th className="n">{t('Disk /')}</th><th className="n">{t('Load average')}</th><th>{t('Services')}</th><th>{t('Uptime')}</th><th /></tr></thead>
        <tbody>
          {shown.map(({h, row}) => {
            const r = row.state === 'done' ? row.r : null
            const o = r && !r.error ? r.overview : null
            return (
              <tr key={h.id}>
                <td><b>{h.label}</b><div className="sp-note m">{h.hostname}</div></td>
                <td>
                  {row.state !== 'done' ? <span className="sp-pill">{row.state === 'checking' ? t('Checking…') : t('Waiting')}</span>
                    : r?.error ? <span className="sp-pill bad" title={r.error}>{t('Down')}</span>
                    : <span className="sp-pill ok" title={t('{ms} ms to connect', {ms: r?.latency_ms ?? 0})}>{t('Up')}</span>}
                  {r?.error && <div className="sp-note">{r.error}</div>}
                </td>
                <td className="n">{o ? cell(Math.round(o.cpu_pct)) : '–'}</td>
                <td className="n">{o ? cell(pct(o.mem_used, o.mem_total)) : '–'}</td>
                <td className="n">{o ? cell(pct(o.disk_used, o.disk_total)) : '–'}</td>
                <td className="n m">{o ? o.load[0].toFixed(2) : '–'}</td>
                <td>{o ? (r!.failed_units > 0 ? <span className="sp-pill bad">{t('{n} failed', {n: r!.failed_units})}</span> : <span className="sp-pill ok">{t('All running')}</span>) : '–'}</td>
                <td className="sp-note">{o ? formatUptime(o.uptime_sec) : '–'}</td>
                <td><div className="sp-acts"><button onClick={() => onPanel(h)}>{t('Open panel')}</button></div></td>
              </tr>
            )
          })}
        </tbody>
      </table>{list.length === 0 && <div className="sp-empty">{t('No hosts yet.')}</div>}</div>
      <div className="sp-note">{t('Each host gets a short connection of its own. A host whose key you have not trusted yet is reported as down: open it once in a terminal and trust it.')}</div>
    </div>
  )
}

import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {Select} from '../ui/Select'
import {errMsg} from '../util'
import type {IncidentBucket, IncidentEvent, ServerIncident} from '../types'
import {Card, Err, Toolbar, useLoad} from './shared'
import {LogRange, useServerClock, type LogWindow} from './LogRange'
import {around, serverClockText} from './logTime'
import type {LogSource} from './ServerPanel'

const HOUR = 3600000
const SPANS: Record<string, number> = {'1h': HOUR, '6h': 6 * HOUR, '24h': 24 * HOUR, '72h': 72 * HOUR}
/** the most bars the strip of errors draws: a longer range puts several minutes in a bar */
const BARS = 96
const iso = (ms: number) => new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')

/** the error counts of the minutes put into at most BARS bars across the range */
export function barsOf(errors: IncidentBucket[], from: number, until: number): {start: number; end: number; count: number}[] {
  const minute = 60000
  const step = Math.max(minute, Math.ceil((until - from) / BARS / minute) * minute)
  const n = Math.max(1, Math.ceil((until - from) / step))
  const bars = Array.from({length: n}, (_, i) => ({start: from + i * step, end: Math.min(until, from + (i + 1) * step), count: 0}))
  for (const e of errors) {
    const at = Date.parse(e.minute)
    const i = Math.floor((at - from) / step)
    if (i >= 0 && i < n) bars[i].count += e.count
  }
  return bars
}

/**
 * The Incident tab: what the server wrote down about a time range, read back from its own journal and Docker: what failed or was
 * restarted, what the kernel killed for memory, when it started or stopped, how many errors there were each minute. It answers
 * "what happened at 3 in the morning" the next day. Every line opens the log at that moment. Nothing is installed, nothing changed
 * (except Keep the journal, which asks first).
 */
export function Incident({sid, onLogs}: {sid: string; onLogs(s: LogSource): void}) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const toast = useToast()
  const confirm = useConfirm()
  const clock = useServerClock(sid)
  const [span, setSpan] = useState('24h')
  const [custom, setCustom] = useState<LogWindow | null>(null)
  const base = useRef<{now: number; at: number} | null>(null) // the time on the server when asked, and when here
  useEffect(() => { void clock.get().then(c => { base.current = {now: Date.parse(c.now), at: Date.now()} }, () => {}) }, [clock.get]) // eslint-disable-line react-hooks/exhaustive-deps
  const [win, setWin] = useState<LogWindow | null>(null) // the range the answer is for
  const load = useCallback(async () => {
    const c = await clock.get()
    base.current = {now: Date.parse(c.now), at: Date.now()}
    const w = custom ?? {from: iso(base.current.now - SPANS[span]), until: iso(base.current.now)}
    setWin(w)
    return be.server.incident(sid, w.from, w.until)
  }, [be, sid, span, custom, clock.get]) // eslint-disable-line react-hooks/exhaustive-deps
  const {data, error, busy, reload} = useLoad<ServerIncident>(load, null)
  const asked = useRef(span + '|' + custom?.from + '|' + custom?.until)
  useEffect(() => {
    const now = span + '|' + custom?.from + '|' + custom?.until
    if (asked.current !== now) { asked.current = now; reload() }
  }, [span, custom, reload])
  const offset = clock.clock?.offset ?? 0
  const at = (v: string) => serverClockText(v, offset)
  const logsAround = (when: string, minutes: number, kind: LogSource['kind'] = '', name = '') => onLogs({kind, name, range: around(when, minutes)})
  const bars = useMemo(() => (data && win ? barsOf(data.errors, Date.parse(win.from), Date.parse(win.until)) : []), [data, win])
  const peak = Math.max(1, ...bars.map(b => b.count))
  const keep = async () => {
    if (!(await confirm(t('Keep the journal on disk? A folder /var/log/journal is made and the journal service restarts, so the log survives a reboot. It uses disk space (up to 10% of the disk by default).'), {confirmLabel: t('Keep it')}))) return
    try { await be.server.journalKeep(sid); toast.info(t('The journal is kept on disk from now on.')) } catch (e) { toast.error(errMsg(e)) }
    reload()
  }
  const kindLabel: Record<IncidentEvent['kind'], string> = {failed: t('Failed'), restart: t('Restarted'), oom: t('Out of memory'), boot: t('Started'), shutdown: t('Shutdown')}
  const bad = (k: IncidentEvent['kind']) => k === 'failed' || k === 'oom'
  const startsLate = data && win && data.oldest && Date.parse(data.oldest) > Date.parse(win.from)
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <Select className="sp-pick" label={t('Time range')} value={custom ? 'custom' : span} onChange={v => {
          if (v === 'custom') { setCustom(win ?? {from: iso(Date.now() - HOUR), until: iso(Date.now())}); return }
          setCustom(null); setSpan(v)
        }} options={[{value: '1h', label: t('Last hour')}, {value: '6h', label: t('Last 6 hours')}, {value: '24h', label: t('Last 24 hours')}, {value: '72h', label: t('Last 3 days')}, {value: 'custom', label: t('Choose a range…')}]} />
        {custom && <LogRange range={custom} clock={clock.clock} onRange={setCustom} />}
      </Toolbar>
      <Err error={error} />
      {data && !data.persistent && <div className="sp-note k8s-hint" role="status">{t('The journal of this server is kept in memory only: a reboot or a crash that restarts it erases the log of what happened before.')} <button onClick={() => void keep()}>{t('Keep it on disk')}</button></div>}
      {data && data.limited && <div className="sp-note k8s-hint" role="status">{t('This login sees only its own messages, so the system log is incomplete. Add it to the systemd-journal group, or log in as root.')}</div>}
      {startsLate && <div className="sp-note k8s-hint" role="status">{t('The journal starts at {time}: nothing is left of the time before.', {time: at(data!.oldest)})}</div>}
      {data && (
        <>
          <Card title={t('Errors by minute')} right={<span className="sp-note">{t('Server time ({zone})', {zone: clock.clock?.zone ?? ''})}</span>}>
            {data.errors.length === 0 ? <div className="sp-empty">{t('No error lines in this time range.')}</div> : (
              <div className="sp-strip" role="group" aria-label={t('Errors by minute')}>
                {bars.map(b => (
                  <button key={b.start} className={'sp-bar-col' + (b.count ? '' : ' none')} disabled={!b.count}
                    style={{height: b.count ? Math.max(8, Math.round((b.count / peak) * 100)) + '%' : '2px'}}
                    title={`${at(iso(b.start))} · ${tn(b.count, '{n} error', '{n} errors')}`}
                    aria-label={`${at(iso(b.start))}: ${tn(b.count, '{n} error', '{n} errors')}`}
                    onClick={() => onLogs({kind: '', name: '', range: {from: iso(b.start), until: iso(b.end)}})} />
                ))}
              </div>
            )}
          </Card>
          <Card title={t('What happened')} count={data.events.length}>
            {data.events.length === 0 ? <div className="sp-empty">{t('No failed service, restart, memory kill or reboot in this time range.')}</div> : (
              <div className="sp-table"><table>
                <thead><tr><th>{t('Time')}</th><th>{t('What')}</th><th>{t('Detail')}</th><th /></tr></thead>
                <tbody>{data.events.map((e, i) => (
                  <tr key={i}>
                    <td className="m">{at(e.time)}</td>
                    <td><span className={'sp-pill ' + (bad(e.kind) ? 'bad' : e.kind === 'restart' ? 'warn' : '')}>{kindLabel[e.kind]}</span></td>
                    <td className="m">{e.text}</td>
                    <td><button onClick={() => logsAround(e.time, 5)}>{t('Logs')}</button></td>
                  </tr>
                ))}</tbody>
              </table></div>
            )}
            {data.truncated && <div className="sp-note">{t('Only the last {n} are shown: choose a shorter range to see the rest.', {n: data.events.length})}</div>}
          </Card>
          {data.has_docker && (
            <Card title={t('Containers')} count={data.containers.length}>
              {data.containers.length === 0 ? <div className="sp-empty">{t('No container started or stopped in this time range.')}</div> : (
                <div className="sp-table"><table>
                  <thead><tr><th>{t('Container')}</th><th>{t('Started')}</th><th>{t('Stopped')}</th><th className="n">{t('Restarts')}</th><th /></tr></thead>
                  <tbody>{data.containers.map(c => (
                    <tr key={c.name}>
                      <td className="m">{c.name}{c.oom && <> <span className="sp-pill bad">{t('Out of memory')}</span></>}</td>
                      <td className="m">{c.started ? at(c.started) : ''}</td>
                      <td className="m">{c.running ? t('running') : c.finished ? at(c.finished) + ' · ' + t('exit {code}', {code: c.exit_code}) : ''}</td>
                      <td className="n">{c.restarts}</td>
                      <td><button onClick={() => logsAround(c.finished || c.started, 5, 'container', c.name)}>{t('Logs')}</button></td>
                    </tr>
                  ))}</tbody>
                </table></div>
              )}
            </Card>
          )}
        </>
      )}
    </div>
  )
}

import {useCallback, useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import type {ServerClock} from '../types'
import {fromServerLocal, offsetText, toServerLocal} from './logTime'

/** The time on the server, asked once when first needed: a time typed for the server and one read from a log line meet here. */
export function useServerClock(sid: string) {
  const be = useBackend()
  const [clock, setClock] = useState<ServerClock | null>(null)
  const asked = useRef<Promise<ServerClock> | null>(null)
  useEffect(() => { asked.current = null; setClock(null) }, [sid])
  const get = useCallback(() => {
    if (!asked.current) asked.current = be.server.clock(sid).then(c => { setClock(c); return c }, e => { asked.current = null; throw e })
    return asked.current
  }, [be, sid])
  return {clock, get}
}

export interface LogWindow {from: string; until: string}

/** The two boxes of a time range, in the time of the server, and the button that applies them. */
export function LogRange({range, clock, onRange}: {range: LogWindow; clock: ServerClock | null; onRange(r: LogWindow): void}) {
  const t = useT()
  const offset = clock?.offset ?? 0
  const [from, setFrom] = useState(toServerLocal(range.from, offset))
  const [until, setUntil] = useState(toServerLocal(range.until, offset))
  // the range changed from outside (a line was picked, the clock arrived): the boxes follow it
  useEffect(() => { setFrom(toServerLocal(range.from, offset)); setUntil(toServerLocal(range.until, offset)) }, [range.from, range.until, offset])
  const f = fromServerLocal(from, offset), u = fromServerLocal(until, offset)
  const bad = !f || !u || Date.parse(u) <= Date.parse(f)
  const zone = clock ? t('Server time ({zone}, UTC{offset})', {zone: clock.zone, offset: offsetText(clock.offset)}) : t('Server time')
  return (
    <span className="sp-range" role="group" aria-label={t('Time range of the log')}>
      <label>{t('From')} <input type="datetime-local" value={from} onChange={e => setFrom(e.target.value)} aria-label={t('From')} /></label>
      <label>{t('To')} <input type="datetime-local" value={until} onChange={e => setUntil(e.target.value)} aria-label={t('To')} /></label>
      <button className="primary" disabled={bad} title={bad ? t('The end must be after the start') : undefined} onClick={() => f && u && onRange({from: f, until: u})}>{t('Show')}</button>
      <span className="sp-note">{zone}</span>
    </span>
  )
}

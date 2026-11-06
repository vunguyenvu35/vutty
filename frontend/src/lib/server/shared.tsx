import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import {useBackend} from '../backend'
import type {TFn} from '../i18n'
import {useT} from '../i18n'
import {AuthPrompt} from '../ui/AuthPrompt'
import {IconChevron, IconClose, IconRefresh} from '../ui/Icons'
import {useMinBusy} from '../ui/useMinBusy'
import {authenticity} from '../sftp/RemotePane'
import {errMsg} from '../util'
import type {AuthChallenge, Host, HostKeyPrompt} from '../types'

/** A filter box with a × that empties it (only while there is something to clear) */
export function FilterInput({value, onChange, label, placeholder, invalid, className = ''}: {value: string; onChange(v: string): void; label: string; placeholder?: string; invalid?: boolean; className?: string}) {
  const t = useT()
  return (
    <span className={'sp-filter sp-filter-box ' + className}>
      <input aria-label={label} placeholder={placeholder} aria-invalid={invalid || undefined} value={value} onChange={e => onChange(e.target.value)} />
      {value !== '' && <button type="button" className="sp-filter-clear" aria-label={t('Clear filter')} title={t('Clear filter')} onClick={e => { onChange(''); e.currentTarget.previousElementSibling instanceof HTMLInputElement && e.currentTarget.previousElementSibling.focus() }}><IconClose size={10} /></button>}
    </span>
  )
}

let seq = 0

export interface ServerConnection {
  phase: 'connecting' | 'ready' | 'closed'
  /** the session to ask things of, '' until the connection is ready */
  sid: string
  /** the question or the error to show while the connection is not ready */
  notice: JSX.Element | null
}

/**
 * One SSH connection for the Server section or a Server tab: opened when the component shows, closed when it goes away, and asked
 * again with Reconnect. The host key question and the password question of the connection are part of `notice`.
 */
export function useServerConnection(host: Host): ServerConnection {
  const be = useBackend()
  const t = useT()
  const alive = useRef(true)
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<'connecting' | 'ready' | 'closed'>('connecting')
  const [reason, setReason] = useState('')
  const [prompt, setPrompt] = useState<{key: string; p: HostKeyPrompt} | null>(null)
  const [auth, setAuth] = useState<{key: string; c: AuthChallenge} | null>(null)
  const [sid, setSid] = useState('')

  useEffect(() => {
    alive.current = true
    let opened = ''
    const key = `server-${Date.now().toString(36)}-${++seq}`
    const offs: Array<() => void> = [be.on(`term:hostkey:${key}`, (p: HostKeyPrompt) => setPrompt({key, p})), be.on(`term:auth:${key}`, (c: AuthChallenge) => setAuth({key, c}))]
    setPhase('connecting')
    setReason('')
    setSid('')
    ;(async () => {
      try {
        const info = await be.server.open(host.id, key)
        if (!alive.current) { void be.server.close(info.sid).catch(() => {}); return }
        opened = info.sid
        offs.push(be.on(`server:closed:${info.sid}`, () => { setPhase('closed'); setReason(t('The connection was lost.')); setSid(''); opened = '' }))
        setPrompt(null)
        setAuth(null)
        setSid(info.sid)
        setPhase('ready')
      } catch (e) {
        if (alive.current) { setPrompt(null); setAuth(null); setPhase('closed'); setReason(errMsg(e)) }
      }
    })()
    return () => {
      alive.current = false
      offs.forEach(o => o())
      if (opened) void be.server.close(opened).catch(() => {})
    }
  }, [be, host.id, attempt, t])

  const answer = (trust: boolean) => {
    if (!prompt) return
    void be.session.trustHostKey(prompt.key, trust ? prompt.p.fingerprint : '').catch(e => setReason(errMsg(e)))
    setPrompt(null)
  }

  const notice = (
    <>
      {phase === 'connecting' && !prompt && !auth && <div className="side-empty">{t('Connecting to {name}…', {name: host.label})}</div>}
      <AuthPrompt inline challenge={auth?.c ?? null} promptKey={auth?.key ?? ''} onDone={() => setAuth(null)} onError={setReason} />
      {phase === 'closed' && (
        <div className="side-empty" role="alert">
          <div>{reason || t('Disconnected.')}</div>
          <button onClick={() => setAttempt(a => a + 1)}>{t('Reconnect')}</button>
        </div>
      )}
      {prompt && (
        <div className="side-empty" role="alertdialog" aria-label={t('Host key confirmation')}>
          <div>{authenticity(t("The authenticity of {host} can't be established.", {host: '\u0001'}), prompt.p.host)}</div>
          <div className="fp" data-testid="fingerprint">{prompt.p.fingerprint}</div>
          <div className="row"><button className="primary" onClick={() => answer(true)}>{t('Trust and connect')}</button><button onClick={() => answer(false)}>{t('Cancel')}</button></div>
        </div>
      )}
    </>
  )
  return {phase, sid, notice}
}

/**
 * Runs load now and then every `ms` while the view is shown and the window is visible. A slow answer for an older call never
 * replaces a newer one. Returns the state of the last call and a function that asks again at once.
 */
export function useLoad<T>(load: () => Promise<T>, ms: number | null): {data: T | null; error: string; busy: boolean; reload(): void} {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(true)
  const alive = useRef(true)
  const latest = useRef(0)
  const pending = useRef(false) // a load is on its way: the timer waits for it (asking again would make its answer stale, and a slow one would never show)
  const loadRef = useRef(load)
  loadRef.current = load
  const reload = useCallback(() => {
    const mine = ++latest.current
    pending.current = true
    setBusy(true)
    loadRef.current().then(d => {
      if (alive.current && mine === latest.current) { setData(d); setError('') }
    }, e => {
      if (alive.current && mine === latest.current) setError(errMsg(e))
    }).finally(() => {
      if (mine === latest.current) pending.current = false
      if (alive.current && mine === latest.current) setBusy(false)
    })
  }, [])
  useEffect(() => {
    alive.current = true
    reload()
    const timer = ms ? window.setInterval(() => { if (!document.hidden && !pending.current) reload() }, ms) : 0
    return () => { alive.current = false; if (timer) window.clearInterval(timer) }
  }, [reload, ms])
  return {data, error, busy, reload}
}

export type ServerAction = 'start' | 'stop' | 'restart'
export type ServiceAction = ServerAction | 'enable' | 'disable'

/** The question asked before a service or container is started, stopped or restarted: nothing the panel does to a server runs unasked. */
export function actionQuestion(t: TFn, kind: 'service' | 'container', action: ServiceAction, name: string): {message: string; label: string} {
  const p = {name}
  if (kind === 'service') {
    if (action === 'enable') return {message: t('Start the service {name} every time the server boots?', p), label: t('Enable')}
    if (action === 'disable') return {message: t('Stop starting the service {name} when the server boots? It keeps running until you stop it.', p), label: t('Disable')}
    if (action === 'start') return {message: t('Start the service {name}?', p), label: t('Start')}
    if (action === 'restart') return {message: t('Restart the service {name}? Its connections drop for a moment.', p), label: t('Restart')}
    return {message: t('Stop the service {name}?', p), label: t('Stop')}
  }
  if (action === 'start') return {message: t('Start the container {name}?', p), label: t('Start')}
  if (action === 'restart') return {message: t('Restart the container {name}? Its connections drop for a moment.', p), label: t('Restart')}
  return {message: t('Stop the container {name}?', p), label: t('Stop')}
}

export type StackAction = ServerAction | 'update' | 'up' | 'down'

/** The question asked before a Compose project is started, stopped, restarted or updated. */
export function stackQuestion(t: TFn, action: StackAction, name: string): {message: string; label: string} {
  const p = {name}
  if (action === 'up') return {message: t('Bring the stack {name} up? It creates the containers that are missing and recreates the ones whose configuration changed.', p), label: t('Up')}
  if (action === 'down') return {message: t('Take the stack {name} down? Its containers and network are removed; the volumes stay. This panel cannot bring it back: run docker compose up in its folder.', p), label: t('Down')}
  if (action === 'update') return {message: t('Update the stack {name}? It pulls the new images and recreates the containers that changed.', p), label: t('Update')}
  if (action === 'start') return {message: t('Start all containers of {name}?', p), label: t('Start')}
  if (action === 'restart') return {message: t('Restart all containers of {name}? Their connections drop for a moment.', p), label: t('Restart')}
  return {message: t('Stop all containers of {name}?', p), label: t('Stop')}
}

/** The bar above a table: what is on the left, and the refresh button at the right. */
/** The refresh button alone: for a screen that has nothing else for a bar, it goes in the head of its first box instead of making a row of its own. */
export function RefreshButton({busy, onReload}: {busy: boolean; onReload(): void}) {
  const t = useT()
  const spinning = useMinBusy(busy)
  return <button className="icon ghost" aria-label={t('Refresh')} title={t('Refresh')} aria-busy={spinning} onClick={onReload}><IconRefresh size={14} /></button>
}

export function Toolbar({busy, onReload, children}: {busy: boolean; onReload(): void; children?: ReactNode}) {
  return (
    <div className="sp-bar">
      {children}
      <RefreshButton busy={busy} onReload={onReload} />
    </div>
  )
}

/**
 * What a screen shows while its first answer is on the way: a small ring turning in the middle and a line that says what is being loaded.
 * It fades in after a moment, so an answer that comes at once is not preceded by a flash; the ring stands still for a person who asked
 * for less motion. `lines` is the small form, in the line, for a dialog.
 */
export function Loading({kind = 'table', what}: {kind?: 'table' | 'overview' | 'detail' | 'lines'; what?: string}) {
  const t = useT()
  const text = what ?? (kind === 'overview' ? t('Loading the overview…') : kind === 'detail' ? t('Loading the details…') : t('Loading…'))
  return (
    <div className={'ld' + (kind === 'lines' ? ' ld-inline' : '')} role="status" aria-busy="true" aria-live="polite" data-kind={kind}>
      <span className="ld-ring" aria-hidden="true" />
      <span className="ld-text">{text}</span>
    </div>
  )
}

export const Err = ({error}: {error: string}) => (error ? <div className="error" role="alert">{error}</div> : null)

/** A box of the page with a title: the unit of every section that has more than one thing to show. */
const FOLD_KEY = 'vutty.panel.folded'
const readFolded = (): string[] => { try { const v = JSON.parse(localStorage.getItem(FOLD_KEY) ?? '[]'); return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [] } catch { return [] } }

/** Which boxes of the panel are put away, remembered for next time (a convenience: if storage fails they just start open). */
export function useFolded(): [(key: string) => boolean, (key: string) => void] {
  const [set, setSet] = useState<string[]>(readFolded)
  const toggle = (key: string) => {
    // read again before writing: another box may have changed it since this one was built
    const cur = readFolded()
    const next = cur.includes(key) ? cur.filter(x => x !== key) : [...cur, key]
    try { localStorage.setItem(FOLD_KEY, JSON.stringify(next)) } catch { /* not remembered */ }
    setSet(next)
  }
  return [key => set.includes(key), toggle]
}

/** A click on the empty part of a box's head folds it too; a click on a control in the head (a button, a box to type in) is that control's own. */
export const onHeadClick = (fold: () => void) => (e: {target: EventTarget}) => {
  if (!(e.target as Element).closest?.('button, input, select, textarea, a, label, [role=menuitem]')) fold()
}

export function Card({title, children, right, collapsible = true, closed: closedProp, onToggle, count, filter}: {
  title: string; children: ReactNode; right?: ReactNode
  /** the title closes and opens the box (a long list can be put away); every box does, unless it is told not to */
  collapsible?: boolean
  /** the screen keeps whether the box is closed (so it stays closed when the screen is built again); without it the box keeps it itself */
  closed?: boolean
  onToggle?(): void
  /** shown beside the title: how many there are, or "shown of all" while a search is on */
  count?: string | number
  /** a search box in the head that keeps only what contains what is typed; the box owns no text, the screen filters its own rows */
  filter?: {value: string; onChange(v: string): void; label: string}
}) {
  const t = useT()
  const [isFolded, fold] = useFolded()
  const closed = closedProp ?? isFolded(title)
  const toggle = onToggle ?? (() => fold(title))
  return (
    <section className="sp-card sp-wide">
      <div className={'sp-card-head' + (collapsible ? ' foldable' : '')} onClick={collapsible ? onHeadClick(toggle) : undefined}>
        {collapsible
          ? <button className="sp-card-fold" aria-expanded={!closed} aria-label={title} onClick={toggle}>
              <span className={'k8s-chev' + (closed ? '' : ' open')}><IconChevron size={12} /></span><h3>{title}</h3>{count !== undefined && <small className="sp-count">{count}</small>}
            </button>
          : <h3>{title}</h3>}
        {filter && !closed && <FilterInput className="sp-card-filter" label={filter.label} placeholder={t('Search')} value={filter.value} onChange={filter.onChange} />}
        {right}
      </div>
      {!closed && children}
    </section>
  )
}

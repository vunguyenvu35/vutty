import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import {useBackend} from '../backend'
import type {TFn} from '../i18n'
import {useT} from '../i18n'
import {AuthPrompt} from '../ui/AuthPrompt'
import {IconRefresh} from '../ui/Icons'
import {useMinBusy} from '../ui/useMinBusy'
import {authenticity} from '../sftp/RemotePane'
import {errMsg} from '../util'
import type {AuthChallenge, Host, HostKeyPrompt} from '../types'

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
  const loadRef = useRef(load)
  loadRef.current = load
  const reload = useCallback(() => {
    const mine = ++latest.current
    setBusy(true)
    loadRef.current().then(d => {
      if (alive.current && mine === latest.current) { setData(d); setError('') }
    }, e => {
      if (alive.current && mine === latest.current) setError(errMsg(e))
    }).finally(() => {
      if (alive.current && mine === latest.current) setBusy(false)
    })
  }, [])
  useEffect(() => {
    alive.current = true
    reload()
    const timer = ms ? window.setInterval(() => { if (!document.hidden) reload() }, ms) : 0
    return () => { alive.current = false; if (timer) window.clearInterval(timer) }
  }, [reload, ms])
  return {data, error, busy, reload}
}

export type ServerAction = 'start' | 'stop' | 'restart'

/** The question asked before a service or container is started, stopped or restarted: nothing the panel does to a server runs unasked. */
export function actionQuestion(t: TFn, kind: 'service' | 'container', action: ServerAction, name: string): {message: string; label: string} {
  const p = {name}
  if (kind === 'service') {
    if (action === 'start') return {message: t('Start the service {name}?', p), label: t('Start')}
    if (action === 'restart') return {message: t('Restart the service {name}? Its connections drop for a moment.', p), label: t('Restart')}
    return {message: t('Stop the service {name}?', p), label: t('Stop')}
  }
  if (action === 'start') return {message: t('Start the container {name}?', p), label: t('Start')}
  if (action === 'restart') return {message: t('Restart the container {name}? Its connections drop for a moment.', p), label: t('Restart')}
  return {message: t('Stop the container {name}?', p), label: t('Stop')}
}

export type StackAction = ServerAction | 'update'

/** The question asked before a Compose project is started, stopped, restarted or updated. */
export function stackQuestion(t: TFn, action: StackAction, name: string): {message: string; label: string} {
  const p = {name}
  if (action === 'update') return {message: t('Update the stack {name}? It pulls the new images and recreates the containers that changed.', p), label: t('Update')}
  if (action === 'start') return {message: t('Start all containers of {name}?', p), label: t('Start')}
  if (action === 'restart') return {message: t('Restart all containers of {name}? Their connections drop for a moment.', p), label: t('Restart')}
  return {message: t('Stop all containers of {name}?', p), label: t('Stop')}
}

/** The bar above a table: what is on the left, and the refresh button at the right. */
export function Toolbar({busy, onReload, children}: {busy: boolean; onReload(): void; children?: ReactNode}) {
  const t = useT()
  const spinning = useMinBusy(busy)
  return (
    <div className="sp-bar">
      {children}
      <button className="icon ghost" aria-label={t('Refresh')} title={t('Refresh')} aria-busy={spinning} onClick={onReload}><IconRefresh size={14} /></button>
    </div>
  )
}

export const Err = ({error}: {error: string}) => (error ? <div className="error" role="alert">{error}</div> : null)

/** A box of the page with a title: the unit of every section that has more than one thing to show. */
export function Card({title, children, right}: {title: string; children: ReactNode; right?: ReactNode}) {
  return (
    <section className="sp-card sp-wide">
      <div className="sp-card-head"><h3>{title}</h3>{right}</div>
      {children}
    </section>
  )
}

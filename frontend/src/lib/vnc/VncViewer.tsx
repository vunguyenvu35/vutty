import {useEffect, useRef, useState} from 'react'
import RFB from '@novnc/novnc'
import {useT} from '../i18n'
import type {Host} from '../types'
import {useServerConnection} from '../server/shared'

type Phase = 'connecting' | 'credentials' | 'ready' | 'closed'

/**
 * A VNC server drawn in a tab. The bytes travel through a path the app opens on this computer (see VncService; through the SSH login
 * when the host says so); noVNC speaks the protocol and draws the picture. The VNC password is typed here, sent to noVNC and
 * never kept.
 */
export function VncViewer({host, visible}: {host: Host; visible: boolean}) {
  const t = useT()
  const link = useServerConnection(host, 'vnc')
  const box = useRef<HTMLDivElement>(null)
  const rfb = useRef<RFB | null>(null)
  const [phase, setPhase] = useState<Phase>('connecting')
  const [message, setMessage] = useState('')
  const [needs, setNeeds] = useState<string[]>([])
  const [user, setUser] = useState('')
  const [pass, setPass] = useState('')
  const [fit, setFit] = useState(true)
  const [viewOnly, setViewOnly] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    if (link.phase !== 'ready' || !link.url || !box.current) return
    setPhase('connecting'); setMessage(''); setPass('')
    const r = new RFB(box.current, link.url, {shared: true})
    r.scaleViewport = true
    r.resizeSession = false
    r.background = 'transparent'
    rfb.current = r
    const onConnect = () => setPhase('ready')
    const onCredentials = (e: Event) => { setNeeds((e as CustomEvent<{types: string[]}>).detail?.types ?? ['password']); setPhase('credentials') }
    const onSecurity = (e: Event) => setMessage((e as CustomEvent<{reason?: string}>).detail?.reason || t('The VNC server refused the login.'))
    const onDisconnect = (e: Event) => {
      const clean = (e as CustomEvent<{clean: boolean}>).detail?.clean
      setPhase('closed')
      setMessage(m => m || (clean ? t('The VNC session ended.') : t('The VNC connection was lost.')))
    }
    r.addEventListener('connect', onConnect)
    r.addEventListener('credentialsrequired', onCredentials)
    r.addEventListener('securityfailure', onSecurity)
    r.addEventListener('disconnect', onDisconnect)
    return () => {
      r.removeEventListener('connect', onConnect)
      r.removeEventListener('credentialsrequired', onCredentials)
      r.removeEventListener('securityfailure', onSecurity)
      r.removeEventListener('disconnect', onDisconnect)
      try { r.disconnect() } catch { /* already closed */ }
      rfb.current = null
    }
  }, [link.phase, link.url, attempt, t])

  useEffect(() => { if (rfb.current) rfb.current.scaleViewport = fit }, [fit, phase])
  useEffect(() => { if (rfb.current) rfb.current.viewOnly = viewOnly }, [viewOnly, phase])
  useEffect(() => { if (visible && phase === 'ready') rfb.current?.focus() }, [visible, phase])

  const send = (e: {preventDefault(): void}) => {
    e.preventDefault()
    rfb.current?.sendCredentials({username: needs.includes('username') ? user : undefined, password: pass})
    setPass('')
    setPhase('connecting')
  }
  const again = () => { setPhase('connecting'); setMessage(''); setAttempt(a => a + 1) }
  const ended = link.phase === 'closed' || phase === 'closed'

  return (
    <div className="vnc" role="region" aria-label={t('VNC of {name}', {name: host.label})}>
      <div className="vnc-bar">
        <h1>{host.label}</h1>
        <span>{host.vnc_via_ssh ? t('through SSH') : t('direct, not encrypted')}</span>
        <span className="grow" />
        {phase === 'ready' && <>
          <button aria-pressed={fit} className={fit ? 'primary' : ''} onClick={() => setFit(f => !f)} title={t('Scale the screen to fit this tab')}>{t('Fit')}</button>
          <button aria-pressed={viewOnly} className={viewOnly ? 'primary' : ''} onClick={() => setViewOnly(v => !v)} title={t('Look without sending any key or click')}>{t('View only')}</button>
          <button onClick={() => rfb.current?.sendCtrlAltDel()} title={t('Send Ctrl+Alt+Del to the remote computer')}>{t('Ctrl+Alt+Del')}</button>
        </>}
      </div>
      <div className="vnc-stage">
        <div ref={box} className="vnc-screen" hidden={phase !== 'ready'} />
        {link.phase !== 'ready' && link.notice}
        {link.phase === 'ready' && phase === 'connecting' && <div className="side-empty">{t('Connecting to {name}…', {name: host.label})}</div>}
        {phase === 'credentials' && (
          <form className="vnc-login" onSubmit={send} aria-label={t('VNC login')}>
            <div>{t('{name} asks for a VNC password.', {name: host.label})}</div>
            {needs.includes('username') && <input aria-label={t('VNC username')} value={user} onChange={e => setUser(e.target.value)} placeholder={t('Username')} autoComplete="off" />}
            <input aria-label={t('VNC password')} type="password" autoFocus value={pass} onChange={e => setPass(e.target.value)} placeholder={t('Password')} autoComplete="off" />
            <div className="row"><button className="primary" type="submit">{t('Connect')}</button></div>
            <div className="muted">{t('It is sent to the server and not kept.')}</div>
          </form>
        )}
        {ended && link.phase === 'ready' && (
          <div className="side-empty" role="alert">
            <div>{message || t('The VNC connection ended.')}</div>
            <button onClick={again}>{t('Reconnect')}</button>
          </div>
        )}
      </div>
    </div>
  )
}

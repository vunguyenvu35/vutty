import {getHistory, recordCommand} from './history'
import {createSuggester, SUGGEST_POPUP, type Suggester, type SuggestTerm} from './suggester'
import type {Snippet} from '../types'
import {ConnectScreen} from './ConnectScreen'
import {useEffect, useRef, useState} from 'react'
import {ITheme, Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import {SearchAddon} from '@xterm/addon-search'
import {WebLinksAddon} from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import {useBackend} from '../backend'
import {b64ToBytes, errMsg} from '../util'
import {parseCwd, registerPane, registerSelection, setPaneCwd, setPaneSession, setPaneStatus, writeToPane} from './panes'
import {createHighlighter, HighlightTerm} from './highlight'
import {attachGpu, type Gpu} from './gpu'
import {matches} from '../shortcuts/shortcuts'
import {IconArrowDown, IconArrowUp} from '../ui/Icons'
import {AuthPrompt} from '../ui/AuthPrompt'
import {useConfirm} from '../ui/Confirm'
import type {AuthChallenge, ClosedInfo, HostKeyPrompt} from '../types'
import {useT} from '../i18n'
import {composedInput, followReplacements, imeColours, type ComposingTerm} from './ime'

/** how many parsed bytes are reported to the backend at once (flow control); a smaller rest is reported after 20 ms */
const ACK_EVERY = 128 * 1024
/** how long after the shell is ready an initial line is typed: the prompt is drawn by then */
export const INITIAL_INPUT_DELAY = 300

interface Props {
  hostId: string
  /** the host is marked prod: the pane says so, and (with confirmProd) asks before text with a line break is pasted */
  prod?: boolean
  /** the "be careful on production hosts" setting (default on) */
  confirmProd?: boolean
  /** identifies the pane so other parts of the UI (snippets) can type into it */
  paneId?: string
  visible: boolean
  focused: boolean
  fontSize: number
  fontFamily: string
  termTheme: ITheme
  /** key combination that toggles the scrollback search (default Mod+F) */
  searchCombo?: string
  cursorStyle: 'block' | 'bar' | 'underline'
  cursorBlink: boolean
  scrollback: number
  /** colour URLs, IPs, paths and error / warning / success words (default on) */
  highlight?: boolean
  /** draw the pane that has the focus with WebGL (default on); the others, and a pane where WebGL fails, use the DOM renderer */
  gpu?: boolean
  /** copy the selection to the clipboard as soon as it is made (default on) */
  copyOnSelect?: boolean
  /** what a right click does (default: paste) */
  rightClick?: 'paste' | 'menu' | 'none'
  /** key combinations for Copy and Paste (platform defaults when not given) */
  copyCombo?: string
  /** keys the app owns and the terminal must let through to it (Next / Previous tab): xterm would send them to the shell and swallow them */
  appCombos?: string[]
  pasteCombo?: string
  /** command suggestions while typing (the Suggest setting; default on) */
  suggest?: boolean
  /** also read the shell history of the server for them */
  suggestHost?: boolean
  /** key that opens the list of suggestions */
  suggestCombo?: string
  /** who this pane connects to: drawn on the connecting screen */
  connect?: {label: string; os?: string; subtitle: string}
  onShowLogs?(): void
  /** the Close button of the connecting screen */
  onCloseTab?(): void
  /** a line typed and run once, a moment after the shell is connected (the Shell of a Kubernetes pod) */
  initialInput?: string
  /** what the user typed in this pane (the Workspace sends it to the other panes while broadcasting) */
  onTyped?(data: string): void
  onFocus(): void
  /** The remote shell ended cleanly (user typed exit): the pane can go away. */
  onExit(): void
}

type State = 'connecting' | 'ready' | 'closed'

export {composedInput, followReplacements, imeColours} from './ime'

/** One SSH session rendered with xterm.js. Keystrokes go to Go, output arrives as batched events. */
export function TerminalPane(p: Props) {
  const t = useT()
  const be = useBackend()
  const host = useRef<HTMLDivElement>(null)
  const term = useRef<Terminal>()
  const fit = useRef<FitAddon>()
  const gpu = useRef<{term: Terminal; gpu: Gpu} | null>(null)
  const gpuFailed = useRef(false)
  const search = useRef<SearchAddon>()
  const highlighter = useRef<ReturnType<typeof createHighlighter>>()
  const sid = useRef('')
  const stateRef = useRef<State>('connecting')
  const [state, setState] = useState<State>('connecting')
  const [prompt, setPrompt] = useState<HostKeyPrompt | null>(null)
  const [auth, setAuth] = useState<AuthChallenge | null>(null)
  const [closed, setClosed] = useState<ClosedInfo | null>(null)
  const [showSearch, setShowSearch] = useState(false)
  const [needle, setNeedle] = useState('')
  const onExit = useRef(p.onExit)
  const searchCombo = useRef(p.searchCombo ?? 'Mod+F')
  searchCombo.current = p.searchCombo ?? 'Mod+F'
  const copyCombo = useRef(p.copyCombo ?? 'Mod+Shift+C')
  const pasteCombo = useRef(p.pasteCombo ?? 'Mod+Shift+V')
  const copyOnSelect = useRef(p.copyOnSelect ?? true)
  const rightClick = useRef(p.rightClick ?? 'paste')
  copyCombo.current = p.copyCombo ?? 'Mod+Shift+C'
  const appCombos = useRef<string[]>([])
  appCombos.current = p.appCombos ?? []
  pasteCombo.current = p.pasteCombo ?? 'Mod+Shift+V'
  copyOnSelect.current = p.copyOnSelect ?? true
  rightClick.current = p.rightClick ?? 'paste'
  const [menu, setMenu] = useState<{x: number; y: number} | null>(null)
  const confirm = useConfirm()
  const confirmRef = useRef(confirm)
  confirmRef.current = confirm
  const guarded = useRef(false)
  guarded.current = !!p.prod && (p.confirmProd ?? true)
  /** text with a line break runs as soon as it arrives: in a production terminal it is asked about first */
  const okToPaste = async (text: string): Promise<boolean> => {
    if (!guarded.current || !/[\r\n]/.test(text)) return true
    const lines = text.replace(/[\r\n]+$/, '').split(/\r\n|\r|\n/).length
    return confirmRef.current(
      lines === 1 ? t('Paste a line that ends with Enter into a PRODUCTION terminal? It runs at once.') : t('Paste {lines} lines into a PRODUCTION terminal? Each line runs as soon as it arrives.', {lines}),
      {confirmLabel: t('Paste'), danger: true})
  }
  const focusedRef = useRef(p.focused && p.visible)
  focusedRef.current = p.focused && p.visible
  const suggestOn = useRef(p.suggest ?? true)
  suggestOn.current = p.suggest ?? true
  const suggestCombo = useRef(p.suggestCombo ?? 'Mod+Shift+J')
  suggestCombo.current = p.suggestCombo ?? 'Mod+Shift+J'
  const suggestHost = useRef(p.suggestHost ?? false)
  suggestHost.current = p.suggestHost ?? false
  const hostCmds = useRef<string[]>([])
  const onTyped = useRef(p.onTyped)
  onTyped.current = p.onTyped
  onExit.current = p.onExit

  // typed once, never again: after a reconnect the shell is a new one and the line would run unasked
  const pendingInput = useRef(p.initialInput ?? '')
  useEffect(() => {
    if (state !== 'ready' || !pendingInput.current || !p.paneId) return
    const id = window.setTimeout(() => {
      const line = pendingInput.current
      pendingInput.current = ''
      if (line && p.paneId) writeToPane(p.paneId, line.replace(/[\r\n]+$/, '') + '\r')
    }, INITIAL_INPUT_DELAY)
    return () => window.clearTimeout(id)
  }, [state, p.paneId])

  const setSt = (s: State) => { stateRef.current = s; setState(s) }
  // how far the connection got (events from the backend) and where the line stood when it failed
  const [stage, setStage] = useState<'connecting' | 'authenticated'>('connecting')
  const retry = useRef<() => void>(() => {})
  const everReady = useRef(false)
  useEffect(() => { if (state === 'ready') everReady.current = true }, [state])
  // Where the connecting line is heading: slowly while the backend works, further once the login is done, and
  // to the very end when the session is ready (still smoothly: the screen goes when the line has arrived).
  const lineTarget = state === 'ready' ? 1 : stage === 'authenticated' ? 0.92 : 0.55
  const lineRate = state === 'ready' ? 7 : stage === 'authenticated' ? 0.9 : 0.35
  const [lineDone, setLineDone] = useState(true)
  useEffect(() => { if (state === 'connecting') setLineDone(false); else if (state === 'closed') setLineDone(true) }, [state])
  // the tab that holds this pane shows whether it is connecting or closed
  useEffect(() => {
    if (!p.paneId) return
    setPaneStatus(p.paneId, state)
    return () => setPaneStatus(p.paneId!, null)
  }, [state, p.paneId])

  // The clipboard goes through the native runtime where there is one (a webview often refuses page scripts).
  const copyText = (text: string) => { if (text) void be.system.writeClipboard(text).catch(() => {}) }
  const pasteClipboard = async () => {
    try {
      const text = await be.system.readClipboard()
      // the terminal turns it into bracketed paste when the shell asks for that, and the keys go out in order
      if (text && stateRef.current === 'ready' && await okToPaste(text)) term.current?.paste(text)
    } catch { /* clipboard not available */ }
  }

  useEffect(() => {
    const tm = new Terminal({
      fontSize: p.fontSize, fontFamily: p.fontFamily, cursorBlink: p.cursorBlink, cursorStyle: p.cursorStyle, cursorInactiveStyle: 'bar', cursorWidth: 2,
      scrollback: p.scrollback, allowProposedApi: true, theme: p.termTheme,
    })
    const f = new FitAddon()
    const s = new SearchAddon()
    tm.loadAddon(f)
    tm.loadAddon(s)
    tm.loadAddon(new WebLinksAddon())
    term.current = tm
    fit.current = f
    search.current = s
    tm.open(host.current!)
    // a paste made by the browser itself (Cmd+V) is asked about too, before xterm sees it
    const pasteEl = host.current!
    const onPasteCapture = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData('text/plain') ?? ''
      if (!guarded.current || !/[\r\n]/.test(text)) return
      e.preventDefault()
      e.stopPropagation()
      void okToPaste(text).then(ok => { if (ok && stateRef.current === 'ready') tm.paste(text) })
    }
    pasteEl.addEventListener('paste', onPasteCapture, true)
    imeColours(host.current, p.termTheme)
    const unfollowIme = followReplacements(tm as unknown as ComposingTerm)
    try { f.fit() } catch { /* hidden */ }

    // Command suggestions: the sources are the commands typed here, the snippets and (when asked) the host's history
    let snippets: Snippet[] = []
    const loadSnippets = () => { void be.snippets.list().then(l => { snippets = l }).catch(() => {}) }
    loadSnippets()
    window.addEventListener('focus', loadSnippets)
    let suggester: Suggester | null = null
    const popupFromMenu = () => { if (focusedRef.current) suggester?.togglePopup() }
    window.addEventListener(SUGGEST_POPUP, popupFromMenu)
    if (host.current?.parentElement) {
      suggester = createSuggester({
        term: tm as unknown as SuggestTerm, container: host.current.parentElement, send: d => sendKeys(d),
        sources: () => ({typed: getHistory(p.hostId), snippets, host: hostCmds.current}),
        enabled: () => suggestOn.current, ready: () => stateRef.current === 'ready', popupCombo: () => suggestCombo.current, onCommand: line => recordCommand(p.hostId, line),
      })
    }

    // Colouring of finished lines, a moment after output stops arriving
    const hl = createHighlighter(tm as unknown as HighlightTerm)
    highlighter.current = hl
    let hlTimer = 0
    const hlSub = (tm as unknown as {onWriteParsed?(cb: () => void): {dispose(): void}}).onWriteParsed?.(() => {
      window.clearTimeout(hlTimer)
      hlTimer = window.setTimeout(() => hl.scan(), 60)
      suggester?.refresh()
    })
    if (p.highlight === false) hl.dispose()

    let disposed = false
    const offs: Array<() => void> = []

    // Keystrokes leave one call at a time and in order: separate calls that are in flight together may be
    // run in any order by the backend (typing "echo" arrived as "ceho"). What is typed while a call is
    // running goes out together in the next one.
    let pendingKeys = ''
    let sending = false
    const sendKeys = (d: string) => {
      pendingKeys += d
      if (sending) return
      const flush = () => {
        if (!pendingKeys) { sending = false; return }
        const chunk = pendingKeys
        pendingKeys = ''
        sending = true
        be.session.write(sid.current, chunk).catch(() => {}).finally(flush)
      }
      flush()
    }

    // Never reconnect automatically: replaying commands unexpectedly is dangerous. Enter or the Retry button asks for it.
    retry.current = () => {
      setClosed(null)
      setStage('connecting')
      setSt('connecting')
      void be.session.reconnect(sid.current, tm.cols, tm.rows).catch(e => {
        setClosed({reason: errMsg(e), clean: false})
        setSt('closed')
      })
    }
    tm.onData(raw => {
      const d = composedInput(raw)
      if (stateRef.current === 'ready') {
        suggester?.feed(d)
        sendKeys(d)
        onTyped.current?.(d)
      } else if (stateRef.current === 'closed' && (d === '\r' || d === '\n')) {
        retry.current()
      }
    })
    const cwdSub = tm.parser.registerOscHandler(7, data => {
      if (p.paneId) setPaneCwd(p.paneId, parseCwd(data))
      return true
    })
    const unregSelection = p.paneId ? registerSelection(p.paneId, () => tm.getSelection()) : () => {}
    const unregister = p.paneId ? registerPane(p.paneId, text => {
      if (stateRef.current !== 'ready') return false
      sendKeys(text)
      suggester?.feed(text)
      return true
    }) : () => {}
    tm.onResize(({cols, rows}) => {
      if (sid.current && cols > 0 && rows > 0) void be.session.resize(sid.current, cols, rows).catch(() => {})
    })
    tm.attachCustomKeyEventHandler(e => {
      if (e.type !== 'keydown') return true
      if (suggester?.handleKey(e)) return false
      if (matches(e, searchCombo.current)) {
        setShowSearch(v => !v)
        return false
      }
      if (matches(e, copyCombo.current)) {
        const sel = tm.getSelection()
        if (!sel) return true // nothing selected: the key goes to the shell (Ctrl+C is the interrupt)
        copyText(sel)
        e.preventDefault()
        return false
      }
      if (matches(e, pasteCombo.current)) {
        e.preventDefault()
        void pasteClipboard()
        return false
      }
      if (appCombos.current.some(c => matches(e, c))) return false // not for the shell: the app's own handler gets it
      return true
    })

    const cursorSub = (tm as unknown as {onCursorMove?(cb: () => void): {dispose(): void}}).onCursorMove?.(() => suggester?.refresh())

    // copy on select: once the selection stops changing
    let selTimer = 0
    const selSub = (tm as unknown as {onSelectionChange?(cb: () => void): {dispose(): void}}).onSelectionChange?.(() => {
      window.clearTimeout(selTimer)
      selTimer = window.setTimeout(() => { if (copyOnSelect.current) copyText(tm.getSelection()) }, 150)
    })

    ;(async () => {
      try {
        const id = await be.session.open(p.hostId, tm.cols || 80, tm.rows || 24)
        if (disposed) { void be.session.close(id).catch(() => {}); return }
        sid.current = id
        if (p.paneId) setPaneSession(p.paneId, id)
        // Flow control: tell the backend how much xterm has parsed, so that it stops reading the connection while a lot is
        // waiting (a program that writes faster than the terminal can draw would otherwise freeze the window). The total
        // only grows, so reports that arrive late or twice do no harm; they are batched to one call per 128 KB or 20 ms.
        let parsed = 0, reported = 0, ackTimer = 0
        const report = () => {
          window.clearTimeout(ackTimer)
          ackTimer = 0
          if (disposed || parsed === reported) return
          reported = parsed
          void be.session.ack(id, parsed).catch(() => {})
        }
        offs.push(() => window.clearTimeout(ackTimer))
        offs.push(be.on(`term:data:${id}`, (b64: string) => {
          const bytes = b64ToBytes(b64)
          tm.write(bytes, () => {
            parsed += bytes.length
            if (parsed - reported >= ACK_EVERY) report()
            else if (!ackTimer) ackTimer = window.setTimeout(report, 20)
          })
          // output can only come from a live shell: do not keep the connecting screen up until the ready event
          if (stateRef.current === 'connecting') setSt('ready')
        }))
        offs.push(be.on(`term:ready:${id}`, () => { setPrompt(null); setAuth(null); setClosed(null); setSt('ready') }))
        offs.push(be.on(`term:hostkey:${id}`, (hk: HostKeyPrompt) => setPrompt(hk)))
        offs.push(be.on(`term:auth:${id}`, (a: AuthChallenge) => setAuth(a)))
        offs.push(be.on(`term:progress:${id}`, (e: {stage?: string}) => { if (e?.stage === 'authenticated') setStage('authenticated') }))
        offs.push(be.on(`term:closed:${id}`, (c: ClosedInfo) => {
          setPrompt(null)
          setAuth(null)
          setSt('closed')
          if (c.clean) onExit.current()
          else setClosed(c)
        }))
        await be.session.attach(id) // replays anything emitted before we subscribed
      } catch (e) {
        setClosed({reason: errMsg(e), clean: false})
        setSt('closed')
      }
    })()

    const ro = new ResizeObserver(() => { try { f.fit() } catch { /* hidden */ } })
    ro.observe(host.current!)

    return () => {
      disposed = true
      pasteEl.removeEventListener('paste', onPasteCapture, true)
      window.clearTimeout(hlTimer)
      window.clearTimeout(selTimer)
      hlSub?.dispose()
      cursorSub?.dispose()
      suggester?.dispose()
      window.removeEventListener('focus', loadSnippets)
      window.removeEventListener(SUGGEST_POPUP, popupFromMenu)
      selSub?.dispose()
      unfollowIme()
      hl.dispose()
      unregister()
      unregSelection()
      cwdSub.dispose()
      if (p.paneId) { setPaneSession(p.paneId, null); setPaneCwd(p.paneId, null) }
      ro.disconnect()
      offs.forEach(o => o())
      if (sid.current) void be.session.close(sid.current).catch(() => {})
      tm.dispose()
    }
    // the session lives as long as the pane; appearance changes are applied below
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [be, p.hostId])

  useEffect(() => {
    const tm = term.current
    if (!tm) return
    tm.options.fontSize = p.fontSize
    tm.options.fontFamily = p.fontFamily
    tm.options.theme = p.termTheme
    imeColours(host.current, p.termTheme)
    tm.options.cursorStyle = p.cursorStyle
    tm.options.cursorBlink = p.cursorBlink
    tm.options.scrollback = p.scrollback
    try { fit.current?.fit() } catch { /* hidden */ }
  }, [p.fontSize, p.fontFamily, p.termTheme, p.cursorStyle, p.cursorBlink, p.scrollback])

  // the shell history is read once the session is ready: this computer's for the local terminal, a server's when asked for
  useEffect(() => {
    if (state !== 'ready' || !sid.current || p.suggest === false) { hostCmds.current = []; return }
    let gone = false
    void be.session.hostHistory(sid.current).then(l => { if (!gone) hostCmds.current = l }).catch(() => { if (!gone) hostCmds.current = [] }) // refused: an SSH host while that setting is off
    return () => { gone = true }
  }, [be, state, p.suggest, p.suggestHost])

  // the Highlight output switch in Settings
  useEffect(() => {
    const hl = highlighter.current
    if (!hl) return
    if (p.highlight === false) hl.dispose()
    else { hl.reset(); hl.scan() }
  }, [p.highlight])

  // WebGL only while this pane has the focus (a page has about 16 contexts); a lost context is not asked for again
  useEffect(() => {
    const tm = term.current
    if (!tm) return
    if (gpu.current && gpu.current.term !== tm) gpu.current = null // the terminal was made again: its addon went with it
    const want = p.gpu !== false && p.visible && p.focused && !gpuFailed.current
    if (want && !gpu.current) {
      const g = attachGpu(tm, () => { gpuFailed.current = true; gpu.current = null })
      if (g) gpu.current = {term: tm, gpu: g}
      else gpuFailed.current = true
    } else if (!want && gpu.current) {
      gpu.current.gpu.dispose()
      gpu.current = null
    }
  }, [p.gpu, p.visible, p.focused, state])

  useEffect(() => {
    if (p.visible) {
      try { fit.current?.fit() } catch { /* hidden */ }
      if (p.focused) term.current?.focus()
    }
  }, [p.visible, p.focused])

  const onContextMenu = (e: React.MouseEvent) => {
    e.preventDefault() // never the browser's own menu
    const mode = rightClick.current
    if (mode === 'paste') void pasteClipboard()
    else if (mode === 'menu') setMenu({x: e.clientX, y: e.clientY})
  }
  // the right-click menu goes away on any click, Esc, or when the pane loses its place
  useEffect(() => {
    if (!menu) return
    const off = () => setMenu(null)
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); off() } }
    window.addEventListener('mousedown', off)
    window.addEventListener('keydown', esc, true)
    window.addEventListener('blur', off)
    return () => { window.removeEventListener('mousedown', off); window.removeEventListener('keydown', esc, true); window.removeEventListener('blur', off) }
  }, [menu])
  const hasSelection = !!menu && !!term.current?.hasSelection()

  const trust = (ok: boolean) => {
    if (!prompt) return
    void be.session.trustHostKey(sid.current, ok ? prompt.fingerprint : '').catch(e => setClosed({reason: errMsg(e), clean: false}))
    setPrompt(null)
  }

  const authenticity = t("The authenticity of {host} can't be established.", {host: '\u0001'}).split('\u0001')
  return (
    <div className={'pane' + (p.focused ? ' focused' : '') + (p.prod ? ' prod' : '')} onMouseDown={p.onFocus} onContextMenu={onContextMenu} data-testid="pane" style={{background: p.termTheme.background}}>
      <div ref={host} style={{height: '100%', background: p.termTheme.background}} />
      {p.prod && <div className="prod-badge" title={t('This host is marked production')} aria-label={t('Production host')}>PROD</div>}
      {menu && (
        <div className="ctx-menu" role="menu" aria-label={t('Terminal')} style={{left: menu.x, top: menu.y}} onMouseDown={e => e.stopPropagation()} onContextMenu={e => e.preventDefault()}>
          <button role="menuitem" disabled={!hasSelection} onClick={() => { copyText(term.current?.getSelection() ?? ''); setMenu(null); term.current?.focus() }}>{t('Copy')}</button>
          <button role="menuitem" onClick={() => { void pasteClipboard(); setMenu(null); term.current?.focus() }}>{t('Paste')}</button>
          <button role="menuitem" onClick={() => { term.current?.selectAll(); setMenu(null) }}>{t('Select all')}</button>
          <button role="menuitem" onClick={() => { term.current?.clear(); setMenu(null); term.current?.focus() }}>{t('Clear')}</button>
        </div>
      )}
      {showSearch && (
        <div className="searchbar">
          <input autoFocus aria-label={t('Search terminal')} value={needle} placeholder={t('Find…')}
            onChange={e => { setNeedle(e.target.value); search.current?.findNext(e.target.value, {incremental: true}) }}
            onKeyDown={e => {
              if (e.key === 'Enter') e.shiftKey ? search.current?.findPrevious(needle) : search.current?.findNext(needle)
              if (e.key === 'Escape') { setShowSearch(false); term.current?.focus() }
            }} />
          <button className="icon" aria-label={t('Previous match')} onClick={() => search.current?.findPrevious(needle)}><IconArrowUp size={15} /></button>
          <button className="icon" aria-label={t('Next match')} onClick={() => search.current?.findNext(needle)}><IconArrowDown size={15} /></button>
        </div>
      )}
      {p.connect && !prompt && (state === 'connecting' || (state === 'ready' && !lineDone) || (state === 'closed' && closed && !everReady.current)) ? (
        <div className="connect-over" role={state === 'closed' ? undefined : 'status'} aria-label={state === 'closed' ? t('Connection failed') : t('Connecting')} style={{background: p.termTheme.background}}
          // a click on the screen must not take the keyboard away from the terminal under it
          onMouseDown={e => { if (!(e.target as HTMLElement).closest('button')) e.preventDefault() }}>
          <ConnectScreen label={p.connect.label} os={p.connect.os} subtitle={p.connect.subtitle} step={state === 'closed' ? 'failed' : 'connecting'}
            target={lineTarget} rate={lineRate} onFull={() => setLineDone(true)}
            headerAction={p.onShowLogs ? <button onClick={p.onShowLogs}>{t('Show logs')}</button> : undefined}
            footer={state === 'closed'
              ? <>{p.onCloseTab ? <button onClick={p.onCloseTab}>{t('Close')}</button> : <span />}<button className="primary" onClick={() => retry.current()}>{t('Retry')}</button></>
              : p.onCloseTab ? <button onClick={p.onCloseTab}>{t('Close')}</button> : undefined}>
            {state === 'closed' && closed
              ? <div className="connect-error" role="alert">{closed.reason}<small>{t('Press Enter to try again')}</small></div>
              : <div className="connect-msg">{stage === 'authenticated' ? t('Opening the shell…') : t('Connecting…')}</div>}
          </ConnectScreen>
        </div>
      ) : (!p.connect && state === 'connecting' && !prompt ? <div className="banner info">{t('Connecting…')}</div> : null)}
      {state === 'closed' && closed && !(!everReady.current && p.connect) && (
        <div className="banner" role="alert">
          <span>{closed.reason}</span>
          <b>{t('Press Enter to reconnect')}</b>
        </div>
      )}
      <AuthPrompt challenge={auth} promptKey={sid.current} onDone={() => setAuth(null)} onError={m => { setClosed({reason: m, clean: false}); setSt('closed') }} />
      {prompt && (
        <div className="overlay" role="alertdialog" aria-label={t('Host key confirmation')}>
          <div className="card">
            <h3 style={{marginTop: 0}}>{t('Unknown host key')}</h3>
            <div>{authenticity[0]}<b>{prompt.host}</b>{authenticity[1]}</div>
            <div className="muted">{t('{type} fingerprint', {type: prompt.key_type})}</div>
            <div className="fp" data-testid="fingerprint">{prompt.fingerprint}</div>
            <div className="row">
              <button className="primary" onClick={() => trust(true)}>{t('Trust and connect')}</button>
              <button onClick={() => trust(false)}>{t('Cancel')}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

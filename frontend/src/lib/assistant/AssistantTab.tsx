import {useCallback, useEffect, useRef, useState} from 'react'
import {baseHostId} from '../hostId'
import {useBackend} from '../backend'
import {useToast} from '../ui/Toast'
import {Select} from '../ui/Select'
import {ModelPicker} from './ModelPicker'
import {errMsg} from '../util'
import {useT, type TFn} from '../i18n'
import {rich} from './rich'
import {selectionOfPane} from '../terminal/panes'
import {CommandCard} from './CommandCard'
import {Composer, FileChip} from './Composer'
import {MessageText} from './MessageText'
import {readAttachment, toAttachment, type Attach} from './attachments'
import {PreviewPanel} from './PreviewPanel'
import {usePaneSession} from './useSession'
import type {AiCommand, AiModelList, AiPolicy, AiPreview, AiProvider, AiReply, AiSessionState, AiStep} from '../types'

interface ConfirmReq {call: string; server: string; tool: string; args: string; answer?: 'allowed' | 'declined'}

type Msg =
  | {kind: 'user'; text: string; selection?: boolean; files?: Attach[]}
  /** a question the assistant needs the user to answer in the chat: share the screen (a host set to "ask"), or send pictures as they are */
  | {kind: 'confirm'; rid: string; what: 'share' | 'images'; question: string; selection: string; files: Attach[]; preview?: AiPreview}
  | {kind: 'assistant'; rid: string; text: string; streaming: boolean; steps: AiStep[]; confirms: ConfirmReq[]; commands: AiCommand[]; meta?: string; error?: {message: string; code: string}}

/** Fired by the "next model" shortcut; the open assistant moves to the next model of its provider. */
export const AI_NEXT_MODEL = 'vutty:ai-next-model'

const store = {
  get: (k: string) => { try { return localStorage.getItem(k) ?? '' } catch { return '' } },
  set: (k: string, v: string) => { try { localStorage.setItem(k, v) } catch { /* not remembered */ } },
}
/** The agent answers in a JSON envelope; while it is still arriving it is not shown as text. */
const looksLikeJSON = (s: string) => /^[\s`]*(\{|json)/.test(s)
const requestId = () => 'req-' + Math.random().toString(36).slice(2, 10) + Date.now().toString(36)

/**
 * The AI assistant of a terminal, in the sidebar. It asks a command-line AI the user is signed in to about
 * the text on the screen. Vutty redacts secrets on the way out and in (shown as *******); the assistant
 * only proposes commands, and the user decides.
 */
export function AssistantTab({paneId, onOpenSettings}: {paneId: string; onOpenSettings(): void}) {
  const t = useT()
  const be = useBackend()
  const toast = useToast()
  const sid = usePaneSession(paneId)
  const [providers, setProviders] = useState<AiProvider[]>([])
  const [provider, setProvider] = useState('')
  const [models, setModels] = useState<AiModelList | null>(null)
  const [model, setModel] = useState('')
  const [info, setInfo] = useState<AiSessionState | null>(null)
  const [preview, setPreview] = useState<AiPreview | null>(null)
  const [showPreview, setShowPreview] = useState(false)
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [input, setInput] = useState('')
  const [files, setFiles] = useState<Attach[]>([])
  const [dragging, setDragging] = useState(false)
  const dragDepth = useRef(0)
  const [runId, setRunId] = useState('')
  const [loadErr, setLoadErr] = useState('')
  const listEnd = useRef<HTMLDivElement>(null)
  const offs = useRef<Array<() => void>>([])

  const enabled = providers.filter(p => p.enabled && p.status.state === 'ready')
  const current = providers.find(p => p.id === provider)

  const loadProviders = useCallback(async () => {
    try {
      const list = await be.assistant.providers()
      setProviders(list)
      setProvider(cur => {
        const ready = list.filter(p => p.enabled && p.status.state === 'ready')
        if (ready.some(p => p.id === cur)) return cur
        const remembered = store.get('vutty.ai.provider')
        return (ready.find(p => p.id === remembered) ?? ready.find(p => p.default) ?? ready[0])?.id ?? ''
      })
      setLoadErr('')
    } catch (e) { setLoadErr(errMsg(e)) }
  }, [be])

  const loadInfo = useCallback(async () => {
    if (!sid) { setInfo(null); setPreview(null); return }
    try {
      setInfo(await be.assistant.sessionInfo(sid))
      setPreview(await be.assistant.preview(sid, 0, ''))
    } catch { /* the session is gone */ }
  }, [be, sid])

  useEffect(() => { void loadProviders() }, [loadProviders])
  // the tools are looked for in the background: ask again until every state is known
  const unknown = providers.some(p => p.status.state === '')
  useEffect(() => {
    if (!unknown) return
    const timer = window.setTimeout(() => { void loadProviders() }, 600)
    return () => window.clearTimeout(timer)
  }, [unknown, providers, loadProviders])
  useEffect(() => { void loadInfo() }, [loadInfo])
  // Settings may change providers from its own window
  useEffect(() => be.on('bridge:changed', (s: string) => { if (s === 'AssistantService') void loadProviders() }), [be, loadProviders])
  useEffect(() => { setMsgs([]) }, [sid])
  useEffect(() => () => { offs.current.forEach(o => o()) }, [])
  useEffect(() => { listEnd.current?.scrollIntoView?.({block: 'end'}) }, [msgs])

  // The models of the chosen provider come from the provider itself. They load in the background (starting the CLI can
  // take a moment), once per provider, and a failure can be retried: the list is for choosing, not for typing.
  const providersRef = useRef(providers)
  providersRef.current = providers
  const [modelsBusy, setModelsBusy] = useState(false)
  const loadModels = useCallback(async (refresh: boolean) => {
    if (!provider) { setModels(null); return }
    setModelsBusy(true)
    try {
      const l = await be.assistant.models(provider, refresh)
      setModels(l)
      const want = store.get('vutty.ai.model.' + provider) || providersRef.current.find(p => p.id === provider)?.model || ''
      setModel(l.models.some(m => m.id === want) ? want : '')
    } catch (e) {
      setModels({models: [{id: '', name: t('Default (CLI decides)')}], error: errMsg(e), can_list: true})
    } finally { setModelsBusy(false) }
  }, [be, provider, t])
  useEffect(() => { void loadModels(false) }, [loadModels])

  const policy = info?.policy ?? 'ask'
  const sharing = policy === 'on' || (policy === 'ask' && !!info?.confirmed)
  const realHost = !!info && info.host_id !== 'local' && !info.host_id.startsWith('serial:')
  const hostId = baseHostId(info?.host_id ?? '')
  const busy = !!runId

  const patchMsg = (rid: string, fn: (m: Extract<Msg, {kind: 'assistant'}>) => Partial<Extract<Msg, {kind: 'assistant'}>>) =>
    setMsgs(ms => ms.map(m => (m.kind === 'assistant' && m.rid === rid ? {...m, ...fn(m)} : m)))

  const finish = () => { offs.current.forEach(o => o()); offs.current = []; setRunId('') }

  const QUICK = [t('Explain the last error'), t('What should I check first?'), t('Summarize this output'), t('Is anything wrong here?')]

  const send = async (question: string, selection: string, attached: Attach[] = files, allowImages = false, again = false) => {
    if (!sid || (!question.trim() && attached.length === 0) || busy) return
    question = question.trim() || (attached.some(a => a.kind === 'image') ? t('What is in this?') : t('Look at the attached file.'))
    const rid = requestId()
    setMsgs(ms => [...ms, ...(again ? [] : [{kind: 'user' as const, text: question, selection: !!selection, files: attached}]), {kind: 'assistant', rid, text: '', streaming: true, steps: [], confirms: [], commands: []}])
    setInput('')
    setFiles([])
    setRunId(rid)
    // listen first: the answer can start before ask() returns
    offs.current = [
      be.on(`assistant:chunk:${rid}`, (d: {text: string}) => { if (!looksLikeJSON(d.text)) patchMsg(rid, () => ({text: d.text})) }),
      be.on(`assistant:step:${rid}`, (d: AiStep) => patchMsg(rid, m => ({steps: [...m.steps, d]}))),
      be.on(`assistant:confirm:${rid}`, (d: ConfirmReq) => patchMsg(rid, m => ({confirms: [...m.confirms, d]}))),
      be.on(`assistant:done:${rid}`, (d: AiReply) => {
        patchMsg(rid, () => ({text: d.text, commands: d.commands ?? [], streaming: false,
          meta: [providers.find(p => p.id === d.provider)?.name ?? d.provider, d.model, d.redacted ? t('{n} redacted', {n: d.redacted}) : ''].filter(Boolean).join(' · ')}))
        finish()
        void loadInfo()
      }),
      be.on(`assistant:error:${rid}`, (d: {message: string; code: string}) => {
        patchMsg(rid, () => ({streaming: false, error: d}))
        finish()
      }),
    ]
    try {
      await be.assistant.ask(sid, question, {provider, model, lines: 0, selection, request_id: rid, attachments: attached.map(toAttachment), allow_images: allowImages})
    } catch (e) {
      finish()
      const asCard = (what: 'share' | 'images', preview?: AiPreview) => setMsgs(ms => ms.map(m => (m.kind === 'assistant' && m.rid === rid ? {kind: 'confirm' as const, rid, what, question, selection, files: attached, preview} : m)))
      if (errMsg(e) === 'confirm_share') {
        let preview: AiPreview | undefined
        try { preview = await be.assistant.preview(sid, 0, selection, attached.map(toAttachment)) } catch { /* the card says so */ }
        asCard('share', preview)
      } else if (errMsg(e) === 'confirm_images') asCard('images')
      else {
        // the question did not start: take it back so it can be corrected
        setMsgs(ms => ms.filter(m => !(m.kind === 'assistant' && m.rid === rid)).slice(0, again ? undefined : -1))
        setInput(question); setFiles(attached)
        toast.error(errMsg(e))
      }
    }
  }

  /** the user answered a confirmation card in the chat */
  const answerCard = async (card: Extract<Msg, {kind: 'confirm'}>, yes: boolean) => {
    setMsgs(ms => ms.filter(m => !(m.kind === 'confirm' && m.rid === card.rid)))
    if (!yes) {
      // withdraw the question: it goes back to the box with its files
      setMsgs(ms => { const i = ms.map(m => m.kind).lastIndexOf('user'); return i >= 0 ? ms.filter((_, k) => k !== i) : ms })
      setInput(card.question); setFiles(card.files)
      return
    }
    try {
      if (card.what === 'share') await be.assistant.confirmShare(sid)
      await send(card.question, card.selection, card.files, card.what === 'images', true)
      void loadInfo()
    } catch (e) { toast.error(errMsg(e)) }
  }

  const cancel = () => { if (runId) void be.assistant.cancel(runId).catch(() => {}) }

  const answerTool = (rid: string, call: string, allow: boolean) => {
    void be.assistant.confirmTool(rid, call, allow).catch(() => {})
    patchMsg(rid, m => ({confirms: m.confirms.map(c => (c.call === call ? {...c, answer: allow ? 'allowed' : 'declined'} : c))}))
  }

  const askSelection = () => {
    const sel = selectionOfPane(paneId)
    if (!sel.trim()) { toast.error(t('Select some text in the terminal first')); return }
    void send(input.trim() || t('Explain this'), sel)
  }
  const addFiles = async (list: File[]) => {
    let taken = files
    for (const f of list) {
      try {
        const r = await readAttachment(f, taken, !!current?.supports_images)
        if (r.ok) { taken = [...taken, r.attach]; setFiles(taken) } else toast.error(r.error)
      } catch (e) { toast.error(errMsg(e)) }
    }
  }
  // dragging a file over the panel shows where to drop it
  const isFileDrag = (e: React.DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes('Files')
  const dragProps = {
    onDragEnter: (e: React.DragEvent) => { if (isFileDrag(e)) { e.preventDefault(); dragDepth.current++; setDragging(true) } },
    onDragOver: (e: React.DragEvent) => { if (isFileDrag(e)) e.preventDefault() },
    onDragLeave: (e: React.DragEvent) => { if (isFileDrag(e) && --dragDepth.current <= 0) { dragDepth.current = 0; setDragging(false) } },
    onDrop: (e: React.DragEvent) => {
      if (!isFileDrag(e)) return
      e.preventDefault(); dragDepth.current = 0; setDragging(false)
      void addFiles(Array.from(e.dataTransfer.files))
    },
  }

  const pickModel = (v: string) => {
    setModel(v)
    store.set('vutty.ai.model.' + provider, v)
  }
  const modelsRef = useRef({models, model, busy})
  modelsRef.current = {models, model, busy}
  useEffect(() => {
    const next = () => {
      const {models: l, model: cur, busy: b} = modelsRef.current
      if (b || !l || l.models.length < 2) return
      const i = l.models.findIndex(m => m.id === cur)
      const m = l.models[(i + 1) % l.models.length]
      pickModel(m.id)
      toast.info(t('Model: {name}', {name: m.name}))
    }
    window.addEventListener(AI_NEXT_MODEL, next)
    return () => window.removeEventListener(AI_NEXT_MODEL, next)
  })
  const setPolicy = async (v: string) => {
    try { await be.assistant.setHostPolicy(hostId, v as AiPolicy); await loadInfo() } catch (e) { toast.error(errMsg(e)) }
  }
  const newChat = () => { if (sid) void be.assistant.resetConversation(sid).catch(() => {}); setMsgs([]) }
  const openPreview = async () => {
    if (!sid) return
    try { setPreview(await be.assistant.preview(sid, 0, '', files.map(toAttachment))); setShowPreview(v => !v) } catch (e) { toast.error(errMsg(e)) }
  }

  if (loadErr) return <div className="ai-empty"><div className="error" role="alert">{loadErr}</div><button onClick={() => void loadProviders()}>{t('Retry')}</button></div>
  if (providers.length > 0 && enabled.length === 0 && providers.some(p => p.status.state === '')) {
    return <div className="ai-empty muted" data-testid="ai-checking" role="status">{t('Looking for AI tools on this computer…')}</div>
  }
  if (providers.length > 0 && enabled.length === 0) {
    const waiting = providers.filter(p => p.status.state === 'needs_signin')
    return (
      <div className="ai-empty" data-testid="ai-setup">
        <b>{t('AI assistant')}</b>
        <p className="muted">{t('Ask an AI about what is on this terminal. Vutty uses the AI command-line tool you already have, so no key is needed.')}</p>
        {waiting.length > 0 && <p className="muted">{t('Not signed in: {list}.', {list: waiting.map(p => p.status.hint ? `${p.name} ${t('(run {hint})', {hint: p.status.hint})}` : p.name).join(', ')})}</p>}
        <button className="primary" onClick={onOpenSettings}>{t('Enable a provider in Settings')}</button>
      </div>
    )
  }
  if (providers.length === 0) return <div className="ai-empty muted" data-testid="ai-setup">{t('No AI tools were found on this computer. Install Claude Code, Codex CLI or Gemini CLI, or add your own command in Settings.')}<button onClick={onOpenSettings}>{t('Open Settings')}</button></div>

  const policyOptions = [{value: '', label: info?.production ? t('Default (off)') : t('Default (on)')}, {value: 'off', label: t('Off')}, {value: 'ask', label: t('Ask first, in the chat')}, {value: 'on', label: t('On')}]

  return (
    <div className={'ai-panel' + (dragging ? ' dropping' : '')} data-testid="ai-panel" {...dragProps}>
      {dragging && <div className="ai-drop" role="status">{t('Drop files or screenshots to add them to your question')}</div>}
      <div className="ai-head">
        <div className="ai-share">
          <span className={'ai-badge' + (sharing ? ' on' : '')} data-testid="ai-badge" title={t('The terminal text that goes to the AI is redacted first')}>
            {policy === 'off' ? t('Not sharing') : sharing ? t('Sharing · {n} redacted', {n: preview?.redacted ?? 0}) : t('Asks before sharing')}
          </span>
          <button className="link" onClick={() => void openPreview()} disabled={!sid}>{t('What will be sent')}</button>
          <span className="spacer" />
          <button className="link" onClick={newChat} disabled={msgs.length === 0}>{t('New chat')}</button>
        </div>
        {realHost && (
          <div className="ai-policy">
            <span className="muted">{t('This host')}</span>
            <Select className="inline" label={t('Assistant on this host')} value={info?.stored ?? ''} onChange={v => void setPolicy(v)} options={policyOptions} />
          </div>
        )}
      </div>

      {policy === 'off' ? (
        <div className="ai-empty" data-testid="ai-off">{t('The assistant is turned off for {name}.', {name: info?.label || t('this host')})}{realHost && <button onClick={() => void setPolicy('ask')}>{t('Turn on (ask first)')}</button>}</div>
      ) : (
        <>
          <div className="ai-msgs" aria-live="polite">
            {msgs.length === 0 && (
              <div className="ai-intro">
                <b>{t('Ask about this terminal')}</b>
                <div className="muted">{t('I read the screen (secrets hidden), explain it and suggest commands. You decide what runs. You can also drop a log or a screenshot here.')}</div>
                <div className="ai-quick" role="group" aria-label={t('Suggestions')}>
                  {QUICK.map(q => <button key={q} onClick={() => void send(q, '')} disabled={!sid || busy}>{q}</button>)}
                </div>
              </div>
            )}
            {showPreview && preview && <PreviewPanel preview={preview} provider={current?.name ?? ''} actions={<button onClick={() => setShowPreview(false)}>{t('Hide')}</button>} />}
            {msgs.map((m, i) => m.kind === 'confirm' ? (
              <div key={'c' + m.rid} className="ai-msg assistant ai-ask" role="group" aria-label={m.what === 'share' ? t('Share this terminal?') : t('Send pictures as they are?')}>
                {m.what === 'share' ? (
                  m.preview
                    ? <PreviewPanel preview={m.preview} provider={current?.name ?? ''} title={t('Share this terminal with the assistant?')} actions={<><button className="primary" onClick={() => void answerCard(m, true)}>{t('Share and ask')}</button><button onClick={() => void answerCard(m, false)}>{t('Cancel')}</button></>} />
                    : <><b>{t('Share this terminal with the assistant?')}</b><div className="ai-cmd-actions"><button className="primary" onClick={() => void answerCard(m, true)}>{t('Share and ask')}</button><button onClick={() => void answerCard(m, false)}>{t('Cancel')}</button></div></>
                ) : (
                  <>
                    <b>{t('Send pictures as they are?')}</b>
                    <div className="ai-chips">{m.files.filter(f => f.kind === 'image').map(f => <FileChip key={f.id} file={f} />)}</div>
                    <p><b>{t('Vutty hides secrets in text, but it cannot find a secret inside a picture.')}</b> {m.files.filter(f => f.kind === 'image').length === 1 ? t('Anything you can see in it goes to {name} as it is.', {name: current?.name ?? ''}) : t('Anything you can see in them goes to {name} as it is.', {name: current?.name ?? ''})} {t('The terminal text and text files are still redacted. Check that no password, token or key is visible.')}</p>
                    <div className="ai-cmd-actions"><button className="primary" onClick={() => void answerCard(m, true)}>{t('Send pictures')}</button><button onClick={() => void answerCard(m, false)}>{t('Cancel')}</button></div>
                  </>
                )}
              </div>
            ) : m.kind === 'user' ? (
              <div key={i} className="ai-msg user">
                {m.files && m.files.length > 0 && <div className="ai-chips">{m.files.map(f => <FileChip key={f.id} file={f} />)}</div>}
                {m.text}{m.selection && <span className="badge">{t('selection')}</span>}
              </div>
            ) : (
              <div key={m.rid} className="ai-msg assistant" data-testid="ai-answer">
                {m.steps.length > 0 && <ul className="ai-steps">{m.steps.map((s, k) => <li key={k} className={s.state}>{s.tool}: {s.state}{s.note ? ` (${s.note})` : ''}</li>)}</ul>}
                {m.text && <MessageText text={m.text} />}
                {m.streaming && !m.text && <div className="muted">{t('Thinking…')}</div>}
                {m.confirms.map(c => (
                  <div key={c.call} className="ai-confirm" role="group" aria-label={t('Allow {tool}?', {tool: c.tool})}>
                    <div>{rich(t('{server} wants to run {tool}'), {server: <b>{c.server}</b>, tool: <code>{c.tool}</code>})}</div>
                    <code className="ai-args">{c.args}</code>
                    {c.answer ? <div className="muted">{c.answer === 'allowed' ? t('Allowed') : t('Declined')}</div> : (
                      <div className="ai-cmd-actions"><button className="primary" onClick={() => answerTool(m.rid, c.call, true)}>{t('Allow')}</button><button onClick={() => answerTool(m.rid, c.call, false)}>{t('Decline')}</button></div>
                    )}
                  </div>
                ))}
                {m.commands.map((c, k) => <CommandCard key={k} sid={sid} cmd={c} autoKey={`${m.rid}:${k}`} />)}
                {m.error && (
                  <div className="ai-error" role="alert">
                    {m.error.message}
                    {m.error.code === 'needs_signin' && <button onClick={onOpenSettings}>{t('Open Settings')}</button>}
                  </div>
                )}
                {m.meta && <div className="ai-meta muted">{m.meta}</div>}
              </div>
            ))}
            <div ref={listEnd} />
          </div>
          <Composer value={input} onChange={setInput} files={files} onRemoveFile={id => setFiles(fs => fs.filter(f => f.id !== id))} onAddFiles={list => void addFiles(list)}
            onSend={() => void send(input, '')} onUseSelection={askSelection} onStop={cancel} busy={busy} ready={!!sid} images={!!current?.supports_images}
            leading={<ModelPicker providers={enabled} provider={provider} onProvider={v => { setProvider(v); store.set('vutty.ai.provider', v); setMsgs([]) }} models={models} model={model}
              onModel={pickModel} loading={modelsBusy} onRetry={() => void loadModels(true)} disabled={busy} />} />
        </>
      )}

    </div>
  )
}

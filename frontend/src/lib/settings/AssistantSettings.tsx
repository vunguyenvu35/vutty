import {useCallback, useEffect, useRef, useState} from 'react'
import {IconRefresh} from '../ui/Icons'
import {useBackend} from '../backend'
import {useToast} from '../ui/Toast'
import {Switch} from '../ui/Switch'
import {Select} from '../ui/Select'
import {errMsg, splitArgs} from '../util'
import {McpServersPanel} from './McpServers'
import {useT, type TFn} from '../i18n'
import {rich} from '../assistant/rich'
import type {AiModelList, AiProvider, AiSettings, Host} from '../types'

const company = (t: TFn): Record<string, string> => ({claude: 'Anthropic', codex: 'OpenAI', gemini: 'Google', antigravity: t('Google (and the model provider you chose in Antigravity)')})

const stateLabel = (t: TFn): Record<string, string> => ({ready: t('Ready'), needs_signin: t('Needs sign-in'), unsupported: t('Unsupported version'), not_installed: t('Not installed'), '': t('Checking…')})

/**
 * Settings → AI assistant. Providers are found on this computer, never assumed: each is switched on by the
 * user and must pass an isolation self-test (it is asked to use a tool and must be unable to). Models are
 * read from the provider itself. Redaction cannot be turned off here, only made stricter.
 */
export function AssistantSettings() {
  const t = useT()
  const be = useBackend()
  const toast = useToast()
  const [providers, setProviders] = useState<AiProvider[] | null>(null)
  const [hosts, setHosts] = useState<Host[]>([])
  const [models, setModels] = useState<Record<string, AiModelList>>({})
  const [settings, setSettings] = useState<AiSettings | null>(null)
  const [patterns, setPatterns] = useState('')
  const [lines, setLines] = useState('200')
  const [busy, setBusy] = useState('')
  const [err, setErr] = useState<Record<string, string>>({})
  const [customOpen, setCustomOpen] = useState(false)
  const loading = useRef(new Set<string>())

  const load = useCallback(async () => {
    try {
      const [ps, s] = await Promise.all([be.assistant.providers(), be.assistant.settings()])
      setProviders(ps); setSettings(s); setPatterns(s.patterns.join('\n')); setLines(String(s.lines))
    } catch (e) { toast.error(errMsg(e)) }
    // the hosts a server can be limited to: loaded here, with the rest, so every part of the page shows the same list
    try { setHosts(await be.hosts.listHosts({})) } catch { /* the page still works: a server is then offered on every host */ }
  }, [be, toast])
  useEffect(() => { void load() }, [load])
  // the tools are looked for in the background (starting each CLI takes a moment): ask again until every state is known
  const checking = !!providers?.some(p => p.status.state === '')
  useEffect(() => {
    if (!checking) return
    // only the providers are asked again: the rest of the page (and what is being typed in it) stays as it is
    const timer = window.setTimeout(() => { void be.assistant.providers().then(setProviders).catch(() => {}) }, 600)
    return () => window.clearTimeout(timer)
  }, [be, checking, providers])
  // Settings is its own window: what changed in the main window (hosts, providers) is read again when it is focused
  useEffect(() => {
    const again = () => { void load() }
    window.addEventListener('focus', again)
    const off = be.on('bridge:changed', again)
    return () => { window.removeEventListener('focus', again); off() }
  }, [be, load])

  // the model list of an enabled provider comes from the provider itself
  useEffect(() => {
    for (const p of providers ?? []) {
      if (p.enabled && !models[p.id] && !loading.current.has(p.id)) {
        loading.current.add(p.id)
        void be.assistant.models(p.id, false).then(l => setModels(m => ({...m, [p.id]: l})))
          .catch(e => setModels(m => ({...m, [p.id]: {models: [{id: '', name: t('Default (CLI decides)')}], error: errMsg(e), can_list: true}})))
          .finally(() => loading.current.delete(p.id))
      }
    }
  }, [be, providers, models, t])

  const reloadModels = async (id: string) => {
    setModels(m => { const n = {...m}; delete n[id]; return n })
    try { const l = await be.assistant.models(id, true); setModels(m => ({...m, [id]: l})) } catch (e) { setModels(m => ({...m, [id]: {models: [{id: '', name: t('Default (CLI decides)')}], error: errMsg(e), can_list: true}})) }
  }
  const refresh = async () => {
    setBusy('refresh')
    try { setProviders(await be.assistant.refreshProviders()); setModels({}) } catch (e) { toast.error(errMsg(e)) } finally { setBusy('') }
  }
  const toggle = async (p: AiProvider, on: boolean) => {
    setBusy(p.id); setErr(x => ({...x, [p.id]: ''}))
    try { await be.assistant.setProviderEnabled(p.id, on); await load() } catch (e) { setErr(x => ({...x, [p.id]: errMsg(e)})) } finally { setBusy('') }
  }
  const setModel = async (p: AiProvider, model: string) => {
    try { await be.assistant.setProviderModel(p.id, model); await load(); setModels(m => { const n = {...m}; delete n[p.id]; return n }) } catch (e) { toast.error(errMsg(e)) }
  }
  const saveSettings = async (patch: Partial<AiSettings>) => {
    if (!settings) return
    try { const s = await be.assistant.saveSettings({...settings, ...patch}); setSettings(s); setPatterns(s.patterns.join('\n')); setLines(String(s.lines)) } catch (e) { toast.error(errMsg(e)) }
  }

  // the page is drawn at once; what is still being read shows as such
  const list = providers ?? []
  return (
    <>
      <section className="card-section" aria-label={t('AI providers')}>
        <h4>{t('Providers')}</h4>
        <div className="muted">{t("Vutty runs the official AI command-line tool you installed and signed in to; no key is stored here, and the use of each tool is governed by its provider's terms. Vutty is not affiliated with Anthropic, OpenAI or Google. A provider stays off until you switch it on.")}</div>
        {!providers && <div className="muted" role="status">{t('Looking for AI tools on this computer…')}</div>}
        {providers && providers.length === 0 && <div className="muted">{t('No AI tools were found. Install Claude Code, Codex CLI or Gemini CLI, or add your own command below.')}</div>}
        {list.map(p => {
          const ready = p.status.state === 'ready'
          const ml = models[p.id]
          return (
            <div key={p.id} className="ai-provider" data-testid={'provider-' + p.id}>
              <div className="setting-row">
                <div>
                  <b>{p.name}</b> {p.id === 'antigravity' && <span className="badge" title={t('Antigravity has no documented non-interactive mode')}>{t('Experimental')}</span>} {p.status.version && <span className="muted">{p.status.version}</span>}
                  <div className="muted">
                    <span className={'badge ai-state ' + p.status.state}>{stateLabel(t)[p.status.state] ?? p.status.state}</span>{' '}
                    {p.status.detail && (/[.!?]$/.test(p.status.detail) ? p.status.detail : p.status.detail + '.')}{p.status.hint && <> {rich(t('Run {cmd}.'), {cmd: <code>{p.status.hint}</code>})}</>}
                  </div>
                  {p.id === 'antigravity' && <div className="muted">{t("Experimental: Google does not document running agy from another app. Check Google's terms for your account before you switch it on; you use it at your own risk.")}</div>}
                </div>
                <Switch label={t('Enable {name}', {name: p.name})} checked={p.enabled} disabled={busy === p.id || (!p.enabled && !ready)} onChange={v => void toggle(p, v)} />
              </div>
              {busy === p.id && <div className="muted" role="status">{t('Testing isolation: the agent is asked to use a tool and must fail…')}</div>}
              {err[p.id] && <div className="error" role="alert">{err[p.id]}</div>}
              {p.enabled && (
                <div className="ai-provider-opts">
                  <div className="setting-row">
                    <div><b>{t('Default model')}</b>
                      <div className="muted">
                        {!ml ? t('Loading the models from the provider…') : ml.error ? t('Models could not be loaded: {error}', {error: ml.error}) : !ml.can_list ? t('This tool cannot list its models, so it uses its default one.') : t('Loaded from the provider.')}
                        {ml?.error && <> <button className="link" onClick={() => void reloadModels(p.id)}>{t('Retry')}</button></>}
                      </div>
                    </div>
                    <Select className="inline" label={t('Default model of {name}', {name: p.name})} value={ml ? p.model : ''} onChange={v => void setModel(p, v)} disabled={!ml}
                      placeholder={ml ? undefined : t('Loading…')}
                      options={(ml?.models ?? []).map(m => ({value: m.id, label: m.name || m.id}))} />
                  </div>
                  <div className="setting-row">
                    <div><b>{t('Default provider')}</b><div className="muted">{t('The assistant panel starts with this one.')}</div></div>
                    {p.default ? <span className="badge">{t('Default')}</span> : <button onClick={() => void be.assistant.setDefaultProvider(p.id).then(load).catch(e => toast.error(errMsg(e)))}>{t('Make default')}</button>}
                  </div>
                  <div className="muted ai-company">{t('The redacted terminal text is sent to {company}.', {company: company(t)[p.id] ?? t('whatever this command talks to')})}</div>
                  {p.custom && <div><button className="danger" onClick={() => void be.assistant.deleteCustomProvider(p.id).then(load).catch(e => toast.error(errMsg(e)))}>{t('Remove this provider')}</button></div>}
                </div>
              )}
            </div>
          )
        })}
        <div className="ai-row-actions">
          <button aria-busy={busy === 'refresh'} onClick={() => void refresh()} disabled={busy === 'refresh'}><IconRefresh size={14} /> {busy === 'refresh' ? t('Checking…') : t('Refresh')}</button>
          <button onClick={() => setCustomOpen(o => !o)}>{customOpen ? t('Cancel') : t('Add a command…')}</button>
        </div>
        {customOpen && <CustomProviderForm onSaved={() => { setCustomOpen(false); void load() }} />}
      </section>

      {settings && <section className="card-section" aria-label={t('AI privacy')}>
        <h4>{t('Privacy')}</h4>
        <div className="muted">{rich(t('Secrets (keys, tokens, passwords, anything saved in the vault) are replaced by {mask} before text is sent to a provider and again before an answer is shown. This cannot be switched off. It is best-effort: a password typed in plain sentences cannot always be recognised.'), {mask: <code>*******</code>})}</div>
        <div className="setting-row">
          <div><b>{t('Terminal lines shared')}</b><div className="muted">{t('How many of the last lines of the screen go with a question (20 to 1000).')}</div></div>
          <input aria-label={t('Lines shared')} type="number" min={20} max={1000} value={lines} className="ai-num" onChange={e => setLines(e.target.value)}
            onBlur={() => { const n = Number(lines); if (n !== settings!.lines) void saveSettings({lines: Number.isFinite(n) ? n : 200}) }} />
        </div>
        <label className="field">
          <span>{t('Extra patterns to hide (one regular expression per line; they only add to the built-in ones)')}</span>
          <textarea aria-label={t('Extra redaction patterns')} rows={3} value={patterns} placeholder="EMP-\d{6}" onChange={e => setPatterns(e.target.value)}
            onBlur={() => { const list = patterns.split('\n').map(s => s.trim()).filter(Boolean); if (list.join('\n') !== settings!.patterns.join('\n')) void saveSettings({patterns: list}) }} />
        </label>
        <div className="setting-row">
          <div><b>{t('Run read-only commands by itself')}</b><div className="muted">{t('The assistant runs commands that only look (ls, df, which, pgrep, brew list, systemctl status, …, joined by ; && || or a pipe) without asking, and reads their output. Anything that could change something still asks first, in the chat. Never on production hosts.')}</div></div>
          <Switch label={t('Run read-only commands by itself')} checked={settings!.auto_run} onChange={v => void saveSettings({auto_run: v})} />
        </div>
      </section>}

      <McpServersPanel providers={list} hosts={hosts} />
    </>
  )
}

function CustomProviderForm({onSaved}: {onSaved(): void}) {
  const t = useT()
  const be = useBackend()
  const [name, setName] = useState('')
  const [command, setCommand] = useState('')
  const [args, setArgs] = useState('')
  const [via, setVia] = useState<'stdin' | 'arg'>('stdin')
  const [err, setErr] = useState('')
  const save = async () => {
    try { await be.assistant.saveCustomProvider({name, command, args: splitArgs(args), prompt_via: via}); onSaved() } catch (e) { setErr(errMsg(e)) }
  }
  return (
    <div className="ai-custom" role="group" aria-label={t('Add a command')}>
      <div className="muted">{t('For an AI tool Vutty has no adapter for. Its tools cannot be checked from here, so it must pass the isolation self-test before it can be switched on. Use {prompt} in the arguments to place the prompt when it is passed as an argument.')}</div>
      <label className="field"><span>{t('Name')}</span><input aria-label={t('Provider name')} value={name} onChange={e => setName(e.target.value)} placeholder="My AI" /></label>
      <label className="field"><span>{t('Command')}</span><input aria-label={t('Provider command')} value={command} onChange={e => setCommand(e.target.value)} placeholder="mytool" /></label>
      <label className="field"><span>{t('Arguments')}</span><input aria-label={t('Provider arguments')} value={args} onChange={e => setArgs(e.target.value)} placeholder='--ask "{prompt}"' /></label>
      <label className="field"><span>{t('The prompt is passed')}</span>
        <Select label={t('Prompt passed by')} value={via} onChange={v => setVia(v as 'stdin' | 'arg')} options={[{value: 'stdin', label: t('on stdin')}, {value: 'arg', label: t('as an argument ({prompt})')}]} />
      </label>
      {err && <div className="error" role="alert">{err}</div>}
      <div><button className="primary" onClick={() => void save()} disabled={!name.trim() || !command.trim()}>{t('Add')}</button></div>
    </div>
  )
}


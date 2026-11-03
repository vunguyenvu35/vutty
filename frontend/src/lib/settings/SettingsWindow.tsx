import {Fragment, lazy, PointerEvent as ReactPointerEvent, Suspense, useEffect, useRef, useState} from 'react'
import {useBackend, type UpdateInfo} from '../backend'
import {useToast} from '../ui/Toast'
import {useConfirm} from '../ui/Confirm'
import {Switch} from '../ui/Switch'
import {Select} from '../ui/Select'
import {biometricName, errMsg} from '../util'
import {ThemePicker} from './ThemePicker'
import {resolveTerminalTheme} from '../terminal/themes'
import {IconArrowUpRight, IconCloud, IconEnterKey, IconFolder, IconGear, IconShield, IconSparkle, IconTerminal, IconUser} from '../ui/Icons'
import {formatBytes} from '../sftp/format'
import {ShortcutsEditor} from './ShortcutsEditor'
import {capture, formatCombo, matches} from '../shortcuts/shortcuts'
import type {Appearance} from './appearance'
import type {CursorStyle, Settings, ThemeMode, VaultStatus} from '../types'
import {DONATE_NETWORKS, KOFI, SHOW_CRYPTO_DONATE} from '../donate'

// what is not on the first page is loaded when it is first needed, so the window opens and answers clicks at once
const loadSupport = () => import('../panels/SupportPanel').then(m => ({default: m.SupportPanel}))
const loadSync = () => import('../panels/SyncPanel').then(m => ({default: m.SyncPanel}))
const loadAssistant = () => import('./AssistantSettings').then(m => ({default: m.AssistantSettings}))
const SupportPanel = lazy(loadSupport)
const SyncPanel = lazy(loadSync)
const AssistantSettings = lazy(loadAssistant)
// the pages are fetched right after the window opens, so a tab opens at once instead of flashing "Loading…"
const preload = () => { void loadSupport(); void loadSync(); void loadAssistant() }
const Loading = () => <div className="muted" role="status">Loading…</div>

/** what this computer calls the place that keeps the vault key for quick unlock */
const keychainName = () => {
  const p = typeof navigator === 'undefined' ? '' : navigator.platform || navigator.userAgent || ''
  return /mac/i.test(p) ? 'the macOS Keychain' : /windows|win32|win64/i.test(p) ? 'Windows Credential Manager' : 'the system keychain'
}

type Cat = 'general' | 'account' | 'terminal' | 'security' | 'assistant' | 'sync' | 'shortcuts' | 'storage'
interface CatInfo {id: Cat; label: string; icon: () => JSX.Element; desc: string; group: string}
// the order is the order of the keys (Mod+1…8) and of Ctrl+Tab; the groups only draw the menu
const CATS: CatInfo[] = [
  {id: 'general', label: 'General', icon: () => <IconGear size={16} />, group: 'Preferences', desc: 'How the interface looks and behaves.'},
  {id: 'account', label: 'Account', icon: () => <IconUser size={16} />, group: 'Preferences', desc: 'Your profile and ways to support Vutty.'},
  {id: 'terminal', label: 'Terminal', icon: () => <IconTerminal size={16} />, group: 'Preferences', desc: 'Theme, font, cursor and how the terminal reacts.'},
  {id: 'security', label: 'Security', icon: () => <IconShield size={16} />, group: 'Vault and integrations', desc: 'Lock the vault, unlock it faster and change its master password.'},
  {id: 'assistant', label: 'AI assistant', icon: () => <IconSparkle size={16} />, group: 'Vault and integrations', desc: 'Let an AI command-line tool you already use work in your terminal.'},
  {id: 'sync', label: 'Sync', icon: () => <IconCloud size={16} />, group: 'Vault and integrations', desc: 'Keep your encrypted hosts and keys in step across machines.'},
  {id: 'shortcuts', label: 'Shortcuts', icon: () => <IconEnterKey size={16} />, group: 'Advanced', desc: 'Change the keys that drive Vutty.'},
  {id: 'storage', label: 'Storage', icon: () => <IconFolder size={16} />, group: 'Advanced', desc: 'Clear what Vutty can rebuild, or start the settings over.'},
]

const FONTS = [
  {label: 'Menlo', value: "Menlo, Monaco, 'Courier New', monospace"},
  {label: 'SF Mono', value: "'SF Mono', Menlo, monospace"},
  {label: 'JetBrains Mono', value: "'JetBrains Mono', Menlo, monospace"},
  {label: 'Monaco', value: 'Monaco, Menlo, monospace'},
  {label: 'Courier New', value: "'Courier New', monospace"},
]

const REPO = 'https://github.com/vunguyenvu35/vutty'
const LINKS: [string, string][] = [
  ['Documentation', REPO + '#readme'],
  ['Request a feature', REPO + '/issues/new'],
  ['Report a bug', REPO + '/issues/new'],
  ['Release notes', REPO + '/blob/main/RELEASE_NOTES.md'],
]

const DONATE: [string, string][] = DONATE_NETWORKS.map(n => [`${n.name.replace(' · ', ' / ')} (${n.chain})`, n.address])

interface Props {
  settings: Settings
  appearance: Appearance
  vault: VaultStatus
  onSettings(s: Settings): void
  onVault(): void
  onChanged(): void
  onClose(): void
  /** Rendered inside its own native window (the Settings process): no floating chrome, the OS provides it. */
  native?: boolean
  /** false while the saved settings are still being fetched (the window is already up): the menu works, the pages wait */
  ready?: boolean
  /** true while the values shown are the ones remembered from last time: they show, but nothing is saved over the real ones yet */
  stale?: boolean
}

/**
 * Settings as its own window: a floating window with a title bar, a menu on the left
 * (categories, theme, help links, version) and cards on the right. The dots work like macOS: red closes,
 * yellow rolls the window up to its title bar, green maximises (double-click the title bar too). Drag the
 * title bar to move it and the bottom-right corner to resize. Wails v2 can only create one OS window, so
 * this is an in-app window layer rather than a second native window. Changes apply immediately.
 */
export function SettingsWindow({settings: s, appearance, vault, onSettings, onVault, onChanged, onClose, native = false, ready = true, stale = false}: Props) {
  const be = useBackend()
  const toast = useToast()
  const confirm = useConfirm()
  const [cat, setCat] = useState<Cat>('general')
  const catRef = useRef(cat)
  catRef.current = cat
  const [version, setVersion] = useState('')
  const [logFolder, setLogFolder] = useState('')
  const [scroll, setScroll] = useState(String(s.scrollback))
  const [auto, setAuto] = useState(String(vault.auto_lock_minutes))
  const [hint, setHint] = useState(vault.hint ?? '')
  const [oldPw, setOldPw] = useState('')
  const [newPw, setNewPw] = useState('')
  const [name, setName] = useState(s.account_name)
  const [email, setEmail] = useState(s.account_email)
  const [err, setErr] = useState('')
  const [offset, setOffset] = useState({x: 0, y: 0})
  const [maximized, setMaximized] = useState(false)
  const [rolledUp, setRolledUp] = useState(false)
  const drag = useRef<{sx: number; sy: number; ox: number; oy: number} | null>(null)

  const [newer, setNewer] = useState<UpdateInfo | null>(null)
  const [checking, setChecking] = useState(false)
  const [supportOpen, setSupportOpen] = useState(false)
  const checkUpdate = async () => {
    setChecking(true)
    try {
      const u = await be.system.checkUpdate()
      setNewer(u)
      toast.info(u.available ? `Version ${u.latest} is available` : 'You are on the latest version')
    } catch (e) { toast.error(errMsg(e)) } finally { setChecking(false) }
  }
  useEffect(() => { const t = setTimeout(preload, 150); return () => clearTimeout(t) }, [])
  useEffect(() => { void be.system.info().then(i => setVersion(i.version)).catch(() => {}) }, [be])
  // where session logs go now: asked again when the choice changes
  useEffect(() => { if (ready && s.session_log !== 'off') void be.settings.sessionLogFolder().then(setLogFolder).catch(() => {}) }, [be, ready, s.session_log, s.session_log_dir])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !document.querySelector('.select-menu') && !document.querySelector('.key-capture') && !document.querySelector('.modal')) onClose() // a dropdown or a recording eats its own Esc
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, native])
  // Mod+1…8 jump to a category, Ctrl+Tab / Ctrl+Shift+Tab go to the next / previous one (not while a shortcut is being recorded)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (capture.active) return
      const go = (i: number) => { e.preventDefault(); setCat(CATS[(i + CATS.length) % CATS.length].id); setErr('') }
      const n = CATS.findIndex((_, i) => matches(e, 'Mod+' + (i + 1)))
      if (n >= 0) return go(n)
      const at = CATS.findIndex(c => c.id === catRef.current)
      if (matches(e, 'Ctrl+Tab')) go(at + 1)
      else if (matches(e, 'Ctrl+Shift+Tab')) go(at - 1)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const startDrag = (e: ReactPointerEvent) => {
    if (maximized || (e.target as HTMLElement).closest('button')) return
    drag.current = {sx: e.clientX, sy: e.clientY, ox: offset.x, oy: offset.y}
    ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
  }
  const moveDrag = (e: ReactPointerEvent) => {
    const d = drag.current
    if (!d) return
    const limitX = window.innerWidth / 2, limitY = window.innerHeight / 2
    setOffset({x: Math.max(-limitX, Math.min(limitX, d.ox + e.clientX - d.sx)), y: Math.max(-limitY, Math.min(limitY, d.oy + e.clientY - d.sy))})
  }
  const endDrag = () => { drag.current = null }
  const toggleMax = () => { setMaximized(m => !m); setRolledUp(false); setOffset({x: 0, y: 0}) }

  // the fields that start from the saved values are filled in again when those arrive
  useEffect(() => {
    if (!ready) return
    setScroll(String(s.scrollback)); setAuto(String(vault.auto_lock_minutes)); setHint(vault.hint ?? ''); setName(s.account_name); setEmail(s.account_email)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, stale])
  const update = async (patch: Partial<Settings>) => {
    if (!ready || stale) return // never save the placeholder or remembered values over the real ones
    try { onSettings(await be.settings.save({...s, ...patch})) } catch (e) { toast.error(errMsg(e)) }
  }
  const guard = async (fn: () => Promise<void>, ok: string) => {
    setErr('')
    try { await fn(); toast.info(ok) } catch (e) { setErr(errMsg(e)) }
  }

  const [busyStorage, setBusyStorage] = useState(false)
  const clearCache = async () => {
    setBusyStorage(true)
    try {
      const r = await be.settings.clearCache()
      toast.info(r.files > 0 ? `Cache cleared: ${r.files} file${r.files === 1 ? '' : 's'}, ${formatBytes(r.bytes)}` : 'Cache cleared')
    } catch (e) { toast.error(errMsg(e)) } finally { setBusyStorage(false) }
  }
  const resetSettings = async () => {
    if (!await confirm('Reset every setting to its default? Your profile, sync target, hosts, keys and vault stay as they are.', {confirmLabel: 'Reset settings', danger: true})) return
    setBusyStorage(true)
    try {
      const next = await be.settings.reset()
      onSettings(next)
      onVault() // the auto-lock time is part of the vault status
      setScroll(String(next.scrollback))
      setAuto(String(next.auto_lock_minutes))
      toast.info('Settings reset to their defaults')
    } catch (e) { toast.error(errMsg(e)) } finally { setBusyStorage(false) }
  }

  const term = resolveTerminalTheme(s.terminal_theme, appearance).theme
  const fontKnown = FONTS.some(f => f.value === s.font_family)
  const initial = (s.account_name.trim() || s.account_email.trim() || '?')[0].toUpperCase()

  return (
    <div className={'win-back' + (maximized ? ' maximized' : '') + (native ? ' native' : '')} role="presentation">
      <div className={'win' + (maximized ? ' max' : '') + (rolledUp ? ' rolled' : '') + (native ? ' native' : '')} role="dialog" aria-label="Settings"
        style={maximized || native ? undefined : {transform: `translate(${offset.x}px, ${offset.y}px)`}}>
        <div className="win-title" {...(native ? {onDoubleClick: (e: React.MouseEvent) => { if (!(e.target as HTMLElement).closest('button')) void be.system.toggleMaximise() }} : {onPointerDown: startDrag, onPointerMove: moveDrag, onPointerUp: endDrag, onPointerCancel: endDrag, onDoubleClick: toggleMax})}>
          {/* maximised, the window covers the app and the native macOS buttons sit at the top left: the in-app dots give way to Restore / Close on the right */}
          {!maximized && !native && <span className="win-dots">
            <button className="dot close" aria-label="Close settings" title="Close" onClick={onClose} />
            <button className="dot min" aria-label={rolledUp ? 'Unroll settings' : 'Minimize settings'} title={rolledUp ? 'Show' : 'Minimize'} onClick={() => setRolledUp(r => !r)} />
            <button className="dot max" aria-label={maximized ? 'Restore settings' : 'Maximize settings'} title={maximized ? 'Restore' : 'Maximize'} onClick={toggleMax} />
          </span>}
          <span className="win-name">Settings</span>
          {maximized && (
            <span className="win-actions">
              <button className="win-act" aria-label="Restore settings" title="Restore" onClick={toggleMax}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="8" y="3" width="13" height="13" rx="2" /><path d="M16 21H5a2 2 0 0 1-2-2V8" /></svg>
              </button>
              <button className="win-act" aria-label="Close settings" title="Close" onClick={onClose}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18" /></svg>
              </button>
            </span>
          )}
        </div>

        {!rolledUp && (
          <div className="win-body">
            <nav className="win-nav" aria-label="Settings menu">
              <div role="tablist" aria-orientation="vertical" className="win-tabs">
                {CATS.map((c, i) => (
                  <Fragment key={c.id}>
                    {c.group !== CATS[i - 1]?.group && <div className="win-group" aria-hidden="true">{c.group}</div>}
                    <button role="tab" aria-selected={cat === c.id} className={'settings-tab' + (cat === c.id ? ' active' : '')} onClick={() => { setCat(c.id); setErr('') }}>
                      <i className="tab-ico" aria-hidden="true">{c.icon()}</i>
                      <span className="tab-name">{c.label}</span>
                      <kbd className="settings-key" aria-hidden="true">{formatCombo('Mod+' + (i + 1))}</kbd>
                    </button>
                  </Fragment>
                ))}
              </div>
              <div className="spacer" />
              <div className="win-group" aria-hidden="true">Help</div>
              {LINKS.map(([label, url]) => (
                <button key={label} className="win-link" onClick={() => be.system.openUrl(url)}>{label}<IconArrowUpRight size={14} /></button>
              ))}
              <button className="win-link" onClick={() => setSupportOpen(true)}>Support Vutty</button>
              <div className="win-sep" />
              <div className="win-row">
                <span>Theme</span>
                <Select className="inline" label="Theme" value={s.theme} onChange={v => update({theme: v as ThemeMode})} disabled={!ready}
                  options={[{value: 'auto', label: 'Auto'}, {value: 'light', label: 'Light'}, {value: 'dark', label: 'Dark'}]} />
              </div>
              {newer?.available && (
                <button className="win-link update" onClick={() => be.system.openUrl(newer.url)}>Version {newer.latest} is available<IconArrowUpRight size={14} /></button>
              )}
              <div className="win-row version">
                <span>Version <b data-testid="app-version">{version || '…'}</b></span>
                <button className="link" disabled={checking} onClick={() => void checkUpdate()}>{checking ? 'Checking…' : 'Check for updates'}</button>
              </div>
            </nav>

            <div className="win-content">
              <div className="win-page">
              <header className="page-head">
                <h2>{CATS.find(c => c.id === cat)?.label}</h2>
                <p>{CATS.find(c => c.id === cat)?.desc}</p>
              </header>
              {!ready ? <Loading /> : <>
              {cat === 'general' && (
                <>
                  <section className="card-section">
                    <h4>Interface</h4>
                    <div className="setting-row">
                      <div><b>Zoom</b><div className="muted">Make the whole interface larger or smaller (80% to 160%).</div></div>
                      <div className="stepper">
                        <button aria-label="Zoom out" disabled={s.zoom <= 80} onClick={() => update({zoom: Math.max(80, s.zoom - 10)})}>−</button>
                        <output aria-label="Zoom level" className="zoom-level">{s.zoom}%</output>
                        <button aria-label="Zoom in" disabled={s.zoom >= 160} onClick={() => update({zoom: Math.min(160, s.zoom + 10)})}>+</button>
                        <button aria-label="Reset zoom" disabled={s.zoom === 100} onClick={() => update({zoom: 100})}>Reset</button>
                      </div>
                    </div>
                    <div className="setting-row">
                      <div><b>Reduce motion</b><div className="muted">Turn off animations and transitions.</div></div>
                      <Switch label="Reduce motion" checked={s.reduce_motion} onChange={v => update({reduce_motion: v})} />
                    </div>
                  </section>
                  <section className="card-section">
                    <h4>Behaviour</h4>
                    <div className="setting-row">
                      <div><b>Confirm before closing a tab</b><div className="muted">Ask first, because closing a terminal or SFTP tab ends its connection.</div></div>
                      <Switch label="Confirm before closing a tab" checked={s.confirm_tab_close} onChange={v => update({confirm_tab_close: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Be careful on production hosts</b><div className="muted">In the terminal of a host marked <i>prod</i>, ask before text with a line break is pasted (the line would run at once) and before a snippet or a history command is run. Terminals of production hosts also carry a red PROD badge.</div></div>
                      <Switch label="Be careful on production hosts" checked={s.confirm_prod} onChange={v => update({confirm_prod: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Check for updates automatically</b><div className="muted">When Vutty opens, ask GitHub (at most once a day) whether a newer release exists. Only the release information is read; nothing about you is sent.</div></div>
                      <Switch label="Check for updates automatically" checked={s.auto_update_check} onChange={v => update({auto_update_check: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Ask which protocol to use</b><div className="muted">Opening a host first shows SSH, Mosh and Telnet to choose from. Turn it off to connect with SSH straight away (hosts that have Telnet still ask).</div></div>
                      <Switch label="Ask which protocol to use" checked={s.ask_protocol} onChange={v => update({ask_protocol: v})} />
                    </div>
                  </section>
                </>
              )}

              {cat === 'account' && (
                <>
                  <section className="card-section account-card">
                    <h4>Profile</h4>
                    <div className="profile">
                      <span className="avatar" aria-hidden="true">{s.account_name || s.account_email ? initial : <IconUser size={26} />}</span>
                      <div className="profile-fields">
                        <input aria-label="Display name" className="control wide" placeholder="Display name" value={name} onChange={e => setName(e.target.value)} />
                        <input aria-label="Email" className="control wide" placeholder="Email (optional)" value={email} onChange={e => setEmail(e.target.value)} />
                      </div>
                    </div>
                    <div><button className="primary" disabled={name.trim() === s.account_name && email.trim() === s.account_email}
                      onClick={() => guard(async () => { onSettings(await be.settings.save({...s, account_name: name, account_email: email})) }, 'Profile saved')}>Save profile</button></div>
                    <div className="muted">Stored only on this Mac, in your settings file. It is not sent anywhere.</div>
                  </section>
                  <section className="card-section">
                    <h4>Donate</h4>
                    <div className="muted">{SHOW_CRYPTO_DONATE
                      ? 'Vutty is free and open source. If it saves you time, you can send USDT. Check the network before you send.'
                      : 'Vutty is free and open source. If it saves you time, you can support it on Ko-fi.'}</div>
                    <div className="muted">☕ $3 – a cup of coffee · 🍜 $10 – a lunch · 🚀 $25 – fuel new features</div>
                    {!SHOW_CRYPTO_DONATE && <div><button className="primary" onClick={() => void be.system.openUrl(KOFI)}>Support on Ko-fi</button></div>}
                    {SHOW_CRYPTO_DONATE && DONATE.map(([label, addr]) => (
                      <div className="setting-row" key={addr}>
                        <div><b>{label}</b><div className="muted" style={{wordBreak: 'break-all', userSelect: 'text'}}>{addr}</div></div>
                        <button aria-label={'Copy ' + label + ' address'} onClick={() => { void be.system.writeClipboard(addr).then(() => toast.info('Address copied')).catch(e => toast.error(errMsg(e))) }}>Copy</button>
                      </div>
                    ))}
                  </section>
                  <section className="card-section">
                    <h4>Plan</h4>
                    <div className="setting-row">
                      <div><b>Local</b> <span className="badge">free · open source</span><div className="muted">All features are available without an account.</div></div>
                    </div>
                  </section>
                  <section className="card-section">
                    <h4>Cloud account</h4>
                    <div className="setting-row">
                      <div><b>Sign in</b><div className="muted">Accounts, hosted backup and team sharing are planned. Until then your data stays on this Mac, and you can sync it through your own Git repository (Sync tab).</div></div>
                      <button disabled title="Not available yet">Sign in</button>
                    </div>
                    <div className="setting-row">
                      <div><b>Team sharing</b><div className="muted">Share hosts and snippets with a team.</div></div>
                      <span className="badge">coming later</span>
                    </div>
                  </section>
                </>
              )}

              {cat === 'terminal' && (
                <>
                  <section className="card-section">
                    <h4>Terminal theme</h4>
                    <ThemePicker value={s.terminal_theme} appearance={appearance} onChange={id => update({terminal_theme: id})} />
                  </section>
                  <section className="card-section">
                    <h4>Text and cursor</h4>
                    <div className="setting-row">
                      <b>Font</b>
                      <Select className="control" label="Font family" value={s.font_family} onChange={v => update({font_family: v})}
                        options={[...(fontKnown ? [] : [{value: s.font_family, label: 'Custom'}]), ...FONTS.map(f => ({value: f.value, label: f.label}))]} />
                    </div>
                    <div className="setting-row">
                      <b>Font size</b>
                      <div className="stepper">
                        <button aria-label="Decrease font size" disabled={s.font_size <= 8} onClick={() => update({font_size: s.font_size - 1})}>−</button>
                        <input aria-label="Font size" className="control" type="number" min={8} max={40} value={s.font_size} onChange={e => update({font_size: Number(e.target.value)})} />
                        <button aria-label="Increase font size" disabled={s.font_size >= 40} onClick={() => update({font_size: s.font_size + 1})}>+</button>
                      </div>
                    </div>
                    <div className="setting-row">
                      <b>Cursor</b>
                      <div className="segmented" role="radiogroup" aria-label="Cursor style">
                        {(['block', 'bar', 'underline'] as CursorStyle[]).map(c => (
                          <button key={c} role="radio" aria-checked={s.cursor_style === c} className={s.cursor_style === c ? 'on' : ''} onClick={() => update({cursor_style: c})}>{c}</button>
                        ))}
                      </div>
                    </div>
                    <div className="setting-row">
                      <b>Blinking cursor</b>
                      <Switch label="Blinking cursor" checked={s.cursor_blink} onChange={v => update({cursor_blink: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Copy on select</b><div className="muted">Selecting text copies it to the clipboard at once.</div></div>
                      <Switch label="Copy on select" checked={s.copy_on_select} onChange={v => update({copy_on_select: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Right click</b><div className="muted">What a right click does inside a terminal. Copy and paste keys are in Shortcuts.</div></div>
                      <Select label="Right click" className="inline" value={s.right_click} onChange={v => update({right_click: v as Settings['right_click']})}
                        options={[{value: 'paste', label: 'Paste', hint: 'Right-click pastes'}, {value: 'menu', label: 'Show a menu', hint: 'Copy, Paste, Select all, Clear'}, {value: 'none', label: 'Do nothing'}]} />
                    </div>
                    <div className="setting-row">
                      <div><b>Highlight output</b><div className="muted">Colour URLs, IP addresses, paths, dates and the words error, warning and success. Colours chosen by a program are kept.</div></div>
                      <Switch label="Highlight output" checked={s.highlight} onChange={v => update({highlight: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Suggest commands</b><div className="muted">While you type, a list of the commands that match appears under the cursor (↓ ↑ choose a row, Enter or Tab put it on the line, Esc closes it), and ⌘⇧J (Ctrl+Shift+J elsewhere) opens it by hand. It uses the commands you ran in Vutty, the shell history of this computer (~/.zsh_history, ~/.bash_history) in the local terminal, and your Snippets of one line; it is not offered in vim or at a password prompt, and a line that looks like a secret is never suggested.</div></div>
                      <Switch label="Suggest commands" checked={s.suggest} onChange={v => update({suggest: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Suggest from the host's shell history</b><div className="muted">Also read the last 3000 commands of ~/.bash_history and ~/.zsh_history on an SSH host, once when its terminal opens. They are kept in memory only, and a command with a secret in it is left out.</div></div>
                      <Switch label="Suggest from the host's shell history" checked={s.suggest_host} disabled={!s.suggest} onChange={v => update({suggest_host: v})} />
                    </div>
                    <div className="setting-row">
                      <div><b>Scrollback</b><div className="muted">Lines kept per terminal (500 to 100000).</div></div>
                      <input aria-label="Scrollback lines" className="control narrow" type="number" value={scroll}
                        onChange={e => setScroll(e.target.value)} onBlur={() => update({scrollback: Number(scroll)})}
                        onKeyDown={e => e.key === 'Enter' && update({scrollback: Number(scroll)})} />
                    </div>
                  </section>
                  <section className="card-section">
                    <h4>Session logs</h4>
                    <div className="setting-row">
                      <div><b>Save a log of terminal sessions</b><div className="muted">One text file for each session, line by line with the time: what the host printed, with colours and cursor moves taken out. What you type is not recorded, except what the host echoes back. A log holds everything that was shown on screen, a password in a file you displayed included, so keep the folder private. Files are readable only by you and each stops at 50 MB.</div></div>
                      <Select label="Session logs" className="control" value={s.session_log} onChange={v => update({session_log: v as Settings['session_log']})}
                        options={[{value: 'off', label: 'Off'}, {value: 'prod', label: 'Production hosts only', hint: 'Hosts marked prod'}, {value: 'all', label: 'All sessions', hint: 'SSH, Telnet, serial and this computer'}]} />
                    </div>
                    {s.session_log !== 'off' && (
                      <div className="setting-row">
                        <div><b>Folder</b><div className="muted mono" style={{wordBreak: 'break-all', userSelect: 'text'}} data-testid="session-log-folder">{logFolder || '…'}</div></div>
                        <div className="fixed-row">
                          <button onClick={() => void be.system.pickDirectory('Choose the folder for session logs').then(d => { if (d) void update({session_log_dir: d}) }).catch(e => toast.error(errMsg(e)))}>Choose…</button>
                          {s.session_log_dir && <button onClick={() => void update({session_log_dir: ''})}>Use the default</button>}
                          <button disabled={!logFolder} onClick={() => void be.system.openPath(logFolder).catch(e => toast.error(errMsg(e)))}>Open</button>
                        </div>
                      </div>
                    )}
                  </section>
                  <section className="card-section">
                    <h4>Preview</h4>
                    <pre className="term-preview" data-testid="term-preview" style={{background: term.background, color: term.foreground, fontFamily: s.font_family, fontSize: s.font_size}}>
                      <span style={{color: term.green}}>demo@app-server</span>:<span style={{color: term.blue}}>~</span>$ ls -la{'\n'}
                      <span style={{color: term.blue}}>drwxr-xr-x</span> <span style={{color: term.yellow}}>5</span> demo  <span style={{color: term.cyan}}>projects</span>{'\n'}
                      <span style={{color: term.red}}>error:</span> permission denied <span style={{background: term.selectionBackground}}> selected text </span>{'\n'}
                      <span style={{color: term.green}}>demo@app-server</span>:<span style={{color: term.blue}}>~</span>$ <span style={{background: term.cursor, color: term.background}}>{s.cursor_style === 'block' ? ' ' : ''}</span>
                    </pre>
                  </section>
                </>
              )}

              {cat === 'security' && (
                <>
                  <section className="card-section">
                    <h4>Vault</h4>
                    <div className="setting-row">
                      <div><b>Auto-lock</b><div className="muted">Minutes of inactivity before the vault locks (0 = never).</div></div>
                      <div className="fixed-row">
                        <input aria-label="Auto-lock minutes" className="control narrow" value={auto} onChange={e => setAuto(e.target.value)} />
                        <button onClick={() => guard(async () => { await be.vault.setAutoLock(Number(auto)); onVault() }, 'Auto-lock updated')}>Apply</button>
                      </div>
                    </div>
                    <div className="setting-row">
                      <div><b>Lock when the computer sleeps</b><div className="muted">The vault locks as the computer goes to sleep, and on a Mac when the screen is locked, so nobody who opens the lid finds your keys unlocked. Open terminals stay connected. Where the system does not announce a sleep, the vault locks when the computer wakes up.</div></div>
                      <Switch label="Lock when the computer sleeps" checked={s.lock_on_sleep} onChange={v => update({lock_on_sleep: v})} />
                    </div>
                    <div className="setting-row">
                      <div>
                        <b>{vault.biometric_available ? `Unlock with ${biometricName()}` : `Unlock with ${keychainName()}`}</b>
                        <div className="muted">{vault.biometric_available
                          ? `Use ${biometricName()} instead of typing the master password. The vault key stays in ${keychainName()}, and each unlock asks you to confirm.`
                          : `Stores the vault key in ${keychainName()}, so you do not type the master password. Anyone signed in to this computer account can then open the vault.`}</div>
                      </div>
                      <Switch label="Keychain unlock" checked={vault.keychain_enabled}
                        onChange={v => guard(async () => { await (v ? be.vault.enableBiometric() : be.vault.disableBiometric()); onVault() }, v ? 'Keychain unlock enabled' : 'Keychain unlock disabled')} />
                    </div>
                    {vault.biometric_available && vault.keychain_enabled && (
                      <div className="setting-row">
                        <div><b>Ask as soon as Vutty opens</b><div className="muted">The lock screen shows the {biometricName()} sheet by itself. Off: press "Use {biometricName()}" yourself.</div></div>
                        <Switch label="Ask on open" checked={s.biometric_on_open} onChange={v => update({biometric_on_open: v})} />
                      </div>
                    )}
                  </section>
                  <section className="card-section">
                    <h4>Password hint</h4>
                    <div className="muted">A reminder shown on the lock screen if you forget the master password. Anyone who opens Vutty can read it, so never write the password in it.</div>
                    <div className="input-row">
                      <input aria-label="Password hint" className="control wide" maxLength={120} placeholder="A reminder only you understand" value={hint} onChange={e => setHint(e.target.value)} />
                      <button disabled={hint.trim() === (vault.hint ?? '')}
                        onClick={() => guard(async () => { await be.vault.setHint(hint); onVault() }, hint.trim() ? 'Password hint saved' : 'Password hint removed')}>Save</button>
                    </div>
                  </section>
                  <section className="card-section">
                    <h4>Change master password</h4>
                    <input aria-label="Current password" type="password" className="control wide" placeholder="Current password" value={oldPw} onChange={e => setOldPw(e.target.value)} />
                    <input aria-label="New password" type="password" className="control wide" placeholder="New password (12+ characters)" value={newPw} onChange={e => setNewPw(e.target.value)} />
                    <div><button disabled={!oldPw || newPw.length < 12}
                      onClick={() => guard(async () => { await be.vault.changePassword(oldPw, newPw); setOldPw(''); setNewPw('') }, 'Master password changed')}>Change password</button></div>
                    <div className="muted">Forgot it? Lock the vault: the unlock screen offers a reset.</div>
                  </section>
                  {err && <div className="error" role="alert">{err}</div>}
                </>
              )}

              {cat === 'assistant' && <Suspense fallback={<Loading />}><AssistantSettings /></Suspense>}

              {cat === 'sync' && <Suspense fallback={<Loading />}><SyncPanel page vault={vault} onClose={() => {}} onChanged={onChanged} /></Suspense>}

              {cat === 'shortcuts' && <ShortcutsEditor overrides={s.shortcuts ?? {}} onChange={shortcuts => update({shortcuts})} />}

              {cat === 'storage' && (
                <section className="card-section">
                  <h4>Storage</h4>
                  <div className="setting-row">
                    <div><b>Clear cache</b><div className="muted">Removes files Vutty can make again and what it remembers about your AI tools (they are looked up again). Your hosts, keys, vault, settings and history are not touched.</div></div>
                    <button disabled={busyStorage} onClick={() => void clearCache()}>Clear cache</button>
                  </div>
                  <div className="setting-row">
                    <div><b>Reset settings</b><div className="muted">Puts appearance, terminal, general options, shortcuts and auto-lock back to their defaults. Your profile and sync target stay. Hosts, keys and the vault are not touched.</div></div>
                    <button className="danger" disabled={busyStorage} onClick={() => void resetSettings()}>Reset settings…</button>
                  </div>
                </section>
              )}

              {err && cat === 'account' && <div className="error" role="alert">{err}</div>}
              </>}
              </div>
            </div>
          </div>
        )}
      </div>
      {supportOpen && <Suspense fallback={null}><SupportPanel onClose={() => setSupportOpen(false)} /></Suspense>}
    </div>
  )
}

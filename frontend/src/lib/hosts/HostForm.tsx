import {useEffect, useRef, useState} from 'react'
import {useT, useTn} from '../i18n'
import {Field, SidePanel, Section} from '../ui/Modal'
import {resolveTerminalTheme} from '../terminal/themes'
import {ThemePicker} from '../terminal/ThemePicker'
import {useAppearance} from '../settings/appearance'
import {Select} from '../ui/Select'
import {useBackend} from '../backend'
import {errMsg, parseTags} from '../util'
import {IconCheckCircle, IconEye, IconEyeOff, IconPlus, IconServer, IconWarn, IconXCircle} from '../ui/Icons'
import {Switch} from '../ui/Switch'
import {AuthPrompt} from '../ui/AuthPrompt'
import type {AiPolicy, AuthChallenge, ConnectionTest, Group, Host, HostKeyPrompt, Identity, Settings} from '../types'

interface Props {
  host?: Host
  /** the host is a draft (for example from quick connect), not an existing record */
  isNew?: boolean
  groups: Group[]
  /** the group open on the hosts page: a new host starts in it */
  defaultGroupId?: string
  hosts: Host[]
  identities: Identity[]
  onClose(): void
  onSaved(h: Host): void
  /** Save, then open a terminal. */
  onConnect?(h: Host): void
  /** Lets the page show the host being typed as a draft card. */
  onDraft?(text: string): void
}

type Test = {state: 'idle'} | {state: 'running'} | {state: 'done'; result: ConnectionTest} | {state: 'failed'; message: string}

let testSeq = 0

/**
 * New / edit host, docked next to the host list. Flow: address → general → port → credentials
 * (username + password, or a key identity) → options. Test connection checks the typed values without
 * saving anything; Connect saves and opens a terminal.
 */
export function HostForm({host, isNew, groups, defaultGroupId, hosts, identities, onClose, onSaved, onConnect, onDraft}: Props) {
  const t = useT()
  const tn = useTn()
  const be = useBackend()
  const [appSetting, setAppSetting] = useState('auto')
  const [appTheme, setAppTheme] = useState<Settings['theme']>('dark')
  const appearance = useAppearance(appTheme)
  useEffect(() => { void be.settings.get().then(s => { setAppSetting(s.terminal_theme || 'auto'); setAppTheme(s.theme) }).catch(() => {}) }, [be])
  const [f, setF] = useState({
    label: host?.label ?? '',
    hostname: host?.hostname ?? '',
    port: String(host?.port ?? 22),
    username: host?.username ?? '',
    password: '',
    identity_id: host?.identity_id ?? '',
    group_id: host?.group_id || (host?.id ? '' : defaultGroupId ?? ''),
    jump_host_id: host?.jump_host_id ?? '',
    env: host?.env ?? '',
    tags: (host?.tags ?? []).join(', '),
    keepalive_sec: String(host?.keepalive_sec ?? 30),
    // an empty Telnet port means Telnet is off for this host
    telnet_on: !!host?.telnet_enabled,
    telnet_port: host?.telnet_enabled ? String(host.telnet_port || 23) : '',
    telnet_username: host?.telnet_username ?? '',
    telnet_password: '',
    telnet_encoding: host?.telnet_encoding || 'utf-8',
    // FTP for the files of the host, in the SFTP tab: plain, or FTPS with explicit or implicit TLS
    ftp_on: !!host?.ftp_enabled,
    ftp_port: host?.ftp_enabled ? String(host.ftp_port || 21) : '',
    ftp_security: host?.ftp_enabled ? host.ftp_security || 'plain' : 'explicit', // an existing host with no mode is plain FTP
    ftp_username: host?.ftp_username ?? '',
    ftp_password: '',
    // Remote Desktop: Vutty starts the system's own client; there is no password here
    rdp_on: !!host?.rdp_enabled,
    rdp_port: host?.rdp_enabled ? String(host.rdp_port || 3389) : '',
    rdp_username: host?.rdp_username ?? '',
    terminal_theme: host?.terminal_theme ?? '',
    ai_policy: (host?.ai_policy ?? '') as AiPolicy,
    forward_agent: !!host?.forward_agent,
  })
  const [showPw, setShowPw] = useState(false)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [test, setTest] = useState<Test>({state: 'idle'})
  const [prompt, setPrompt] = useState<{key: string; p: HostKeyPrompt} | null>(null)
  const [auth, setAuth] = useState<{key: string; c: AuthChallenge} | null>(null)
  const alive = useRef(true)
  useEffect(() => () => { alive.current = false }, [])
  const [showTelnetPw, setShowTelnetPw] = useState(false)
  const [showFtpPw, setShowFtpPw] = useState(false)
  const set = (k: Exclude<keyof typeof f, 'telnet_on' | 'ftp_on' | 'rdp_on'>) => (e: {target: {value: string}}) => setF(x => ({...x, [k]: e.target.value}))
  const existing = !!host && !isNew

  useEffect(() => { onDraft?.(f.label.trim() || f.hostname.trim()) }, [f.label, f.hostname, onDraft])
  useEffect(() => () => onDraft?.(''), [onDraft])

  /** Validates the form; returns the host to save, or null after showing the problem. */
  const draft = (): Partial<Host> | null => {
    const port = Number(f.port)
    if (!f.hostname.trim()) { setErr(t('Hostname is required')); return null }
    if (/\s/.test(f.hostname) || /\s/.test(f.username)) { setErr(t('Hostname and username cannot contain spaces')); return null }
    if (!Number.isInteger(port) || port < 1 || port > 65535) { setErr(t('Port must be between 1 and 65535')); return null }
    const telnet = !f.telnet_on ? 0 : f.telnet_port.trim() === '' ? 23 : Number(f.telnet_port)
    if (f.telnet_on && (!Number.isInteger(telnet) || telnet < 1 || telnet > 65535)) { setErr(t('Telnet port must be between 1 and 65535')); return null }
    const ftp = !f.ftp_on ? 0 : f.ftp_port.trim() === '' ? (f.ftp_security === 'implicit' ? 990 : 21) : Number(f.ftp_port)
    if (f.ftp_on && (!Number.isInteger(ftp) || ftp < 1 || ftp > 65535)) { setErr(t('FTP port must be between 1 and 65535')); return null }
    const rdp = !f.rdp_on ? 0 : f.rdp_port.trim() === '' ? 3389 : Number(f.rdp_port)
    if (f.rdp_on && (!Number.isInteger(rdp) || rdp < 1 || rdp > 65535)) { setErr(t('Remote Desktop port must be between 1 and 65535')); return null }
    setErr('')
    return {
      ...(existing ? host : {}),
      ftp_enabled: ftp > 0,
      ftp_port: ftp,
      ftp_security: f.ftp_on && f.ftp_security !== 'plain' ? f.ftp_security : '',
      ftp_username: f.ftp_on ? f.ftp_username.trim() : '',
      rdp_enabled: rdp > 0,
      rdp_port: rdp,
      rdp_username: f.rdp_on ? f.rdp_username.trim() : '',
      telnet_enabled: telnet > 0,
      telnet_port: telnet,
      telnet_username: f.telnet_on ? f.telnet_username.trim() : '',
      telnet_encoding: f.telnet_on && f.telnet_encoding !== 'utf-8' ? f.telnet_encoding : '',
      terminal_theme: f.terminal_theme,
      ai_policy: f.ai_policy,
      forward_agent: f.forward_agent,
      label: f.label.trim(),
      hostname: f.hostname.trim(),
      port,
      username: f.username.trim(),
      identity_id: f.identity_id,
      group_id: f.group_id,
      jump_host_id: f.jump_host_id,
      env: f.env as Host['env'],
      tags: parseTags(f.tags),
      keepalive_sec: Number(f.keepalive_sec) || 30,
    }
  }

  /** Saves the host; a typed password becomes a vault identity first. */
  const persist = async (): Promise<Host | null> => {
    const h = draft()
    if (!h) return null
    setBusy(true)
    try {
      if (f.password && !f.identity_id) {
        const name = `${h.label || h.hostname} (password)`
        const ident = await be.vault.addPassword(name, h.username ?? '', f.password)
        h.identity_id = ident.id
      }
      const saved = await be.hosts.saveHost(h)
      if (f.telnet_on && f.telnet_password) await be.hosts.setTelnetPassword(saved.id, f.telnet_password)
      if (f.ftp_on && f.ftp_password) await be.hosts.setFtpPassword(saved.id, f.ftp_password)
      return saved
    } catch (e) {
      setErr(errMsg(e))
      return null
    } finally {
      if (alive.current) setBusy(false)
    }
  }

  const save = async () => { const h = await persist(); if (h) onSaved(h) }
  const connect = async () => {
    const h = await persist()
    if (!h) return
    onSaved(h)
    onConnect?.(h)
  }

  const runTest = async () => {
    const h = draft()
    if (!h) return
    const key = `test-${Date.now().toString(36)}-${++testSeq}`
    setTest({state: 'running'})
    const off1 = be.on(`term:hostkey:${key}`, (p: HostKeyPrompt) => setPrompt({key, p}))
    const off2 = be.on(`term:auth:${key}`, (c: AuthChallenge) => setAuth({key, c}))
    const off = () => { off1(); off2() }
    try {
      const result = await be.session.testConnection(h, f.identity_id ? '' : f.password, key)
      if (alive.current) setTest({state: 'done', result})
    } catch (e) {
      if (alive.current) setTest({state: 'failed', message: errMsg(e)})
    } finally {
      off()
      if (alive.current) { setPrompt(null); setAuth(null) }
    }
  }

  const answer = (trust: boolean) => {
    if (!prompt) return
    void be.session.trustHostKey(prompt.key, trust ? prompt.p.fingerprint : '').catch(e => setErr(errMsg(e)))
    setPrompt(null)
  }

  return (
    <SidePanel title={existing ? t('Edit {name}', {name: host!.label}) : t('New host')} onClose={onClose}
      footer={
        <>
          <button onClick={runTest} disabled={test.state === 'running' || busy}>{test.state === 'running' ? t('Testing…') : t('Test connection')}</button>
          <span className="spacer" />
          <button onClick={save} disabled={busy}>{t('Save')}</button>
          {onConnect && <button className="primary" onClick={connect} disabled={busy}>{t('Connect')}</button>}
        </>
      }>
      <Section title={t('Address')}>
        <div className="addr-row">
          <span className="tile env-none"><IconServer size={20} /></span>
          <input aria-label={t('Hostname')} value={f.hostname} onChange={set('hostname')} placeholder={t('IP or Hostname')} autoFocus />
        </div>
      </Section>

      <Section title={t('General')}>
        <Field label={t('Label')}><input aria-label={t('Label')} value={f.label} onChange={set('label')} placeholder={t('Defaults to the address')} /></Field>
        <Field label={t('Parent group')}>
          <Select label={t('Group')} value={f.group_id} onChange={v => setF(x => ({...x, group_id: v}))}
            options={[{value: '', label: t('None')}, ...groups.map(g => ({value: g.id, label: g.name}))]} />
        </Field>
        <Field label={t('Environment (colour)')}>
          <Select label={t('Environment')} value={f.env} onChange={v => setF(x => ({...x, env: v as typeof f.env}))}
            options={[{value: '', label: t('None')}, {value: 'dev', label: 'dev', hint: t('development')}, {value: 'staging', label: 'staging', hint: t('pre-production')}, {value: 'prod', label: 'prod', hint: t('production: shown in red')}]} />
        </Field>
        <Field label={t('Tags')}><input aria-label={t('Tags')} value={f.tags} onChange={set('tags')} placeholder={t('Comma separated')} /></Field>
      </Section>

      <Section>
        <div className="inline-field"><span>{t('SSH on')}</span><input aria-label={t('Port')} className="port" value={f.port} onChange={set('port')} /><span>{t('port')}</span></div>
      </Section>

      {!f.telnet_on ? (
        <button type="button" className="add-wide" onClick={() => setF(x => ({...x, telnet_on: true, telnet_port: x.telnet_port || '23'}))}><IconPlus size={17} /> {t('Add Telnet')}</button>
      ) : (
        <Section>
          <div className="inline-field"><span>{t('Telnet on')}</span><input aria-label={t('Telnet port')} className="port" placeholder="23" value={f.telnet_port} onChange={set('telnet_port')} inputMode="numeric" /><span>{t('port')}</span>
            <button type="button" className="link remove-telnet" onClick={() => setF(x => ({...x, telnet_on: false, telnet_port: '', telnet_username: '', telnet_password: ''}))}>{t('Remove')}</button></div>
          <div className="muted telnet-warning" role="note">{t('Telnet is not encrypted: everything you type, the password included, crosses the network in clear text. Use it only on a network you trust, for devices that offer nothing else.')}</div>
          <h4 className="card-sub">{t('Credentials')}</h4>
          <input aria-label={t('Telnet username')} value={f.telnet_username} onChange={set('telnet_username')} placeholder={t('Username')} autoComplete="off" />
          <div className="pw-row">
            <input aria-label={t('Telnet password')} type={showTelnetPw ? 'text' : 'password'} value={f.telnet_password} onChange={set('telnet_password')} autoComplete="new-password"
              placeholder={existing && host?.telnet_enabled ? t('Saved in the vault (type to replace)') : t('Password')} />
            <button type="button" className="eye" aria-label={showTelnetPw ? t('Hide Telnet password') : t('Show Telnet password')} aria-pressed={showTelnetPw} onClick={() => setShowTelnetPw(v => !v)}>{showTelnetPw ? <IconEyeOff size={17} /> : <IconEye size={17} />}</button>
          </div>
          <Select label={t('Telnet encoding')} value={f.telnet_encoding} onChange={v => setF(x => ({...x, telnet_encoding: v}))}
            options={[{value: 'utf-8', label: 'UTF-8'}, {value: 'iso-8859-1', label: 'ISO-8859-1', hint: t('Latin-1, older devices')}]} />
        </Section>
      )}

      {!f.ftp_on ? (
        <button type="button" className="add-wide" onClick={() => setF(x => ({...x, ftp_on: true, ftp_port: x.ftp_port || (x.ftp_security === 'implicit' ? '990' : '21')}))}><IconPlus size={17} /> {t('Add FTP')}</button>
      ) : (
        <Section>
          <div className="inline-field"><span>{t('FTP on')}</span><input aria-label={t('FTP port')} className="port" placeholder="21" value={f.ftp_port} onChange={set('ftp_port')} inputMode="numeric" /><span>{t('port')}</span>
            <button type="button" className="link remove-ftp" onClick={() => setF(x => ({...x, ftp_on: false, ftp_port: '', ftp_username: '', ftp_password: ''}))}>{t('Remove')}</button></div>
          <Select label={t('FTP security')} value={f.ftp_security} onChange={v => setF(x => ({...x, ftp_security: v, ftp_port: v === 'implicit' && x.ftp_port === '21' ? '990' : v !== 'implicit' && x.ftp_port === '990' ? '21' : x.ftp_port}))}
            options={[{value: 'explicit', label: 'FTPS (explicit TLS)', hint: t('Encrypted: starts plain, then switches to TLS (port 21)')}, {value: 'implicit', label: 'FTPS (implicit TLS)', hint: t('Encrypted from the first byte (port 990)')}, {value: 'plain', label: t('Plain FTP'), hint: t('Not encrypted')}]} />
          {f.ftp_security === 'plain' && <div className="muted telnet-warning" role="note">{t('Plain FTP is not encrypted: the password and every file cross the network in clear text. Use it only on a network you trust.')}</div>}
          <h4 className="card-sub">{t('Credentials')}</h4>
          <input aria-label={t('FTP username')} value={f.ftp_username} onChange={set('ftp_username')} placeholder={t('Username (empty: the one above, or anonymous)')} autoComplete="off" />
          <div className="pw-row">
            <input aria-label={t('FTP password')} type={showFtpPw ? 'text' : 'password'} value={f.ftp_password} onChange={set('ftp_password')} autoComplete="new-password"
              placeholder={existing && host?.ftp_enabled ? t('Saved in the vault (type to replace)') : t('Password')} />
            <button type="button" className="eye" aria-label={showFtpPw ? t('Hide FTP password') : t('Show FTP password')} aria-pressed={showFtpPw} onClick={() => setShowFtpPw(v => !v)}>{showFtpPw ? <IconEyeOff size={17} /> : <IconEye size={17} />}</button>
          </div>
        </Section>
      )}

      {!f.rdp_on ? (
        <button type="button" className="add-wide" onClick={() => setF(x => ({...x, rdp_on: true, rdp_port: x.rdp_port || '3389'}))}><IconPlus size={17} /> {t('Add Remote Desktop')}</button>
      ) : (
        <Section>
          <div className="inline-field"><span>{t('Remote Desktop on')}</span><input aria-label={t('Remote Desktop port')} className="port" placeholder="3389" value={f.rdp_port} onChange={set('rdp_port')} inputMode="numeric" /><span>{t('port')}</span>
            <button type="button" className="link remove-rdp" onClick={() => setF(x => ({...x, rdp_on: false, rdp_port: '', rdp_username: ''}))}>{t('Remove')}</button></div>
          <div className="muted" role="note">{t("Vutty starts your computer's own Remote Desktop client. It does not keep an RDP password: the client asks for it.")}</div>
          <input aria-label={t('Remote Desktop username')} value={f.rdp_username} onChange={set('rdp_username')} placeholder={t('Username (empty: the one above)')} autoComplete="off" />
        </Section>
      )}

      <Section title={t('Credentials')}>
        <Field label={t('Username')}><input aria-label={t('Username')} value={f.username} onChange={set('username')} placeholder={t('Username')} autoComplete="off" /></Field>
        <Field label={t('Password')}><div className="pw-row">
          <input aria-label={t('Password')} type={showPw ? 'text' : 'password'} value={f.password} onChange={set('password')} autoComplete="new-password"
            disabled={!!f.identity_id} placeholder={f.identity_id ? t('Using the selected identity') : existing && host?.identity_id ? t('Saved in the vault (type to replace)') : t('Password')} />
          <button type="button" className="eye" aria-label={showPw ? t('Hide password') : t('Show password')} title={showPw ? t('Hide password') : t('Show password')} aria-pressed={showPw} onClick={() => setShowPw(v => !v)}>{showPw ? <IconEyeOff size={17} /> : <IconEye size={17} />}</button>
        </div></Field>
        <Field label={t('Key or saved identity')}>
          <Select label={t('Identity')} value={f.identity_id} onChange={v => setF(x => ({...x, identity_id: v}))}
            options={[{value: '', label: t('None'), hint: t('use the password above or ssh-agent')}, ...identities.map(i => ({value: i.id, label: i.name, hint: i.auth_type === 'key' ? t('SSH key') : i.auth_type === 'password' ? t('saved password') : 'ssh-agent'}))]} />
        </Field>
        {f.password && <div className="muted">{t('The password is stored encrypted in the vault, never in the host list.')}</div>}
      </Section>

      <Section title={t('Terminal theme')}>
        <ThemePicker value={f.terminal_theme} onChange={v => setF(x => ({...x, terminal_theme: v}))} inherited={resolveTerminalTheme(appSetting, appearance)} />
      </Section>

      <Section title={t('AI assistant')}>
        <Field label={t('Assistant on this host')}>
          <Select label={t('AI assistant on this host')} value={f.ai_policy} onChange={v => setF(x => ({...x, ai_policy: v as AiPolicy}))}
            options={[{value: '', label: f.env === 'prod' ? t('Default (off)') : t('Default (on)')}, {value: 'off', label: t('Off')}, {value: 'ask', label: t('Ask first, in the chat')}, {value: 'on', label: t('On')}]} />
        </Field>
        <div className="muted">{t('When on, the text of the terminal can be sent, with secrets hidden, to the AI tool you chose in Settings. The assistant asks you, in the chat, before anything that could change something. Production hosts are off by default and never run commands for the assistant.')}</div>
      </Section>

      <Section title={t('Options')}>
        <Field label={t('Host chaining (jump host)')}>
          <Select label={t('Jump host')} value={f.jump_host_id} onChange={v => setF(x => ({...x, jump_host_id: v}))}
            options={[{value: '', label: t('Direct connection')}, ...hosts.filter(h => h.id !== host?.id).map(h => ({value: h.id, label: t('Via {name}', {name: h.label}), hint: h.hostname}))]} />
        </Field>
        <div className="setting-row">
          <div>
            <b>{t('Forward my keys to this host')}</b>
            <div className="muted">{t('Lets this server use your ssh-agent (or, with no agent running, this host\'s saved key) to sign in to other machines while you are connected, like ssh -A. Nothing is copied to the server, but anyone with root there can use your keys until you disconnect.')}</div>
          </div>
          <Switch label={t('Forward my keys to this host')} checked={f.forward_agent} onChange={v => setF(x => ({...x, forward_agent: v}))} />
        </div>
        {f.forward_agent && (
          <div className="dx-warn" role="note"><IconWarn size={15} /> <span>{f.env === 'prod' ? t('This is a production host.') + ' ' : ''}{t('Turn this on only for a server you trust. If it is only a stop on the way to other servers, a jump host does the same without exposing your keys.')}</span></div>
        )}
        <div className="inline-field"><span>{t('Keepalive')}</span><input aria-label={t('Keepalive')} className="port" value={f.keepalive_sec} onChange={set('keepalive_sec')} /><span>{t('seconds')}</span></div>
      </Section>

      <AuthPrompt inline challenge={auth?.c ?? null} promptKey={auth?.key ?? ''} onDone={() => setAuth(null)} onError={setErr} />

      {prompt && (
        <div className="notice" role="alertdialog" aria-label={t('Host key confirmation')}>
          <b>{t('Unknown host key for {host}', {host: prompt.p.host})}</b>
          <div className="fp" data-testid="fingerprint">{prompt.p.fingerprint}</div>
          <div className="row">
            <button className="primary" onClick={() => answer(true)}>{t('Trust and continue')}</button>
            <button onClick={() => answer(false)}>{t('Cancel')}</button>
          </div>
        </div>
      )}

      {test.state === 'done' && (
        <div className={'test-result ' + (test.result.ok ? 'ok' : 'bad')} role="status" data-testid="test-result">
          <b className="result-title">{test.result.ok ? <IconCheckCircle size={16} /> : <IconXCircle size={16} />} {test.result.ok ? t('Connection successful') : t('Connection failed')}</b>
          <div>{test.result.message}</div>
          {test.result.ok && <div className="muted">{test.result.server_version} · {test.result.latency_ms} ms{test.result.hops > 1 ? ' · ' + tn(test.result.hops, '{n} hop', '{n} hops') : ''}</div>}
        </div>
      )}
      {test.state === 'failed' && <div className="test-result bad" role="status"><b className="result-title"><IconXCircle size={16} /> {t('Cannot test')}</b><div>{test.message}</div></div>}
      {err && <div className="error" role="alert">{err}</div>}
    </SidePanel>
  )
}

export function GroupForm({group, groups, defaultParentId, onClose, onSaved}: {group?: Group; groups: Group[]; /** the group open on the hosts page: a new group is created inside it */ defaultParentId?: string; onClose(): void; onSaved(): void}) {
  const t = useT()
  const be = useBackend()
  const [name, setName] = useState(group?.name ?? '')
  const [parent, setParent] = useState(group?.parent_id || (group ? '' : defaultParentId ?? ''))
  const [color, setColor] = useState(group?.color ?? '')
  const [err, setErr] = useState('')
  const save = async () => {
    if (!name.trim()) return setErr(t('Name is required'))
    try {
      await be.hosts.saveGroup({...(group ?? {}), name: name.trim(), parent_id: parent, color})
      onSaved()
    } catch (e) {
      setErr(errMsg(e))
    }
  }
  return (
    <SidePanel title={group ? t('Edit group') : t('New group')} onClose={onClose}
      footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" onClick={save}>{t('Save')}</button></>}>
      <Section title={t('General')}>
        <input aria-label={t('Group name')} value={name} onChange={e => setName(e.target.value)} placeholder={t('Group name')} autoFocus />
        <Field label={t('Parent group')}>
          <Select label={t('Parent group')} value={parent} onChange={setParent}
            options={[{value: '', label: t('Top level')}, ...groups.filter(g => g.id !== group?.id).map(g => ({value: g.id, label: g.name}))]} />
        </Field>
        <div className="inline-field"><span>{t('Colour')}</span><input aria-label={t('Color')} type="color" className="color" value={color || '#3a82f7'} onChange={e => setColor(e.target.value)} /></div>
      </Section>
      {err && <div className="error" role="alert">{err}</div>}
    </SidePanel>
  )
}

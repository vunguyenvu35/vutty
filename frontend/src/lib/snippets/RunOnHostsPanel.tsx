import {useEffect, useMemo, useRef, useState} from 'react'
import {Modal} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {IconCheckCircle, IconClock, IconXCircle} from '../ui/Icons'
import {OsIcon} from '../ui/OsIcon'
import {useBackend} from '../backend'
import {errMsg} from '../util'
import {fillVariables, HOST_VARIABLES, parseVariables} from './variables'
import type {ExecResult, ExecRun, Host, Snippet} from '../types'

const TIMEOUTS = [{value: '30', label: '30 seconds'}, {value: '60', label: '1 minute'}, {value: '300', label: '5 minutes'}, {value: '600', label: '10 minutes'}]
// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07/g
const plain = (s: string) => s.replace(ANSI, '')
const finished = (r: ExecResult) => r.state === 'ok' || r.state === 'failed' || r.state === 'canceled'

/**
 * One command on several hosts. Choose the hosts, write the command (or start from a snippet and fill in its blanks), run it, and
 * read what each host printed. Every host gets its own connection and no terminal, five at a time; nothing is asked on the way, so
 * a host whose key you never trusted, or that asks for a code, fails with a message that says so.
 */
export function RunOnHostsPanel({hosts, snippets, snippet, onClose}: {hosts: Host[]; snippets: Snippet[]; snippet?: Snippet; onClose(): void}) {
  const be = useBackend()
  const confirm = useConfirm()
  const toast = useToast()
  const [command, setCommand] = useState(snippet?.script ?? '')
  const [values, setValues] = useState<Record<string, string>>({})
  const [timeout, setTimeoutSecs] = useState('60')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [filter, setFilter] = useState('')
  const [err, setErr] = useState('')
  const [run, setRun] = useState<ExecRun | null>(null)
  const [open, setOpen] = useState<Set<string>>(new Set())
  const runId = useRef('')
  const off = useRef<() => void>(() => {})

  const vars = useMemo(() => parseVariables(command, true), [command])
  const shown = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    return hosts
      .filter(h => !needle || `${h.label} ${h.hostname} ${h.username} ${(h.tags ?? []).join(' ')} ${h.env ?? ''}`.toLowerCase().includes(needle))
      .sort((a, b) => a.label.localeCompare(b.label, undefined, {numeric: true, sensitivity: 'base'}))
  }, [hosts, filter])
  const allShown = shown.length > 0 && shown.every(h => picked.has(h.id))
  const toggle = (id: string) => setPicked(s => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const toggleShown = () => setPicked(s => { const n = new Set(s); for (const h of shown) { if (allShown) n.delete(h.id); else n.add(h.id) } return n })

  // a run that is left behind (the panel closed) is stopped and forgotten: nothing keeps running or keeps its output unseen
  useEffect(() => () => {
    off.current()
    if (runId.current) { void be.exec.cancel(runId.current).catch(() => {}); void be.exec.forget(runId.current).catch(() => {}) }
  }, [be])

  const start = async () => {
    setErr('')
    const chosen = hosts.filter(h => picked.has(h.id))
    const prod = chosen.filter(h => h.env === 'prod')
    if (prod.length > 0) {
      const names = prod.slice(0, 5).map(h => h.label).join(', ') + (prod.length > 5 ? ', …' : '')
      if (!await confirm(`${prod.length} of the ${chosen.length} hosts ${chosen.length === 1 ? 'is' : 'are'} production (${names}). Run the command on ${prod.length === chosen.length ? 'them' : 'all of them'}?`, {confirmLabel: 'Run', danger: true})) return
    }
    try {
      const id = await be.exec.run({host_ids: chosen.map(h => h.id), command: fillVariables(command, values, HOST_VARIABLES), timeout_sec: Number(timeout)})
      runId.current = id
      off.current()
      off.current = be.on(`exec:update:${id}`, (r: ExecRun) => setRun({...r, results: r.results ?? []}))
      setRun(await be.exec.get(id)) // what happened before the first event arrived
      setOpen(new Set())
    } catch (e) {
      setErr(errMsg(e))
    }
  }

  const stop = () => { if (runId.current) void be.exec.cancel(runId.current).catch(e => setErr(errMsg(e))) }
  const back = () => {
    off.current()
    if (runId.current) { void be.exec.cancel(runId.current).catch(() => {}); void be.exec.forget(runId.current).catch(() => {}) }
    runId.current = ''
    setRun(null)
  }
  const copyAll = async () => {
    if (!run) return
    const text = run.results.map(r => `== ${r.label} (${r.state}${r.state === 'failed' && r.exit_code >= 0 ? `, exit ${r.exit_code}` : ''}) ==\n${plain(r.output)}${r.error ? `\n[${r.error}]` : ''}`).join('\n\n')
    try { await be.system.writeClipboard(text); toast.info('Output copied') } catch (e) { toast.error(errMsg(e)) }
  }

  const chosenCount = picked.size
  const body = !run ? (
    <div className="xr-form">
      {snippets.length > 0 && (
        <div className="xr-field">
          <span>Start from a snippet</span>
          <Select label="Start from a snippet" value="" onChange={id => { const s = snippets.find(x => x.id === id); if (s) { setCommand(s.script); setValues({}) } }}
            options={[{value: '', label: 'Choose…'}, ...snippets.map(s => ({value: s.id, label: s.label, hint: s.description || s.script.split('\n')[0]}))]} />
        </div>
      )}
      <label className="xr-field">
        <span>Command</span>
        <textarea aria-label="Command" className="script-area" rows={5} spellCheck={false} value={command} onChange={e => setCommand(e.target.value)}
          placeholder={'systemctl is-active nginx\n# {{host}}, {{hostname}} and {{user}} become each host’s own'} />
      </label>
      {vars.length > 0 && (
        <div className="xr-vars">
          {vars.map(v => (
            <label key={v.name} className="xr-field">
              <span>{v.name}{v.def ? ` (default: ${v.def})` : ''}</span>
              <input aria-label={v.name} spellCheck={false} placeholder={v.def} autoComplete="off" value={values[v.name] ?? ''} onChange={e => setValues(x => ({...x, [v.name]: e.target.value}))} />
            </label>
          ))}
        </div>
      )}
      <div className="xr-field">
        <span>Give up on a host after</span>
        <Select label="Timeout" value={timeout} onChange={setTimeoutSecs} options={TIMEOUTS} />
      </div>
      <div className="xr-hosts-head">
        <b>Hosts</b>
        <input aria-label="Filter hosts" placeholder="Search hosts" value={filter} onChange={e => setFilter(e.target.value)} />
        <label className="xr-check"><input type="checkbox" aria-label="Select all shown" checked={allShown} onChange={toggleShown} /> All shown</label>
      </div>
      <div className="xr-hosts" role="group" aria-label="Hosts">
        {shown.map(h => (
          <label key={h.id} className={'xr-host' + (picked.has(h.id) ? ' on' : '')}>
            <input type="checkbox" aria-label={h.label} checked={picked.has(h.id)} onChange={() => toggle(h.id)} />
            <OsIcon os={h.os} size={16} />
            <span className="xr-name">{h.label}</span>
            <small className="muted">{h.username ? h.username + '@' : ''}{h.hostname}</small>
            {h.env && <i className={'env-pip-inline env-' + h.env} title={h.env}>{h.env}</i>}
          </label>
        ))}
        {shown.length === 0 && <div className="muted" style={{padding: 12}}>{hosts.length ? 'No host matches.' : 'No hosts yet.'}</div>}
      </div>
      <div className="muted xr-note">Runs without a terminal, five hosts at a time. A host whose key you never trusted, or that asks for a code, fails: open it once in a terminal tab first.</div>
      {err && <div className="error" role="alert">{err}</div>}
    </div>
  ) : (
    <div className="xr-run">
      {(() => {
        const done = run.results.filter(finished).length
        const failed = run.results.filter(r => r.state === 'failed').length
        return (
          <div className="xr-summary" role="status">
            {run.done ? 'Finished' : 'Running'}: {done} of {run.results.length} hosts done{failed > 0 ? `, ${failed} failed` : ''}.
          </div>
        )
      })()}
      <div className="xr-results" role="list" aria-label="Results">
        {run.results.map(r => {
          const isOpen = open.has(r.host_id) || (r.state === 'failed' && !open.has('-' + r.host_id))
          return (
            <div key={r.host_id} className={'xr-result ' + r.state} role="listitem" aria-label={`${r.label}: ${r.state}`}>
              <button className="xr-line" aria-expanded={isOpen} onClick={() => setOpen(s => { const n = new Set(s); if (isOpen) { n.delete(r.host_id); n.add('-' + r.host_id) } else { n.add(r.host_id); n.delete('-' + r.host_id) } return n })}>
                <span className="xr-state" aria-hidden="true">
                  {r.state === 'ok' ? <IconCheckCircle size={16} /> : r.state === 'failed' ? <IconXCircle size={16} /> : r.state === 'running' ? <i className="xr-spin" /> : <IconClock size={16} />}
                </span>
                <span className="xr-name">{r.label}</span>
                {r.env && <i className={'env-pip-inline env-' + r.env}>{r.env}</i>}
                <span className="muted xr-meta">
                  {r.state === 'ok' ? `exit 0 · ${(r.duration_ms / 1000).toFixed(1)}s` : r.state === 'failed' ? (r.error || 'failed') : r.state === 'canceled' ? 'cancelled' : r.state}
                </span>
              </button>
              {isOpen && finished(r) && (
                <pre className="xr-out" aria-label={`Output of ${r.label}`}>{plain(r.output) || (r.state === 'ok' ? '(no output)' : '')}{r.truncated ? '\n… (output cut at 64 KB)' : ''}</pre>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )

  const footer = !run ? (
    <>
      <span className="muted xr-count">{chosenCount} {chosenCount === 1 ? 'host' : 'hosts'} chosen</span>
      <button onClick={onClose}>Close</button>
      <button className="primary" disabled={!command.trim() || chosenCount === 0} onClick={() => void start()}>Run on {chosenCount || ''} {chosenCount === 1 ? 'host' : 'hosts'}</button>
    </>
  ) : (
    <>
      {!run.done && <button onClick={stop}>Cancel the run</button>}
      <button onClick={() => void copyAll()} disabled={!run.results.some(finished)}>Copy all output</button>
      <button onClick={back} disabled={false}>{run.done ? 'Run again…' : 'Back'}</button>
      <button className="primary" onClick={onClose}>Close</button>
    </>
  )

  return <Modal title="Run on hosts" wide onClose={onClose} footer={footer}>{body}</Modal>
}

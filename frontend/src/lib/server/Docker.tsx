import {createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode} from 'react'
import {createPortal} from 'react-dom'
import {useMenuAnchor} from './KubernetesShared'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {Field, Modal} from '../ui/Modal'
import {IconChevron} from '../ui/Icons'
import {errMsg} from '../util'
import {actionQuestion, Err, RefreshButton, stackQuestion, useFolded, useLoad, type ServerAction, type StackAction} from './shared'
import type {DockerInspect, DockerObjects, DockerStat, Host, ServerContainer, ServerContainers} from '../types'
import type {LogSource} from './ServerPanel'

const POLL_MS = 5000
type View = 'containers' | 'images' | 'volumes' | 'networks'

/** where the buttons of the shown view go: at the right of the row of the view switch, not in a row of their own */
const BarSlot = createContext<HTMLElement | null>(null)
function InBar({children}: {children: ReactNode}) {
  const el = useContext(BarSlot)
  return el ? createPortal(children, el) : null
}

/** The Docker section: containers (with a shell, their load and what is inside them), and the images, volumes and networks. */
export function Docker({sid, host, onLogs, onShell}: {sid: string; host: Host; onLogs(s: LogSource): void; onShell?(host: Host, command: string, title: string): void}) {
  const t = useT()
  const [view, setView] = useState<View>('containers')
  const [slot, setSlot] = useState<HTMLElement | null>(null)
  const views: [View, string][] = [['containers', t('Containers')], ['images', t('Images')], ['volumes', t('Volumes')], ['networks', t('Networks')]]
  return (
    <div className="sp-view">
      <div className="sp-bar">
        <div className="segmented" role="tablist" aria-label={t('Docker')}>
          {views.map(([k, label]) => <button key={k} role="tab" aria-selected={view === k} className={view === k ? 'on' : ''} onClick={() => setView(k)}>{label}</button>)}
        </div>
        <span className="sp-slot" ref={setSlot} />
      </div>
      <BarSlot.Provider value={slot}>
        {view === 'containers' ? <Containers sid={sid} host={host} onLogs={onLogs} onShell={onShell} /> : <Objects sid={sid} view={view} />}
      </BarSlot.Provider>
    </div>
  )
}

function Containers({sid, host, onLogs, onShell}: {sid: string; host: Host; onLogs(s: LogSource): void; onShell?(host: Host, command: string, title: string): void}) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const [look, setLook] = useState('')
  const [isFolded, fold] = useFolded() // the groups that are put away
  const load = useCallback(() => be.server.containers(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerContainers>(load, null)
  // the load of the running containers, read again every few seconds; a server that cannot answer just shows no figures
  const loadStats = useCallback(() => be.server.dockerStats(sid).catch((): DockerStat[] => []), [be, sid])
  const stats = useLoad<DockerStat[]>(loadStats, POLL_MS).data
  const statOf = useMemo(() => new Map((stats ?? []).map(s => [s.name, s])), [stats])
  const act = async (name: string, action: ServerAction) => {
    const q = actionQuestion(t, 'container', action, name)
    if (!(await confirm(q.message, {confirmLabel: q.label, danger: action !== 'start'}))) return
    setWorking(name)
    try { await be.server.containerAction(sid, name, action); toast.info(t('{name}: done', {name})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  const stack = async (project: string, action: StackAction) => {
    const q = stackQuestion(t, action, project)
    if (!(await confirm(q.message, {confirmLabel: q.label, danger: action === 'stop' || action === 'down'}))) return
    setWorking('stack:' + project)
    try { await be.server.stackAction(sid, project, action); toast.info(t('{name}: done', {name: project})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  const shell = async (c: ServerContainer) => {
    try { onShell?.(host, await be.server.dockerShellCommand(sid, c.name), `${host.label} · ${c.name}`) } catch (e) { toast.error(errMsg(e)) }
  }
  // the containers of a Compose project together, the others after them
  const groups = useMemo(() => {
    const by = new Map<string, ServerContainer[]>()
    for (const c of data?.containers ?? []) by.set(c.project, [...(by.get(c.project) ?? []), c])
    const projects = [...by.keys()].filter(p => p !== '').sort()
    return [...projects.map(p => ({project: p, items: by.get(p)!})), ...(by.has('') ? [{project: '', items: by.get('')!}] : [])]
  }, [data])
  const row = (c: ServerContainer) => {
    const s = statOf.get(c.name)
    const b = working === c.name
    return (
      <tr key={c.id}>
        <td><b>{c.name}</b></td>
        <td className="m cut" title={c.image}>{c.image}</td>
        <td>{c.state === 'running' ? <span className="sp-pill ok" title={c.status}>{t('Running')}</span> : <span className="sp-pill" title={c.status}>{t('Stopped')}</span>}</td>
        <td className="n m">{s ? s.cpu.toFixed(1) + '%' : '–'}</td>
        <td className="n m" title={s ? s.mem_pct.toFixed(1) + '%' : undefined}>{s ? s.mem_usage : '–'}</td>
        <td className="m">{c.ports || '–'}</td>
        <td><div className="sp-acts">
          {c.state === 'running'
            ? (onShell ? <button onClick={() => void shell(c)}>{t('Shell')}</button> : <button onClick={() => onLogs({kind: 'container', name: c.name})}>{t('Logs')}</button>)
            : <button className="primary" aria-busy={b} disabled={b} onClick={() => void act(c.name, 'start')}>{t('Start')}</button>}
          <RowMenu name={c.name} items={[
            ...(c.state === 'running' ? [{label: t('Restart'), disabled: b, run: () => void act(c.name, 'restart')}, {label: t('Stop'), danger: true, disabled: b, run: () => void act(c.name, 'stop')}] : []),
            ...(c.state === 'running' && !onShell ? [] : [{label: t('Logs'), run: () => onLogs({kind: 'container', name: c.name})}]),
            {label: t('Details'), run: () => setLook(c.name)},
          ]} />
        </div></td>
      </tr>
    )
  }
  return (
    <>
      <InBar><RefreshButton busy={busy} onReload={reload} /></InBar>
      <Err error={error} />
      {data && !data.installed && <div className="sp-empty">{t('Docker is not installed on this server.')}</div>}
      {data?.installed && data.containers.length === 0 && <div className="sp-empty">{t('No containers.')}</div>}
      {data?.installed && groups.map(g => (
        <section key={g.project || '-'} className="sp-card sp-wide" aria-label={g.project || t('Other containers')}>
          <div className="sp-card-head">
            <button className="sp-card-fold" aria-expanded={!isFolded('docker:' + g.project)} aria-label={g.project ? t('Stack {name}', {name: g.project}) : t('Other containers')} onClick={() => fold('docker:' + g.project)}>
              <span className={'k8s-chev' + (isFolded('docker:' + g.project) ? '' : ' open')}><IconChevron size={12} /></span><h3>{g.project ? t('Stack {name}', {name: g.project}) : t('Other containers')}</h3>
            </button>
            {g.project && (
              <div className="sp-acts">
                <span className="sp-note">{t('{n} running', {n: g.items.filter(c => c.state === 'running').length})} / {tn(g.items.length, '{n} container', '{n} containers')}</span>
                <button className="primary" aria-busy={working === 'stack:' + g.project} disabled={working === 'stack:' + g.project} onClick={() => void stack(g.project, 'update')}>{t('Update')}</button>
                <button aria-busy={working === 'stack:' + g.project} disabled={working === 'stack:' + g.project} onClick={() => void stack(g.project, 'restart')}>{t('Restart')}</button>
                {g.items.some(c => c.state === 'running')
                  ? <button className="danger" aria-busy={working === 'stack:' + g.project} disabled={working === 'stack:' + g.project} onClick={() => void stack(g.project, 'stop')}>{t('Stop')}</button>
                  : <button aria-busy={working === 'stack:' + g.project} disabled={working === 'stack:' + g.project} onClick={() => void stack(g.project, 'start')}>{t('Start')}</button>}
                <RowMenu name={g.project} items={[
                  {label: t('Up'), disabled: working === 'stack:' + g.project, run: () => void stack(g.project, 'up')},
                  {label: t('Logs'), run: () => onLogs({kind: 'stack', name: g.project})},
                  {label: t('Down'), danger: true, disabled: working === 'stack:' + g.project, run: () => void stack(g.project, 'down')},
                ]} />
              </div>
            )}
          </div>
          {!isFolded('docker:' + g.project) && <div className="sp-table"><table>
            <thead><tr><th>{t('Container')}</th><th>{t('Image')}</th><th>{t('Status')}</th><th className="n">{t('CPU')}</th><th className="n">{t('Memory')}</th><th>{t('Ports')}</th><th /></tr></thead>
            <tbody>{g.items.map(row)}</tbody>
          </table></div>}
        </section>
      ))}
      {look && <Inspect sid={sid} name={look} onClose={() => setLook('')} />}
    </>
  )
}

/** the ⋮ of a row: the actions that are not the main one, in a small menu */
export function RowMenu({name, items}: {name: string; items: {label: string; danger?: boolean; disabled?: boolean; run(): void}[]}) {
  const t = useT()
  const {btn, menu, at, setAt, toggle, style} = useMenuAnchor(180)
  useEffect(() => {
    if (!at) return
    const away = (e: MouseEvent) => { if (!(e.target as Element).closest?.('.k8s-menu') && !btn.current?.contains(e.target as Node)) setAt(null) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setAt(null) }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc) }
  }, [at]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <button ref={btn} className="row-more" aria-haspopup="menu" aria-expanded={!!at} aria-label={t('More actions for {name}', {name})} title={t('More actions')} onClick={toggle}>⋮</button>
      {at && createPortal(
        <div ref={menu} className="k8s-menu row-menu" role="menu" aria-label={t('More actions for {name}', {name})} style={style}>
          {items.map(i => <button key={i.label} role="menuitem" className={'k8s-mi' + (i.danger ? ' danger' : '')} disabled={i.disabled} onClick={() => { setAt(null); i.run() }}>{i.label}</button>)}
        </div>, document.body)}
    </>
  )
}

/** what is inside a container; the value of an environment variable stays hidden until asked (it may be a password) */
function Inspect({sid, name, onClose}: {sid: string; name: string; onClose(): void}) {
  const be = useBackend()
  const t = useT()
  const [shown, setShown] = useState<string[]>([])
  const load = useCallback(() => be.server.dockerInspect(sid, name), [be, sid, name])
  const {data: d, error} = useLoad<DockerInspect>(load, null)
  return (
    <Modal wide title={t('Container {name}', {name})} onClose={onClose} footer={<button onClick={onClose}>{t('Close')}</button>}>
      <Err error={error} />
      {!d && !error && <div className="muted">{t('Loading…')}</div>}
      {d && (
        <div className="sp-inspect">
          <table><tbody>
            <tr><td>{t('Image')}</td><td className="m">{d.image}</td></tr>
            <tr><td>{t('State')}</td><td className="m">{d.state}</td></tr>
            <tr><td>{t('Created')}</td><td className="m">{d.created}</td></tr>
            <tr><td>{t('Command')}</td><td className="m">{d.command || '–'}</td></tr>
            <tr><td>{t('Restart policy')}</td><td className="m">{d.restart}</td></tr>
            <tr><td>{t('Ports')}</td><td className="m">{d.ports.join(', ') || '–'}</td></tr>
            <tr><td>{t('Networks')}</td><td className="m">{d.networks.join(', ') || '–'}</td></tr>
          </tbody></table>
          <h4>{t('Mounts')}</h4>
          {d.mounts.length === 0 ? <div className="sp-note">{t('None.')}</div> : (
            <table><tbody>{d.mounts.map(m => <tr key={m.dest}><td className="m">{m.source}</td><td className="m">→ {m.dest}</td><td>{m.type}{m.rw ? '' : ' · ' + t('read only')}</td></tr>)}</tbody></table>
          )}
          <h4>{t('Environment')}</h4>
          {d.env.length === 0 ? <div className="sp-note">{t('None.')}</div> : (
            <table><tbody>{d.env.map(e => (
              <tr key={e.key}>
                <td className="m">{e.key}</td>
                <td className="m">{shown.includes(e.key) ? e.value : '••••••••'}</td>
                <td><button aria-label={(shown.includes(e.key) ? t('Hide') : t('Show')) + ' ' + e.key} onClick={() => setShown(s => (s.includes(e.key) ? s.filter(k => k !== e.key) : [...s, e.key]))}>{shown.includes(e.key) ? t('Hide') : t('Show')}</button></td>
              </tr>
            ))}</tbody></table>
          )}
        </div>
      )}
    </Modal>
  )
}

/** Images, Volumes and Networks: what Docker keeps besides the containers. Removing asks first and Docker refuses what is in use. */
function Objects({sid, view}: {sid: string; view: Exclude<View, 'containers'>}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.dockerObjects(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<DockerObjects>(load, null)
  const remove = async (kind: 'image' | 'volume', id: string, label: string) => {
    const msg = kind === 'image' ? t('Delete the image {name}?', {name: label}) : t('Delete the volume {name}? The data in it is lost.', {name: label})
    if (!(await confirm(msg, {confirmLabel: t('Delete'), danger: true}))) return
    setWorking(id)
    try { await be.server.dockerRemove(sid, kind, id); toast.info(t('{name}: done', {name: label})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  const [pulling, setPulling] = useState(false)
  const [out, setOut] = useState<{ok: boolean; text: string} | null>(null)
  const pull = async (image: string) => {
    setWorking('pull:' + image); setOut(null)
    try { setOut({ok: true, text: await be.server.dockerPull(sid, image)}); toast.info(t('{name}: done', {name: image})) } catch (e) { setOut({ok: false, text: errMsg(e)}) }
    setWorking(''); reload()
  }
  const pullAgain = async (image: string) => {
    if (!(await confirm(t('Download the newest {name}? Containers keep running the old image until they are recreated.', {name: image}), {confirmLabel: t('Pull')}))) return
    await pull(image)
  }
  const inUse = <span className="sp-pill ok">{t('In use')}</span>
  return (
    <>
      <InBar>{view === 'images' && <button onClick={() => setPulling(true)}>{t('Pull image')}</button>}<RefreshButton busy={busy} onReload={reload} /></InBar>
      <Err error={error} />
      {out && <pre className={'sp-pre ' + (out.ok ? '' : 'bad')} role="status" aria-label={t('Result')}>{out.text}</pre>}
      {pulling && <PullForm onPull={async img => { setPulling(false); await pull(img) }} onClose={() => setPulling(false)} />}
      {data && view === 'images' && (data.images.length === 0 ? <div className="sp-empty">{t('No images.')}</div> : (
        <section className="sp-card sp-wide"><div className="sp-table"><table>
          <thead><tr><th>{t('Image')}</th><th>{t('Size')}</th><th>{t('Created')}</th><th /></tr></thead>
          <tbody>{data.images.map(i => (
            <tr key={i.id}>
              <td><b>{i.name}</b><div className="sp-note m">{i.id}</div></td><td className="m">{i.size}</td><td className="m">{i.created}</td>
              <td><div className="sp-acts">{i.name !== '<none>' && <button aria-busy={working === 'pull:' + i.name} disabled={working !== ''} aria-label={t('Pull {name} again', {name: i.name})} onClick={() => void pullAgain(i.name)}>{t('Pull')}</button>}{i.in_use ? inUse : <button className="danger" aria-busy={working === i.id} disabled={working === i.id} onClick={() => void remove('image', i.id, i.name === '<none>' ? i.id : i.name)}>{t('Delete')}</button>}</div></td>
            </tr>
          ))}</tbody>
        </table></div></section>
      ))}
      {data && view === 'volumes' && (data.volumes.length === 0 ? <div className="sp-empty">{t('No volumes.')}</div> : (
        <section className="sp-card sp-wide"><div className="sp-table"><table>
          <thead><tr><th>{t('Volume')}</th><th>{t('Driver')}</th><th /></tr></thead>
          <tbody>{data.volumes.map(v => (
            <tr key={v.name}>
              <td><b>{v.name}</b></td><td className="m">{v.driver}</td>
              <td><div className="sp-acts">{v.in_use ? inUse : <button className="danger" aria-busy={working === v.name} disabled={working === v.name} onClick={() => void remove('volume', v.name, v.name)}>{t('Delete')}</button>}</div></td>
            </tr>
          ))}</tbody>
        </table></div></section>
      ))}
      {data && view === 'networks' && (data.networks.length === 0 ? <div className="sp-empty">{t('No networks.')}</div> : (
        <section className="sp-card sp-wide"><div className="sp-table"><table>
          <thead><tr><th>{t('Network')}</th><th>{t('Driver')}</th><th>{t('Scope')}</th></tr></thead>
          <tbody>{data.networks.map(n => <tr key={n.id}><td><b>{n.name}</b></td><td className="m">{n.driver}</td><td className="m">{n.scope}</td></tr>)}</tbody>
        </table></div></section>
      ))}
    </>
  )
}

/** Ask for the name of an image to download. */
function PullForm({onPull, onClose}: {onPull(image: string): Promise<void>; onClose(): void}) {
  const t = useT()
  const [image, setImage] = useState('')
  const ok = /^[a-z0-9][a-z0-9._/:@-]{0,254}$/.test(image.trim())
  return (
    <Modal title={t('Pull image')} onClose={onClose} footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!ok} onClick={() => void onPull(image.trim())}>{t('Pull')}</button></>}>
      <Field label={t('Image')}><input value={image} spellCheck={false} onChange={e => setImage(e.target.value)} placeholder="nginx:1.27" /></Field>
      <div className="sp-note">{t('A name in lower case, with a tag if you want one: nginx, nginx:1.27, ghcr.io/org/app:v1.')}</div>
    </Modal>
  )
}

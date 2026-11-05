import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {IconCheck} from '../ui/Icons'
import {errMsg} from '../util'
import type {Host, K8sCheck, K8sContexts, K8sDetect, K8sFailKind} from '../types'

/** what the connect flow hands over when it is done */
export interface K8sSession {detect: K8sDetect; contexts: K8sContexts; ctx: string; check: K8sCheck; rights: Record<string, boolean>}

const rememberKey = (hostId: string) => 'vutty.k8s.context.' + hostId
export const remembered = (hostId: string): string => { try { return localStorage.getItem(rememberKey(hostId)) ?? '' } catch { return '' } }

// the namespaces a person added by hand (a login that may not list the namespaces of the cluster has no list to choose from): newest first
const nsKey = (hostId: string, ctx: string) => 'vutty.k8s.namespaces.' + hostId + '|' + ctx
export const savedNamespaces = (hostId: string, ctx: string): string[] => {
  try { const v: unknown = JSON.parse(localStorage.getItem(nsKey(hostId, ctx)) ?? '[]'); return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [] } catch { return [] }
}
export const saveNamespaces = (hostId: string, ctx: string, list: string[]) => {
  try { if (list.length) localStorage.setItem(nsKey(hostId, ctx), JSON.stringify(list)); else localStorage.removeItem(nsKey(hostId, ctx)) } catch { /* not remembered */ }
}
// a host that had no kubectl the last time: the tab says so at once while it looks again (found now: it goes on; still not: it stays)
const noKubectlKey = (hostId: string) => 'vutty.k8s.nokubectl.' + hostId
export const noKubectlBefore = (hostId: string): K8sDetect | null => {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(noKubectlKey(hostId)) ?? 'null')
    const tried = v && typeof v === 'object' && Array.isArray((v as {tried?: unknown}).tried) ? (v as {tried: unknown[]}).tried.filter((x): x is string => typeof x === 'string') : null
    return tried ? {found: false, cmd: '', path: '', client_version: '', tried} : null
  } catch { return null }
}
const rememberNoKubectl = (hostId: string, d: K8sDetect | null) => {
  try { if (d) localStorage.setItem(noKubectlKey(hostId), JSON.stringify({tried: d.tried})); else localStorage.removeItem(noKubectlKey(hostId)) } catch { /* not remembered */ }
}

/** how long a step may take before the list of steps is drawn: an answer that comes sooner is shown by itself, without a card that flashes and goes */
const STEPS_AFTER_MS = 350

export const remember = (hostId: string, ctx: string) => { try { if (ctx) localStorage.setItem(rememberKey(hostId), ctx); else localStorage.removeItem(rememberKey(hostId)) } catch { /* not remembered */ } }

type Phase =
  | {k: 'detect'}
  | {k: 'contexts'}
  | {k: 'nokubectl'; d: K8sDetect}
  | {k: 'kubeconfig'; c: K8sContexts}
  | {k: 'pick'; c: K8sContexts}
  | {k: 'check'; ctx: string}
  | {k: 'fail'; ctx: string; check: K8sCheck}
  | {k: 'access'; ctx: string; check: K8sCheck; rights: Record<string, boolean>}
  | {k: 'error'; message: string}

/**
 * The way in, one step after another (screens 3a to 3e): kubectl on the host, the kubeconfig and its contexts, the cluster
 * that answers, and what this login may do. A step that fails says what it tried and what to do, with the raw message in grey.
 * `again` starts at the check of a context (a context chosen in the bar of the tab).
 * A cluster opened with a kubeconfig (`detect.direct`) has no kubectl step: its kubeconfig is read from the vault, and a login
 * program that the kubeconfig runs to sign in has to be approved here (once) before it may run.
 */
export function KubernetesConnect({sid, host, again, clusterId, onReopen, onAccess, onReady}: {sid: string; host: Host; again: {detect: K8sDetect; contexts: K8sContexts; ctx: string} | null; clusterId?: string; onReopen?(): void; onAccess?(): void; onReady(s: K8sSession): void}) {
  const be = useBackend()
  const t = useT()
  const seen = useRef(again || clusterId ? null : noKubectlBefore(host.id))
  const [phase, setPhase] = useState<Phase>(() => (again ? {k: 'check', ctx: again.ctx} : seen.current ? {k: 'nokubectl', d: seen.current} : {k: 'detect'}))
  const [attempt, setAttempt] = useState(0)
  const [choice, setChoice] = useState('')
  const [keep, setKeep] = useState(true)
  const known = useRef<{detect: K8sDetect; contexts: K8sContexts} | null>(again ? {detect: again.detect, contexts: again.contexts} : null)
  const alive = useRef(true)
  const direct = useRef(!!again) // a context chosen in the bar, or the one remembered for this host: no rights page, it was seen before
  useEffect(() => { alive.current = true; return () => { alive.current = false } }, [])

  const check = useCallback(async (ctx: string) => {
    setPhase({k: 'check', ctx})
    try {
      const c = await be.server.k8sCheck(sid, ctx)
      if (!alive.current) return
      if (!c.ok) { setPhase({k: 'fail', ctx, check: c}); return }
      const a = await be.server.k8sAccess(sid, ctx, '')
      if (!alive.current) return
      if (direct.current) onReady({...known.current!, ctx, check: c, rights: a.rights})
      else setPhase({k: 'access', ctx, check: c, rights: a.rights})
    } catch (e) { if (alive.current) setPhase({k: 'error', message: errMsg(e)}) }
  }, [be, sid, host.id, onReady])

  // the steps up to the choice of a context
  useEffect(() => {
    if (again && attempt === 0) { void check(again.ctx); return }
    let off = false
    ;(async () => {
      try {
        if (attempt > 0 || !seen.current) setPhase({k: 'detect'}) // the first time, a host that had no kubectl keeps saying so while it is asked again
        const d = await be.server.k8sDetect(sid)
        if (off) return
        if (!d.found) { if (!clusterId && !d.direct) rememberNoKubectl(host.id, d); setPhase({k: 'nokubectl', d}); return }
        rememberNoKubectl(host.id, null)
        setPhase({k: 'contexts'})
        const c = await be.server.k8sContexts(sid)
        if (off) return
        known.current = {detect: d, contexts: c}
        if (c.kind === 'kubeconfig' || c.contexts.length === 0) { setPhase({k: 'kubeconfig', c}); return }
        const saved = remembered(host.id)
        const pre = c.contexts.find(x => x.name === saved)
        const only = c.contexts.length === 1 ? c.contexts[0] : undefined
        if (pre || only) { direct.current = !!pre; void check((pre ?? only)!.name); return }
        setChoice((c.contexts.find(x => x.current) ?? c.contexts[0]).name)
        setPhase({k: 'pick', c})
      } catch (e) { if (!off) setPhase({k: 'error', message: errMsg(e)}) }
    })()
    return () => { off = true }
  }, [be, sid, host.id, attempt]) // eslint-disable-line react-hooks/exhaustive-deps

  // the steps are drawn only when they take a while (see STEPS_AFTER_MS)
  const busy = phase.k === 'detect' || phase.k === 'contexts' || phase.k === 'check'
  const [slow, setSlow] = useState(false)
  useEffect(() => {
    if (!busy) { setSlow(false); return }
    const id = window.setTimeout(() => setSlow(true), STEPS_AFTER_MS)
    return () => window.clearTimeout(id)
  }, [busy])

  const again2 = () => { direct.current = false; setAttempt(a => a + 1) }
  const pickAnother = () => { const c = known.current?.contexts; if (c) { setChoice(prev => prev || c.contexts[0]?.name || ''); setPhase({k: 'pick', c}) } else again2() }
  const accessBtn = onAccess ? <button onClick={onAccess}>{t('Access host')}</button> : null
  const checkAgain = (f?: () => void) => <button onClick={f ?? again2}>{t('Check again')}</button>
  const raw = (s: string) => (s ? <div className="k8s-raw">{s}</div> : null)
  const card = (title: string, children: ReactNode, role?: 'alert' | 'status') => <div className="k8s-card" role={role}><h2>{title}</h2>{children}</div>

  if (busy && !slow) return <div className="k8s-wait" role="status" aria-busy="true" />

  if (phase.k === 'detect' || phase.k === 'contexts' || phase.k === 'check') {
    const step = phase.k === 'detect' ? 1 : phase.k === 'contexts' ? 2 : 3
    const d = known.current?.detect
    const viaConfig = !!clusterId || !!d?.direct
    const items: [string, string][] = viaConfig ? [
      [t('kubeconfig'), step < 2 ? t('not asked yet') : step === 2 ? t('reading the kubeconfig …') : t('read from the vault')],
      [t('API server'), step < 3 ? t('not asked yet') : t('asking {name} …', {name: phase.k === 'check' ? phase.ctx : ''})],
      [t('Rights of this login'), t('not asked yet')],
    ] : [
      [t('SSH login'), t('reused from this Panel')],
      ['kubectl', step < 1 || !d ? t('looking for it …') : `${d.path} · ${t('client {v}', {v: d.client_version})}`],
      [t('kubeconfig'), step < 2 ? t('not asked yet') : step === 2 ? t('reading the kubeconfig …') : t('read')],
      [t('API server'), step < 3 ? t('not asked yet') : t('asking {name} …', {name: phase.k === 'check' ? phase.ctx : ''})],
      [t('Rights of this login'), t('not asked yet')],
    ]
    const done = viaConfig ? [step > 2, false, false] : [true, step > 1, step > 2, false, false]
    const working = viaConfig ? [step === 2, step === 3, false] : [false, step === 1, step === 2, step === 3, false]
    return card(viaConfig ? t('Connecting to the cluster') : t('Looking for Kubernetes on this server'), (
      <>
        {items.map(([name, note], i) => (
          <div key={i} className={'k8s-it' + (done[i] || working[i] ? '' : ' todo')}>
            <span className={'k8s-ic' + (done[i] ? ' done' : '')}>{done[i] ? <IconCheck size={12} /> : working[i] ? <span className="k8s-wheel" /> : null}</span>
            <div><b>{name}</b><small>{note}</small></div>
          </div>
        ))}
        <div className="sp-note">{viaConfig ? t('Each step is one read-only request to the API server. Nothing is changed in the cluster.') : t('Each step runs one read-only command. Nothing is installed or changed on the server.')}</div>
      </>
    ), 'status')
  }

  if (phase.k === 'nokubectl') {
    return card(t('kubectl was not found on this server'), (
      <>
        <p>{t('This tab runs kubectl on the host, so it needs kubectl and a kubeconfig for this login. That is usually the control-plane node, or a bastion that manages the cluster. Nothing is installed here.')}</p>
        <p>{t('On k3s the command is k3s kubectl; if only root can read the kubeconfig, sign in as root or allow sudo.')}</p>
        {phase.d.tried.length > 0 && <div className="sp-note">{t('Tried: {list}', {list: phase.d.tried.join(', ')})}</div>}
        <div className="k8s-foot">{checkAgain()}{accessBtn}</div>
      </>
    ))
  }

  if (phase.k === 'kubeconfig') {
    return card(t('The kubeconfig cannot be read'), (
      <>
        <p>{phase.c.contexts.length === 0 && phase.c.kind !== 'kubeconfig'
          ? t('The kubeconfig has no cluster in it. Add one on the host, then check again.')
          : t('kubectl could not read the kubeconfig of this login. If the file is owned by root, sign in as root, allow sudo without a password, or copy it to ~/.kube/config with the right owner.')}</p>
        {raw(phase.c.raw)}
        <div className="k8s-foot">{checkAgain()}{accessBtn}</div>
      </>
    ), 'alert')
  }

  if (phase.k === 'error') {
    return card(t('Kubernetes could not be reached'), (<><p>{known.current?.detect.direct || clusterId ? t('The cluster could not be asked.') : t('The command did not run on the host.')}</p>{raw(phase.message)}<div className="k8s-foot">{checkAgain()}{accessBtn}</div></>), 'alert')
  }

  if (phase.k === 'fail') {
    const {ctx, check: c} = phase
    if (c.kind === 'approval') {
      const approve = async () => {
        try {
          if (clusterId) await be.clusters.approveExec(clusterId, c.raw)
          if (onReopen) onReopen(); else void check(ctx)
        } catch (e) { setPhase({k: 'error', message: errMsg(e)}) }
      }
      return card(t('A program wants to run on this computer'), (
        <>
          <p>{t('To sign in to {name}, this kubeconfig runs the program below on this computer, with your own login to the cloud provider or identity service. Approve it only if you know what it does and the kubeconfig came from someone you trust.', {name: ctx})}</p>
          <div className="k8s-raw" aria-label={t('Command line')}>{c.raw}</div>
          <div className="k8s-foot">
            <button className="primary" onClick={() => void approve()}>{t('Approve and run')}</button>
            <button onClick={() => setPhase({k: 'error', message: t('The login program was not approved, so the cluster cannot be asked.')})}>{t('Cancel')}</button>
            {known.current && known.current.contexts.contexts.length > 1 && <button onClick={pickAnother}>{t('Choose another cluster')}</button>}
          </div>
        </>
      ), 'alert')
    }
    const viaConfig = !!clusterId || !!known.current?.detect.direct
    const text: Record<K8sFailKind, [string, string]> = {
      unreachable: [t('The API server does not answer'), viaConfig
        ? t('{name} at {server} did not answer within 8 seconds. Check that this computer can reach the cluster: a VPN or a bastion may be needed.', {name: ctx, server: c.server})
        : t('{name} at {server} did not answer within 8 seconds. Check the control-plane node, its firewall, or a VPN the host needs.', {name: ctx, server: c.server})],
      unauthorized: [t('The login is not accepted'), viaConfig
        ? t("The credentials in the kubeconfig were refused. A token or a client certificate has probably expired. Ask whoever gave you the kubeconfig for a new one and use Replace kubeconfig, or for a cloud cluster sign in again with the provider's CLI.")
        : t("The credentials in the kubeconfig were refused. A token or a client certificate has probably expired. Renew it on the host (for cloud clusters, sign in again with the provider's CLI), then check again.")],
      certificate: [t('The certificate does not match'), t('The API server presented a certificate for another name, or one that has expired. Vutty never turns the check off. Fix the certificate or the server address in the kubeconfig.')],
      forbidden: [t('This login may not use the cluster'), t('The cluster knows this login but refuses what the tab asks first. Ask for the right to list pods and nodes, or choose another cluster.')],
      other: [t('The cluster could not be asked'), viaConfig ? t('The cluster answered with the message below.') : t('kubectl failed with the message below.')],
    }
    const [title, body] = text[(c.kind || 'other') as K8sFailKind]
    const another = known.current && known.current.contexts.contexts.length > 1
    const useAccess = c.kind === 'unauthorized' || c.kind === 'other'
    return card(title, (
      <>
        <p>{body}</p>
        {raw(c.raw)}
        <div className="k8s-foot">
          {checkAgain(() => void check(ctx))}
          {(useAccess ? accessBtn : null)}
          {another && <button onClick={pickAnother}>{t('Choose another cluster')}</button>}
        </div>
      </>
    ), 'alert')
  }

  if (phase.k === 'pick') {
    const {c} = phase
    const go = () => { if (keep) remember(host.id, choice); else remember(host.id, ''); direct.current = false; void check(choice) }
    return card(t('Which cluster?'), (
      <>
        <p>{c.contexts.length === 1 ? t('This kubeconfig has 1 context. It is used for everything in this tab.') : t('This kubeconfig has {n} contexts. The one you choose is used for everything in this tab.', {n: c.contexts.length})}</p>
        <div role="radiogroup" aria-label={t('Cluster')} className="k8s-opts">
          {c.contexts.map(x => (
            <button key={x.name} className="k8s-opt" role="radio" aria-checked={choice === x.name} onClick={() => setChoice(x.name)}>
              <span className="k8s-rad" />
              <span><b>{x.name}</b><small>{x.server} · {t('user {name}', {name: x.user})}</small></span>
              {/prod/i.test(x.name) && <span className="sp-pill bad">{t('production')}</span>}
              {x.current && <span className="sp-pill">{t('current in kubeconfig')}</span>}
            </button>
          ))}
        </div>
        <label className="k8s-remember"><input type="checkbox" checked={keep} onChange={e => setKeep(e.target.checked)} /> {t('Remember this choice for {name}', {name: host.label})}</label>
        <div className="k8s-foot"><button className="primary" disabled={!choice} onClick={go}>{t('Continue')}</button></div>
      </>
    ))
  }

  // access
  const {ctx, check: c, rights} = phase
  const yes = (...ids: string[]) => ids.every(i => rights[i] !== false)
  const rows: [string, string, boolean][] = [
    ['list pods, deployments, nodes, events', t('Pods, Workloads, Nodes, Events'), yes('list')],
    ['get pods/log', t('Logs'), yes('logs')],
    ['create pods/exec', t('Shell'), yes('exec')],
    ['create pods/portforward', t('Port-forward'), yes('portforward')],
    ['patch pods/ephemeralcontainers', t('Debug'), yes('debug')],
    ['patch deployments, deployments/scale', t('Scale, Restart, Rollback'), yes('scale', 'restart')],
    ['delete pods', t('Delete pod'), yes('delete_pod')],
    ['create jobs, patch cronjobs', t('Run now, Suspend'), yes('cronjob_run', 'cronjob_patch')],
    ['patch nodes, create pods/eviction', t('Cordon, Drain'), yes('cordon', 'drain')],
    ['get secrets', t('Secrets (names only anyway)'), yes('secrets')],
  ]
  return card(t('What this login may do on {name}', {name: ctx}), (
    <>
      <p>{t('Asked once with kubectl auth can-i. A button for something refused stays locked, with the reason in its tooltip.')}</p>
      <div className="sp-table"><table>
        <thead><tr><th>{t('Right')}</th><th>{t('Needed for')}</th><th>{t('Answer')}</th></tr></thead>
        <tbody>{rows.map(([r, need, ok]) => <tr key={r}><td className="m">{r}</td><td>{need}</td><td><span className={'sp-pill ' + (ok ? 'ok' : 'bad')}>{ok ? t('Yes') : t('No')}</span></td></tr>)}</tbody>
      </table></div>
      <div className="k8s-foot">
        <button onClick={() => void check(ctx)}>{t('Check again')}</button>
        <button className="primary" onClick={() => { if (keep && remembered(host.id) !== ctx) remember(host.id, ctx); onReady({...known.current!, ctx, check: c, rights}) }}>{t('Open Kubernetes')}</button>
      </div>
    </>
  ))
}

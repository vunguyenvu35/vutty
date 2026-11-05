import {useEffect} from 'react'
import {createPortal} from 'react-dom'
import {useT} from '../i18n'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {useK8s, useMenuAnchor} from './KubernetesShared'

export type CopyTarget = {kind: 'pod'; ns: string; name: string; container?: string} | {kind: 'deployment' | 'statefulset' | 'daemonset'; ns: string; name: string} | {kind: 'node'; name: string; ip?: string; jump?: string}

/** a word for a shell: as it is when it is plain, else in single quotes */
export const shq = (s: string) => (/^[A-Za-z0-9_.@:/=,+-]+$/.test(s) ? s : "'" + s.replace(/'/g, "'\\''") + "'")

/** The kubectl lines that do the same as a screen, for the context and namespace in use: to paste in a terminal, a ticket or a chat. Nothing here changes the cluster. */
export function kubectlLines(t: ReturnType<typeof useT>, ctx: string, target: CopyTarget): {label: string; cmd: string}[] {
  const c = ' --context ' + shq(ctx)
  const n = target.kind === 'node' ? '' : ' -n ' + shq(target.ns)
  const kind = target.kind === 'node' ? 'node' : target.kind
  const ref = shq(target.name)
  const lines = [
    {label: t('Get as YAML'), cmd: `kubectl get ${kind} ${ref}${n}${c} -o yaml`},
    {label: t('Describe'), cmd: `kubectl describe ${kind} ${ref}${n}${c}`},
  ]
  if (target.kind === 'pod') {
    const ctr = target.container ? ' -c ' + shq(target.container) : ''
    lines.push(
      {label: t('Follow the logs'), cmd: `kubectl logs ${ref}${ctr}${n}${c} -f --tail=100`},
      {label: t('Open a shell'), cmd: `kubectl exec -it ${ref}${ctr}${n}${c} -- sh`},
      {label: t('Forward a port'), cmd: `kubectl port-forward pod/${ref} 8080:80${n}${c}`},
    )
  } else if (target.kind === 'node') {
    lines.push({label: t('CPU and memory now'), cmd: `kubectl top node ${ref}${c}`}, {label: t('Pods on this node'), cmd: `kubectl get pods -A --field-selector spec.nodeName=${ref}${c} -o wide`})
    if (target.ip) lines.push({label: t('SSH to this node'), cmd: target.jump ? `ssh -J ${shq(target.jump)} ${shq(target.ip)}` : `ssh ${shq(target.ip)}`})
  } else {
    lines.push({label: t('Follow the logs'), cmd: `kubectl logs ${kind}/${ref}${n}${c} -f --tail=100 --all-containers --prefix`})
    if (target.kind === 'deployment') lines.push({label: t('Rollout status'), cmd: `kubectl rollout status deployment/${ref}${n}${c}`}, {label: t('Rollout history'), cmd: `kubectl rollout history deployment/${ref}${n}${c}`})
  }
  return lines
}

/** "Copy ▾": a menu of the kubectl lines of an object; a click copies one */
export function CopyMenu({target}: {target: CopyTarget}) {
  const k = useK8s()
  const t = useT()
  const toast = useToast()
  const {btn, menu, at, setAt, toggle, style} = useMenuAnchor()
  useEffect(() => {
    if (!at) return
    const away = (e: MouseEvent) => { if (!(e.target as Element).closest?.('.k8s-menu') && !btn.current?.contains(e.target as Node)) setAt(null) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setAt(null) }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc) }
  }, [at]) // eslint-disable-line react-hooks/exhaustive-deps
  const copy = async (cmd: string) => { setAt(null); try { await k.be.system.writeClipboard(cmd); toast.info(t('Command copied')) } catch (e) { toast.error(errMsg(e)) } }
  return (
    <>
      <button ref={btn} aria-haspopup="menu" aria-expanded={!!at} title={t('Copy the kubectl command that does the same')} onClick={toggle}>{t('Copy kubectl ▾')}</button>
      {at && createPortal(
        <div ref={menu} className="k8s-menu" role="menu" aria-label={t('kubectl commands for {name}', {name: target.name})} style={style}>
          <h4>{t('Copy a kubectl command')}</h4>
          {kubectlLines(t, k.ctx, target).map(l => (
            <button key={l.label} role="menuitem" className="k8s-mi" onClick={() => void copy(l.cmd)}>
              <b>{l.label}</b>
              <small className="m">{l.cmd.length > 90 ? l.cmd.slice(0, 87) + '…' : l.cmd}</small>
            </button>
          ))}
          <div className="sp-note">{t('Nothing here changes the cluster. The context and namespace are in the command.')}</div>
        </div>, document.body)}
    </>
  )
}

import {useState} from 'react'
import {useT} from '../i18n'
import {Field, Modal} from '../ui/Modal'
import {ActBtn, useK8s} from './KubernetesShared'
import {useCanOpenShell} from './KubernetesPods'
import type {K8sPod} from '../types'

const IMAGES = ['nicolaka/netshoot', 'busybox', 'alpine']

/**
 * Debug: adds a temporary container (a tool box such as netshoot) to a pod and opens a shell in it. For a pod with no shell of its own
 * (distroless, scratch) this is the only way in; the tool box shares the pod's network, and with the target also its processes.
 * It cannot be removed again: it goes when the pod does. Asks first; the audit log records it.
 */
export function DebugButton({pod}: {pod: K8sPod}) {
  const k = useK8s()
  const t = useT()
  const canShell = useCanOpenShell()
  const [open, setOpen] = useState(false)
  const [image, setImage] = useState(IMAGES[0])
  const [target, setTarget] = useState('')
  const own = pod.containers.filter(c => !c.init)
  const id = 'debug:' + pod.namespace + '/' + pod.name
  const start = async () => {
    setOpen(false)
    await k.run(id, async () => {
      const d = await k.be.server.k8sDebugStart(k.sid, k.ctx, pod.namespace, pod.name, image.trim(), target)
      if (k.direct) { // the container is there: a shell in it is a terminal tab that talks to the cluster's API
        const sh = await k.be.server.k8sShellSession(k.sid, k.ctx, pod.namespace, pod.name, d.container)
        k.onPodShell?.(sh.host_id, sh.title, k.host.env === 'prod')
      } else {
        k.onShell?.(k.host, d.command, `${k.host.label} · ${pod.name} · debug`)
      }
    })
  }
  return (
    <>
      <ActBtn id={id} right="debug" disabled={pod.status === 'Succeeded' || pod.status === 'Failed' || !canShell} title={!canShell ? t('A terminal cannot be opened from here') : t('Add a temporary tool box container to this pod and open a shell in it')} onClick={() => setOpen(true)}>{t('Debug')}</ActBtn>
      {open && (
        <Modal title={t('Debug {name}', {name: pod.name})} onClose={() => setOpen(false)}
          footer={<><button onClick={() => setOpen(false)}>{t('Cancel')}</button><button className="primary" disabled={!image.trim()} onClick={() => void start()}>{t('Add and open a shell')}</button></>}>
          <Field label={t('Image of the tool box')}>
            <input list="k8s-debug-images" value={image} spellCheck={false} onChange={e => setImage(e.target.value)} />
          </Field>
          <datalist id="k8s-debug-images">{IMAGES.map(i => <option key={i} value={i} />)}</datalist>
          <Field label={t('See the processes of')}>
            <select value={target} onChange={e => setTarget(e.target.value)}>
              <option value="">{t('No container')}</option>
              {own.map(c => <option key={c.name} value={c.name}>{c.name}</option>)}
            </select>
          </Field>
          <div className="sp-note">{t('The container is added to {name} on {ctx} and cannot be removed: it goes away when the pod does. It shares the network of the pod. The image is pulled by the node, so it must be reachable from there. The audit log records it.', {name: pod.name, ctx: k.ctx})}</div>
        </Modal>
      )}
    </>
  )
}

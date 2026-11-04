import {useEffect, useMemo, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {errMsg} from '../util'
import {Kubernetes} from './Kubernetes'
import {Loading} from './shared'
import {Logs, type LogSource} from './ServerPanel'
import type {Cluster, Host} from '../types'

/** The Host that the Kubernetes tab is given for a cluster: it has no address to connect to, only a name, an id (the key of what is remembered) and an environment. */
export const clusterHost = (c: Cluster): Host => ({id: 'kube:' + c.id, label: c.label, hostname: c.server ?? '', port: 0, username: '', keepalive_sec: 0, env: c.env})

type Phase = 'connecting' | 'ready' | 'closed'

/**
 * A cluster tab: the Kubernetes tab of the Panel for a cluster reached with a kubeconfig, with no SSH host in between. The session
 * is opened when the tab shows and closed when it goes away; the kubeconfig is read from the vault by the backend and never seen here.
 * Reopening (after a login program was approved) opens a session again, which is how the backend learns the approval.
 */
export function ClusterPanel({cluster, visible, onPodShell}: {cluster: Cluster; visible: boolean; onPodShell?(hostId: string, title: string, prod: boolean): void}) {
  const be = useBackend()
  const t = useT()
  const [attempt, setAttempt] = useState(0)
  const [phase, setPhase] = useState<Phase>('connecting')
  const [reason, setReason] = useState('')
  const [sid, setSid] = useState('')
  const [section, setSection] = useState<'kubernetes' | 'logs'>('kubernetes')
  const [source, setSource] = useState<LogSource>({kind: '', name: ''})
  const [ctx, setCtx] = useState('')
  const [ns, setNs] = useState('') // what the Kubernetes tab shows: the Logs tab lists the pods of the same
  const host = useMemo(() => clusterHost(cluster), [cluster.id, cluster.label, cluster.server, cluster.env]) // eslint-disable-line react-hooks/exhaustive-deps
  const alive = useRef(true)

  useEffect(() => {
    alive.current = true
    let opened = ''
    setPhase('connecting')
    setReason('')
    setSid('')
    ;(async () => {
      try {
        const info = await be.clusters.open(cluster.id)
        if (!alive.current) { void be.server.close(info.sid).catch(() => {}); return }
        opened = info.sid
        setSid(info.sid)
        setPhase('ready')
      } catch (e) {
        if (alive.current) { setPhase('closed'); setReason(errMsg(e)) }
      }
    })()
    return () => {
      alive.current = false
      if (opened) void be.server.close(opened).catch(() => {})
    }
  }, [be, cluster.id, attempt])

  const sections: ['kubernetes' | 'logs', string][] = [['kubernetes', t('Kubernetes')], ['logs', t('Logs')]]
  return (
    <div className="sp" role="region" aria-label={t('Cluster {name}', {name: cluster.label})}>
      <div className="sp-head">
        <h1>{cluster.label}</h1>
        <span>{cluster.server}</span>
        {cluster.env === 'prod' && <span className="sp-pill bad" title={t('This cluster is marked production')}>PROD</span>}
      </div>
      {phase === 'ready' && sid ? (
        <>
          <div className="sp-tabs" role="tablist" aria-label={t('Cluster sections')}>
            {sections.map(([id, label]) => <button key={id} role="tab" aria-selected={section === id} onClick={() => setSection(id)}>{label}</button>)}
          </div>
          <div className="sp-body">
            <div className="sp-keep" hidden={section !== 'kubernetes'}>
              <Kubernetes key={sid + ':' + attempt} sid={sid} host={host} clusterId={cluster.id} visible={visible && section === 'kubernetes'} onPodShell={onPodShell} onContext={setCtx} onNamespace={setNs} onReopen={() => setAttempt(a => a + 1)}
                onLogs={p => { setSource({kind: 'pod', name: p.ns + '/' + p.name, container: p.container, previous: p.previous}); setSection('logs') }} />
            </div>
            {section === 'logs' && <Logs sid={sid} visible={visible} source={source} onSource={setSource} k8sCtx={ctx} k8sNs={ns} podsOnly />}
          </div>
        </>
      ) : (
        <div className="sp-notice">
          {phase === 'connecting' && <Loading kind="overview" what={t('Opening {name}…', {name: cluster.label})} />}
          {phase === 'closed' && (
            <div className="side-empty" role="alert">
              <div>{reason || t('Disconnected.')}</div>
              <button onClick={() => setAttempt(a => a + 1)}>{t('Try again')}</button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

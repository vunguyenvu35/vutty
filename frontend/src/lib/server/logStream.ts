import {useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {errMsg} from '../util'

/** the lines kept of a followed log: the oldest go as new ones come */
export const STREAM_KEEP = 5000

type Kind = '' | 'service' | 'container' | 'stack' | 'web'
type Since = '' | '15m' | '1h' | '24h'

/** what is followed: a log of the host, or of a pod (`kubectl logs -f`, or the API server for a cluster opened with a kubeconfig) */
export type FollowTarget =
  | {type: 'host'; kind: Kind; name: string; lines: number; since: Since}
  | {type: 'pod'; ctx: string; ns: string; pod: string; container: string; timestamps: boolean; lines: number; since: Since}

/** a key for one followed log: the backend takes 8 to 64 letters, digits and dashes */
const newKey = () => 'f' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)

/**
 * A log read as it grows. While `on` is true the backend holds a command that does not end (on the host, or a request to the cluster)
 * and sends the lines in batches; this keeps the last STREAM_KEEP of them. It stops when `on` turns false or one of the inputs changes (the next one
 * starts with its own last lines). `failed` is why it could not start, so that the caller can fall back to reading again now and
 * then; `ended` is true when the command on the host ended by itself.
 */
export function useLogStream(sid: string, on: boolean, target: FollowTarget) {
  const be = useBackend()
  const [rows, setRows] = useState<string[]>([])
  const [failed, setFailed] = useState('')
  const [ended, setEnded] = useState(false)
  const [skipped, setSkipped] = useState(0)
  const last = useRef('')
  const inputs = `${sid}|${JSON.stringify(target)}`
  // another log: what failed for the one before does not count
  useEffect(() => { setFailed('') }, [inputs])
  useEffect(() => {
    if (!on || failed) return
    let live = true
    const key = newKey()
    last.current = key
    setRows([]); setEnded(false); setSkipped(0)
    const offLines = be.on('server:log:' + key, (d: {lines?: string[]; dropped?: number}) => {
      if (!live) return
      if (d.dropped) setSkipped(n => n + d.dropped!)
      if (d.lines?.length) setRows(r => { const all = r.concat(d.lines!); return all.length > STREAM_KEEP ? all.slice(all.length - STREAM_KEEP) : all })
    })
    const offEnd = be.on('server:logend:' + key, () => { if (live) setEnded(true) })
    const started = target.type === 'pod'
      ? be.server.k8sLogsFollowStart(sid, key, target.ctx, target.ns, target.pod, {container: target.container, lines: target.lines as 100, since: target.since, previous: false, timestamps: target.timestamps})
      : be.server.logsFollowStart(sid, key, target.kind, target.name, target.lines, target.since)
    started.catch(e => { if (live) setFailed(errMsg(e)) })
    return () => {
      live = false
      offLines(); offEnd()
      be.server.logsFollowStop(sid, key).catch(() => {})
    }
  }, [be, on, failed, inputs]) // eslint-disable-line react-hooks/exhaustive-deps
  return {rows, failed, ended, skipped}
}

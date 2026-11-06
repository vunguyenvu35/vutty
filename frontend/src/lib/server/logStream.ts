import {useEffect, useRef, useState} from 'react'
import {useBackend} from '../backend'
import {errMsg} from '../util'

/** the lines kept of a followed log: the oldest go as new ones come */
export const STREAM_KEEP = 5000

type Kind = '' | 'service' | 'container' | 'stack' | 'web'

/** a key for one followed log: the backend takes 8 to 64 letters, digits and dashes */
const newKey = () => 'f' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10)

/**
 * A log read as it grows. While `on` is true the backend holds a command on the host that does not end and sends the lines in
 * batches; this keeps the last STREAM_KEEP of them. It stops when `on` turns false or one of the inputs changes (the next one
 * starts with its own last lines). `failed` is why it could not start, so that the caller can fall back to reading again now and
 * then; `ended` is true when the command on the host ended by itself.
 */
export function useLogStream(sid: string, on: boolean, kind: Kind, name: string, lines: number, since: '' | '15m' | '1h' | '24h') {
  const be = useBackend()
  const [rows, setRows] = useState<string[]>([])
  const [failed, setFailed] = useState('')
  const [ended, setEnded] = useState(false)
  const [skipped, setSkipped] = useState(0)
  const last = useRef('')
  const inputs = `${sid}|${kind}|${name}|${lines}|${since}`
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
    be.server.logsFollowStart(sid, key, kind, name, lines, since).catch(e => { if (live) setFailed(errMsg(e)) })
    return () => {
      live = false
      offLines(); offEnd()
      be.server.logsFollowStop(sid, key).catch(() => {})
    }
  }, [be, on, failed, sid, kind, name, lines, since])
  return {rows, failed, ended, skipped}
}

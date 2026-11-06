import {useEffect, useRef} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {errMsg} from '../util'
import {alertText, keepFleetHosts, reportFleet, setFleetNotifier, useFleetAlertConfig} from './fleetAlerts'
import type {Host, ServerFleetHost} from '../types'

const MONITOR_MS = 60000
const WORKERS = 2

/**
 * Draws nothing. While desktop notifications are on it hands them to the system, and while the Fleet page is not the one in sight it
 * asks every saved host (a short connection each, as the page does) once a minute, so that a server that goes wrong is told about
 * when you are in another page or another app.
 */
export function FleetMonitor({hosts, fleetShown}: {hosts: Host[]; fleetShown: boolean}) {
  const be = useBackend()
  const t = useT()
  const cfg = useFleetAlertConfig()
  const list = hosts.filter(h => h.hostname)
  const ids = list.map(h => h.id).join(',')
  const listRef = useRef(list)
  listRef.current = list
  const busy = useRef(false)
  const tRef = useRef(t)
  tRef.current = t

  useEffect(() => { keepFleetHosts(ids ? ids.split(',') : []) }, [ids])

  useEffect(() => {
    if (!cfg.notify) { setFleetNotifier(null); return }
    setFleetNotifier((label, alerts) => {
      const body = alerts.map(a => alertText(tRef.current, a)).join(', ')
      void be.server.notify(label, body).catch(() => { /* a system that cannot show it: the alerts are on the Fleet page */ })
    })
    return () => setFleetNotifier(null)
  }, [be, cfg.notify])

  useEffect(() => {
    if (!cfg.notify || fleetShown) return
    let alive = true
    const round = async () => {
      if (busy.current) return
      busy.current = true
      const targets = [...listRef.current]
      let next = 0
      const worker = async () => {
        while (alive && next < targets.length) {
          const h = targets[next++]
          let r: ServerFleetHost
          try { r = await be.server.fleetHost(h.id) } catch (e) { r = {host_id: h.id, error: errMsg(e), overview: {} as ServerFleetHost['overview'], failed_units: 0, latency_ms: 0} }
          if (alive) reportFleet(h, r)
        }
      }
      await Promise.all(Array.from({length: Math.min(WORKERS, targets.length)}, worker))
      busy.current = false
    }
    void round()
    const timer = window.setInterval(() => void round(), MONITOR_MS)
    return () => { alive = false; busy.current = false; window.clearInterval(timer) }
  }, [be, cfg.notify, fleetShown])

  return null
}

import {useEffect, useState} from 'react'

/** busy, held on for a moment after it ends so that a quick refresh still shows that it ran. */
export function useMinBusy(busy: boolean, ms = 600): boolean {
  const [held, setHeld] = useState(busy)
  useEffect(() => {
    if (busy) { setHeld(true); return }
    const id = window.setTimeout(() => setHeld(false), ms)
    return () => window.clearTimeout(id)
  }, [busy, ms])
  return busy || held
}

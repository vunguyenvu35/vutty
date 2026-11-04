import {useCallback, useEffect, useState} from 'react'
import {IconRefresh} from '../ui/Icons'
import {useMinBusy} from '../ui/useMinBusy'
import {Section, SidePanel} from '../ui/Modal'
import {Select} from '../ui/Select'
import {useBackend} from '../backend'
import {errMsg} from '../util'
import {useT} from '../i18n'

export const BAUD_RATES = [1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600]

/** "/dev/cu.usbserial-1410" → "cu.usbserial-1410" for tab titles. */
export const portName = (path: string) => path.replace(/^\/dev\//, '')

/** Docked panel to open a serial port (USB adapter, router console, board) in a terminal tab. */
export function SerialPanel({onClose, onConnect}: {onClose(): void; onConnect(path: string, baud: number): void}) {
  const t = useT()
  const be = useBackend()
  const [ports, setPorts] = useState<string[] | null>(null)
  const [port, setPort] = useState('')
  const [baud, setBaud] = useState('115200')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)
  const spinning = useMinBusy(loading)

  const refresh = useCallback(async () => {
    setErr('')
    setLoading(true)
    try {
      const list = (await be.serial.ports()) ?? []
      setPorts(list)
      setPort(p => (list.includes(p) ? p : list[0] ?? ''))
    } catch (e) {
      setPorts([])
      setErr(errMsg(e))
    } finally {
      setLoading(false)
    }
  }, [be])
  useEffect(() => { void refresh() }, [refresh])

  return (
    <SidePanel title={t('Serial')} onClose={onClose}
      footer={<><button onClick={onClose}>{t('Cancel')}</button><button className="primary" disabled={!port} onClick={() => onConnect(port, Number(baud))}>{t('Connect')}</button></>}>
      <Section title={t('Port')}>
        {ports === null && <div className="muted">{t('Looking for serial ports…')}</div>}
        {ports && ports.length === 0 && <div className="muted">{t('No serial ports found. Plug the device in, then refresh.')}</div>}
        {ports && ports.length > 0 && <Select label={t('Serial port')} value={port} onChange={setPort} options={ports.map(p => ({value: p, label: portName(p), hint: p}))} />}
        <button aria-busy={spinning} onClick={refresh}><IconRefresh size={14} /> {t('Refresh')}</button>
      </Section>
      <Section title={t('Speed')}>
        <Select label={t('Baud rate')} value={baud} onChange={setBaud} options={BAUD_RATES.map(b => ({value: String(b), label: String(b), hint: b === 115200 ? t('common default') : b === 9600 ? t('classic default') : undefined}))} />
        <div className="muted">{t('8 data bits, no parity, 1 stop bit: what almost every device uses.')}</div>
      </Section>
      {err && <div className="error" role="alert">{err}</div>}
    </SidePanel>
  )
}

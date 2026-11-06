import {useState} from 'react'
import {ConnectScreen} from './ConnectScreen'
import type {Host} from '../types'
import {useT} from '../i18n'

/** 'panel' is no protocol: it opens a dashboard of the server (over SSH) in the tab instead of a terminal */
export type Protocol = 'ssh' | 'telnet' | 'ftp' | 'rdp' | 'panel'

interface Props {
  host: Host
  onContinue(p: Protocol): void
  onClose(): void
}

/** "Choose protocol": SSH, or Telnet when the host has it (unencrypted, so never the default). */
export function ProtocolScreen({host, onContinue, onClose}: Props) {
  const t = useT()
  const [choice, setChoice] = useState<Protocol>('ssh')
  const all: {id: Protocol | 'mosh'; name: string; cmd: string; port: number; off?: string}[] = [
    {id: 'ssh', name: 'SSH', cmd: `ssh ${host.hostname}`, port: host.port},
    {id: 'mosh', name: 'Mosh', cmd: `mosh ${host.hostname}`, port: host.port, off: t('Not supported yet')},
    {id: 'telnet', name: 'Telnet', cmd: `telnet ${host.hostname}`, port: host.telnet_port || 23, off: host.telnet_enabled ? undefined : t('Turn it on in the host')},
    {id: 'ftp', name: host.ftp_security ? 'FTPS' : 'FTP', cmd: t('Files of the host in the SFTP tab'), port: host.ftp_port || 21, off: host.ftp_enabled ? undefined : t('Turn it on in the host')},
    {id: 'rdp', name: 'RDP', cmd: t('Remote Desktop, in your own client'), port: host.rdp_port || 3389, off: host.rdp_enabled ? undefined : t('Turn it on in the host')},
    {id: 'panel', name: t('Panel'), cmd: t('Server dashboard: load, processes, services, Docker, logs'), port: host.port},
  ]
  // what can be used first, what cannot yet at the end; each group keeps its order
  const options = [...all.filter(o => !o.off), ...all.filter(o => o.off)]
  const portText = t('port: {port}', {port: '\u0001'}).split('\u0001')
  return (
    <ConnectScreen label={host.label} os={host.os} subtitle={host.hostname} step="choose"
      footer={<>
        <button onClick={onClose}>{t('Close')}</button>
        <button className="primary" onClick={() => onContinue(choice)}>{t('Continue')}</button>
      </>}>
      <h3 className="connect-title">{t('Choose protocol')}</h3>
      <div className="proto-list" role="radiogroup" aria-label={t('Protocol')}>
        {options.map(o => (
          <button key={o.id} role="radio" aria-checked={choice === o.id} aria-disabled={!!o.off} title={o.off} className={'proto ' + o.id + (choice === o.id ? ' on' : '') + (o.off ? ' off' : '')}
            onClick={() => { if (!o.off) setChoice(o.id as Protocol) }} onDoubleClick={() => { if (!o.off) onContinue(o.id as Protocol) }}>
            <span className="proto-main"><b>{o.name}</b><em>{o.cmd}</em></span>
            <span className="proto-port">{o.off ? <em>{o.off}</em> : o.id === 'panel' || o.id === 'ftp' || o.id === 'rdp' ? <em>{o.id === 'ftp' ? t('Files only') : o.id === 'rdp' ? t('Opens another app') : t('No terminal')}</em> : <>{portText[0]}<u>{o.port}</u>{portText[1]}</>}</span>
          </button>
        ))}
      </div>
      {choice === 'ftp' && !host.ftp_security && <div className="muted telnet-warning" role="note">{t('Plain FTP is not encrypted: the password and every file cross the network in clear text.')}</div>}
      {choice === 'telnet' && <div className="muted telnet-warning" role="note">{t('Telnet is not encrypted: what you type, the password included, crosses the network in clear text.')}</div>}
    </ConnectScreen>
  )
}

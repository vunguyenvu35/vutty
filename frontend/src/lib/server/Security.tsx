import {useCallback, useState} from 'react'
import {useBackend} from '../backend'
import {useT} from '../i18n'
import {useConfirm} from '../ui/Confirm'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {Card, Err, Toolbar, useLoad} from './shared'
import type {ServerSecurity} from '../types'

/** The Security tab: who is knocking (failed SSH logins), who fail2ban keeps out, and how sshd is set up. */
export function Security({sid}: {sid: string}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const confirm = useConfirm()
  const [working, setWorking] = useState('')
  const load = useCallback(() => be.server.security(sid), [be, sid])
  const {data, error, busy, reload} = useLoad<ServerSecurity>(load, null)
  const unban = async (jail: string, ip: string) => {
    if (!(await confirm(t('Let {ip} in again? It is removed from the jail {jail}.', {ip, jail}), {confirmLabel: t('Unban')}))) return
    setWorking(ip)
    try { await be.server.unban(sid, jail, ip); toast.info(t('{name}: done', {name: ip})) } catch (e) { toast.error(errMsg(e)) }
    setWorking('')
    reload()
  }
  const ssh = data?.ssh
  return (
    <div className="sp-view">
      <Err error={error} />
      <Card title={t('SSH settings')} right={<Toolbar busy={busy} onReload={reload} />}>
        {data && !ssh?.known && <div className="sp-empty">{t('This login cannot read the SSH configuration.')}</div>}
        {ssh?.known && (
          <table className="sp-kv"><tbody>
            <tr><td>{t('Port')}</td><td className="m">{ssh.port}</td></tr>
            <tr><td>{t('Root login')}</td><td><span className={'sp-pill ' + (ssh.root_login === 'no' ? 'ok' : 'warn')}>{ssh.root_login === 'no' ? t('Off') : ssh.root_login}</span></td></tr>
            <tr><td>{t('Password login')}</td><td><span className={'sp-pill ' + (ssh.password_auth === 'no' ? 'ok' : 'warn')}>{ssh.password_auth === 'no' ? t('Off') : t('On')}</span></td></tr>
          </tbody></table>
        )}
        {ssh?.known && (ssh.root_login !== 'no' || ssh.password_auth !== 'no') && <div className="sp-note">{t('Safer: keys only, and no direct root login. Change it in sshd_config in the terminal.')}</div>}
      </Card>
      <Card title={t('Failed SSH logins (last 24 hours)')}>
        {data && !data.failed.known && <div className="sp-empty">{t('There is no SSH log this login can read.')}</div>}
        {data?.failed.known && (
          <>
            <div className="sp-bar"><span className={'sp-pill ' + (data.failed.total > 100 ? 'warn' : 'ok')}>{t('{n} failed logins', {n: data.failed.total})}</span></div>
            {data.failed.top.length > 0 && (
              <div className="sp-table"><table>
                <thead><tr><th>{t('Address')}</th><th className="n">{t('Attempts')}</th></tr></thead>
                <tbody>{data.failed.top.map(x => <tr key={x.ip}><td className="m">{x.ip}</td><td className="n">{x.count}</td></tr>)}</tbody>
              </table></div>
            )}
          </>
        )}
      </Card>
      <Card title="fail2ban">
        {data && !data.fail2ban.installed && <div className="sp-empty">{t('fail2ban is not installed on this server.')}</div>}
        {data?.fail2ban.installed && !data.fail2ban.running && <div className="sp-empty">{t('fail2ban is installed but not running, or this login cannot ask it.')}</div>}
        {data?.fail2ban.running && (
          <div className="sp-table"><table>
            <thead><tr><th>{t('Jail')}</th><th className="n">{t('Banned now')}</th><th className="n">{t('Banned in total')}</th><th>{t('Addresses')}</th></tr></thead>
            <tbody>
              {data.fail2ban.jails.map(j => (
                <tr key={j.name}>
                  <td><b>{j.name}</b></td><td className="n">{j.banned}</td><td className="n">{j.total_banned}</td>
                  <td><div className="sp-chips">{j.ips.map(ip => (
                    <span key={ip} className="sp-chip"><span className="m">{ip}</span><button disabled={working === ip} aria-label={t('Unban {ip}', {ip})} onClick={() => void unban(j.name, ip)}>{t('Unban')}</button></span>
                  ))}{j.ips.length === 0 && '–'}</div></td>
                </tr>
              ))}
            </tbody>
          </table>{data.fail2ban.jails.length === 0 && <div className="sp-empty">{t('No jails.')}</div>}</div>
        )}
      </Card>
      <div className="sp-note">{t('Ports that are open to the outside are listed under Ports & firewall.')}</div>
    </div>
  )
}

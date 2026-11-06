import type {TFn} from '../i18n'
import type {ServerAudit, ServerSecurity} from '../types'

export type CheckLevel = 'ok' | 'warn' | 'bad' | 'info'
export interface SecurityCheck {
  id: string
  level: CheckLevel
  title: string
  detail: string
}

/** The Security check: what the tab reads, put into a list of things that are fine and things to look at. A check that cannot be made is left out, not shown as fine. */
export function securityChecks(t: TFn, s: ServerSecurity | null, a: ServerAudit | null): SecurityCheck[] {
  const out: SecurityCheck[] = []
  const add = (id: string, level: CheckLevel, title: string, detail: string) => out.push({id, level, title, detail})
  if (s?.ssh.known) {
    const r = s.ssh.root_login
    if (r === 'no') add('root', 'ok', t('Root cannot log in over SSH'), '')
    else if (r === 'prohibit-password' || r === 'without-password') add('root', 'ok', t('Root can log in over SSH with a key only'), '')
    else add('root', 'bad', t('Root can log in over SSH'), t('Turn it off under SSH settings, after another account can use sudo.'))
    if (s.ssh.password_auth === 'no') add('password', 'ok', t('SSH logins need a key'), '')
    else add('password', 'warn', t('SSH accepts passwords'), t('Anyone on the internet can try them. Keys only is safer.'))
    if (s.ssh.port === 22) add('port', 'info', t('SSH listens on the usual port 22'), t('A different port cuts the noise in the logs; it does not replace the other checks.'))
  }
  if (s) {
    if (!s.fail2ban.installed) add('f2b', 'warn', t('fail2ban is not installed'), t('It bans the addresses that keep failing to log in.'))
    else if (!s.fail2ban.running) add('f2b', 'warn', t('fail2ban is installed but not running'), '')
    else add('f2b', 'ok', t('fail2ban is running'), '')
    if (s.failed.known && s.failed.total >= 100) add('failed', 'warn', t('{n} failed SSH logins in the last 24 hours', {n: s.failed.total}), t('Ban the addresses below, or turn passwords off.'))
  }
  if (a) {
    if (a.firewall === 'ufw' || a.firewall === 'firewalld' || a.firewall === 'nft') add('fw', 'ok', t('A firewall is on'), '')
    else if (a.firewall === 'ufw-off' || a.firewall === 'firewalld-off') add('fw', 'bad', t('The firewall is installed but off'), t('Every port that something listens on is open. Turn it on under Ports & firewall.'))
    else add('fw', 'warn', t('No firewall was found'), t('Install ufw, or keep the ports closed some other way.'))
    if (a.auto_updates === 'yes') add('auto', 'ok', t('Security updates install by themselves'), '')
    else if (a.auto_updates === 'no') add('auto', 'warn', t('Automatic updates are installed but not scheduled'), '')
    else add('auto', 'warn', t('Updates do not install by themselves'), t('Install unattended-upgrades (Debian, Ubuntu) or dnf-automatic, or update by hand under Maintenance.'))
    if (a.reboot_required) add('reboot', 'warn', t('The server needs a reboot'), t('An update is waiting for it.'))
    if (a.uid0.length > 0) add('uid0', 'bad', t('Other accounts have root rights (user id 0): {names}', {names: a.uid0.join(', ')}), t('Only root should have user id 0.'))
    if (a.passwords_known) {
      if (a.empty_passwords.length > 0) add('emptypw', 'bad', t('Accounts without a password: {names}', {names: a.empty_passwords.join(', ')}), t('Lock them or give them a password.'))
      else add('emptypw', 'ok', t('No account has an empty password'), '')
    }
    if (a.nopasswd > 0) add('nopasswd', 'info', t('{n} sudo rules need no password', {n: a.nopasswd}), t('Common on cloud images; it means a stolen key is a root key.'))
  }
  return out
}

/** How many checks pass of those that can pass or fail (a note is neither). */
export function securityScore(checks: SecurityCheck[]): {ok: number; total: number} {
  const judged = checks.filter(c => c.level !== 'info')
  return {ok: judged.filter(c => c.level === 'ok').length, total: judged.length}
}

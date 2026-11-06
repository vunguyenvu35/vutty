import {securityChecks, securityScore} from './securityChecks'
import type {ServerAudit, ServerSecurity} from '../types'

const t = (s: string, p?: Record<string, string | number>) => s.replace(/\{(\w+)\}/g, (_, k) => String(p?.[k] ?? ''))
const sec = (over: Partial<ServerSecurity['ssh']> = {}, f2b: Partial<ServerSecurity['fail2ban']> = {}, failed = 0): ServerSecurity => ({
  fail2ban: {installed: true, running: true, jails: [], ...f2b}, ssh: {known: true, port: 2222, root_login: 'no', password_auth: 'no', ...over}, failed: {known: true, total: failed, top: []},
})
const aud = (over: Partial<ServerAudit> = {}): ServerAudit => ({firewall: 'ufw', auto_updates: 'yes', uid0: [], empty_passwords: [], passwords_known: true, nopasswd: 0, reboot_required: false, ...over})
const level = (c: ReturnType<typeof securityChecks>, id: string) => c.find(x => x.id === id)?.level

describe('Security check', () => {
  it('passes a server that is set up well', () => {
    const c = securityChecks(t, sec(), aud())
    expect(c.every(x => x.level === 'ok')).toBe(true)
    expect(securityScore(c)).toEqual({ok: c.length, total: c.length})
  })

  it('marks what is wrong, each at its own level', () => {
    const c = securityChecks(t, sec({root_login: 'yes', password_auth: 'yes', port: 22}, {installed: false, running: false}, 500), aud({firewall: 'ufw-off', auto_updates: 'none', reboot_required: true, uid0: ['toor'], empty_passwords: ['bob'], nopasswd: 2}))
    expect(level(c, 'root')).toBe('bad')
    expect(level(c, 'password')).toBe('warn')
    expect(level(c, 'port')).toBe('info')
    expect(level(c, 'f2b')).toBe('warn')
    expect(level(c, 'failed')).toBe('warn')
    expect(level(c, 'fw')).toBe('bad')
    expect(level(c, 'auto')).toBe('warn')
    expect(level(c, 'reboot')).toBe('warn')
    expect(level(c, 'uid0')).toBe('bad')
    expect(level(c, 'emptypw')).toBe('bad')
    expect(level(c, 'nopasswd')).toBe('info')
    expect(c.find(x => x.id === 'uid0')?.title).toContain('toor')
    const score = securityScore(c)
    expect(score.ok).toBe(0)
    expect(score.total).toBe(c.length - 2) // the two notes are not judged
  })

  it('leaves out a check that cannot be made instead of calling it fine', () => {
    const c = securityChecks(t, null, aud({passwords_known: false}))
    expect(c.find(x => x.id === 'emptypw')).toBeUndefined()
    expect(c.find(x => x.id === 'root')).toBeUndefined()
    expect(securityChecks(t, sec({known: false}), null).find(x => x.id === 'root')).toBeUndefined()
    expect(level(securityChecks(t, sec({root_login: 'prohibit-password'}), null), 'root')).toBe('ok')
  })
})

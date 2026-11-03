import {clearHistory, getHistory, MAX_HISTORY, MAX_HOSTS, recordCommand, subscribeHistory} from './history'

beforeEach(() => clearHistory())

describe('command history', () => {
  it('keeps newest first, moves a repeated command up and trims it', () => {
    recordCommand('h1', 'ls')
    recordCommand('h1', '  pwd  ')
    recordCommand('h1', 'ls')
    expect(getHistory('h1')).toEqual(['ls', 'pwd'])
  })
  it('keeps a separate list for each host: a command typed here is not offered there', () => {
    recordCommand('local', 'cd Data/projects/me')
    recordCommand('server', 'systemctl status nginx')
    expect(getHistory('local')).toEqual(['cd Data/projects/me'])
    expect(getHistory('server')).toEqual(['systemctl status nginx'])
    expect(getHistory('another')).toEqual([])
    clearHistory('server')
    expect(getHistory('server')).toEqual([])
    expect(getHistory('local')).toEqual(['cd Data/projects/me'])
  })
  it('keeps at most MAX_HISTORY commands per host, refuses empty and huge lines, and tells subscribers', () => {
    const seen = vi.fn()
    const off = subscribeHistory(seen)
    for (let i = 0; i < MAX_HISTORY + 20; i++) recordCommand('h1', 'cmd ' + i)
    expect(getHistory('h1')).toHaveLength(MAX_HISTORY)
    expect(getHistory('h1')[0]).toBe('cmd ' + (MAX_HISTORY + 19))
    recordCommand('h1', '')
    recordCommand('h1', 'x'.repeat(5000))
    recordCommand('', 'no host')
    expect(getHistory('h1')).toHaveLength(MAX_HISTORY)
    off()
    seen.mockClear()
    recordCommand('h1', 'after')
    expect(seen).not.toHaveBeenCalled()
  })
  it('forgets the host used longest ago when there are too many hosts', () => {
    for (let i = 0; i < MAX_HOSTS; i++) recordCommand('host-' + i, 'ls')
    recordCommand('host-0', 'pwd') // used again: it is now the most recent
    recordCommand('one-more', 'ls')
    expect(getHistory('host-0')).toEqual(['pwd', 'ls'])
    expect(getHistory('host-1')).toEqual([])
    expect(getHistory('one-more')).toEqual(['ls'])
  })
  it('returns the same list until it changes, so a screen can compare it', () => {
    recordCommand('h1', 'ls')
    expect(getHistory('h1')).toBe(getHistory('h1'))
    expect(getHistory('nothing')).toBe(getHistory('nothing-either'))
  })
  it('is stored in localStorage', () => {
    recordCommand('h1', 'uptime')
    expect(JSON.parse(localStorage.getItem('vutty.history.v2')!)).toEqual({h1: ['uptime']})
    clearHistory()
    expect(getHistory('h1')).toEqual([])
  })
})

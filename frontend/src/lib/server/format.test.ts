import {formatRate, formatUptime, level, logLevel, pct} from './format'

describe('server formatting', () => {
  it('formats uptime, rates and percentages', () => {
    expect(formatUptime(3542400)).toBe('41d 0h')
    expect(formatUptime(7500)).toBe('2h 5m')
    expect(formatUptime(90)).toBe('1m')
    expect(formatRate(500)).toBe('500 B/s')
    expect(formatRate(120000)).toBe('117 KB/s')
    expect(pct(1, 4)).toBe(25)
    expect(pct(5, 0)).toBe(0)
    expect(pct(9, 4)).toBe(100)
    expect(level(50)).toBe('ok')
    expect(level(80)).toBe('warn')
    expect(level(95)).toBe('bad')
  })
  it('tells problems and warnings in a log line', () => {
    expect(logLevel('app: Error: ECONNREFUSED')).toBe('err')
    expect(logLevel('Failed to start job')).toBe('err')
    expect(logLevel('systemd: warning: unit changed')).toBe('warn')
    expect(logLevel('Accepted publickey')).toBe('')
    expect(logLevel('terrorism report')).toBe('') // a word, not a part of one
  })
})

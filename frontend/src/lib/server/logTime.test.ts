import {describe, expect, it} from 'vitest'
import {around, fromServerLocal, lineTime, offsetText, serverClockText, toServerLocal, withContext} from './logTime'

describe('times of a log', () => {
  it('writes an offset', () => {
    expect(offsetText(25200)).toBe('+07:00')
    expect(offsetText(0)).toBe('+00:00')
    expect(offsetText(-12600)).toBe('-03:30')
  })
  it('turns a time typed for the server into RFC 3339 and back', () => {
    const iso = fromServerLocal('2026-11-06T03:12', 25200)
    expect(iso).toBe('2026-11-06T03:12:00+07:00')
    expect(Date.parse(iso!)).toBe(Date.parse('2026-11-05T20:12:00Z'))
    expect(toServerLocal(iso!, 25200)).toBe('2026-11-06T03:12')
    expect(serverClockText('2026-11-05T20:12:05Z', 25200)).toBe('2026-11-06 03:12:05')
  })
  it('refuses what is not a time', () => {
    expect(fromServerLocal('yesterday', 0)).toBeNull()
    expect(fromServerLocal('2026-13-45T99:99', 0)).toBeNull()
    expect(toServerLocal('nope', 0)).toBe('')
  })
  it('reads the time at the start of a line of the journal, docker or kubectl', () => {
    expect(lineTime('2026-11-06T03:12:44+0700 host sshd[1]: hi')).toBe('2026-11-05T20:12:44Z')
    expect(lineTime('2026-11-05T20:12:44.123456789Z GET /')).toBe('2026-11-05T20:12:44Z')
    expect(lineTime('no time here')).toBeNull()
    expect(lineTime('-- Boot abc --')).toBeNull()
  })
  it('gives the range around a time', () => {
    expect(around('2026-11-05T20:12:44Z', 5)).toEqual({from: '2026-11-05T20:07:44Z', until: '2026-11-05T20:17:44Z'})
  })
  it('keeps the lines around a match and marks the holes', () => {
    const l = ['a', 'b', 'X', 'c', 'd', 'e', 'f', 'g', 'X', 'h']
    expect(withContext(l, s => s === 'X', 1)).toEqual(['b', 'X', 'c', null, 'g', 'X', 'h'])
    expect(withContext(l, () => false, 1)).toEqual([])
  })
})

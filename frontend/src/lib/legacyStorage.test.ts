import {describe, expect, it} from 'vitest'
import {migrateLegacyStorage} from './legacyStorage'

describe('migrateLegacyStorage', () => {
  it('moves vvterm and vuterm keys to vutty and keeps a value that is already there', () => {
    localStorage.clear()
    localStorage.setItem('vvterm.sidebar', 'closed')
    localStorage.setItem('vvterm:ai-next-model', 'x')
    localStorage.setItem('vvterm.recent', 'old')
    localStorage.setItem('vutty.recent', 'new')
    localStorage.setItem('vuterm.hostsView', 'list')
    localStorage.setItem('vuterm:suggest-popup', 'y')
    localStorage.setItem('other', 'keep')
    migrateLegacyStorage()
    expect(localStorage.getItem('vutty.sidebar')).toBe('closed')
    expect(localStorage.getItem('vutty:ai-next-model')).toBe('x')
    expect(localStorage.getItem('vutty.recent')).toBe('new')
    expect(localStorage.getItem('vutty.hostsView')).toBe('list')
    expect(localStorage.getItem('vutty:suggest-popup')).toBe('y')
    expect(localStorage.getItem('vvterm.sidebar')).toBeNull()
    expect(localStorage.getItem('vuterm.hostsView')).toBeNull()
    expect(localStorage.getItem('other')).toBe('keep')
  })
})

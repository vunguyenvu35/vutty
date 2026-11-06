import {baseHostId, protocolOf} from './hostId'

describe('terminal host ids', () => {
  it('a Telnet or Mosh terminal stands for the saved host behind its prefix', () => {
    expect(baseHostId('telnet:h1')).toBe('h1')
    expect(baseHostId('mosh:h2')).toBe('h2')
    expect(baseHostId('h3')).toBe('h3')
    expect(baseHostId('serial:/dev/ttyUSB0@9600')).toBe('serial:/dev/ttyUSB0@9600')
    expect(protocolOf('telnet:h1')).toBe('telnet')
    expect(protocolOf('mosh:h2')).toBe('mosh')
    expect(protocolOf('h3')).toBe('')
  })
})

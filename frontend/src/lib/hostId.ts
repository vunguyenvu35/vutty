/** The saved host a terminal's host id stands for: a Telnet or Mosh terminal is "telnet:<id>" or "mosh:<id>". */
export const baseHostId = (id: string): string => id.replace(/^(telnet|mosh):/, '')

/** the protocol a terminal's host id asks for, when it is not SSH */
export const protocolOf = (id: string): '' | 'telnet' | 'mosh' => (id.startsWith('telnet:') ? 'telnet' : id.startsWith('mosh:') ? 'mosh' : '')

import {cdCommand, folderOf} from './format'

describe('cdCommand', () => {
  it('quotes a POSIX path and escapes single quotes', () => {
    expect(cdCommand('/home/a b')).toBe("cd '/home/a b'")
    expect(cdCommand("/tmp/it's")).toBe("cd '/tmp/it'\\''s'")
  })
  it('uses PowerShell syntax for a Windows path', () => {
    expect(cdCommand('C:\\Users\\a [Bank]')).toBe("cd -LiteralPath 'C:\\Users\\a [Bank]'")
    expect(cdCommand("D:/it's")).toBe("cd -LiteralPath 'D:/it''s'")
    expect(cdCommand('\\\\srv\\share')).toBe("cd -LiteralPath '\\\\srv\\share'")
  })
})

describe('folderOf', () => {
  it('is the folder itself, or the parent of a file', () => {
    expect(folderOf({path: '/a/b', is_dir: true})).toBe('/a/b')
    expect(folderOf({path: '/a/b.txt', is_dir: false})).toBe('/a')
    expect(folderOf({path: 'C:\\a\\b.txt', is_dir: false})).toBe('C:\\a')
    expect(folderOf({path: 'C:\\b.txt', is_dir: false})).toBe('C:\\')
  })
})

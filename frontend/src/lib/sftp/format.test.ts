import {breadcrumbs, cdCommand, folderOf, isAbsolutePath, isRoot, joinPath, parentOf, validName} from './format'

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

describe('paths on Windows (the local pane)', () => {
  it('goes up, stopping at the drive root', () => {
    expect(parentOf('C:\\Users\\a')).toBe('C:\\Users')
    expect(parentOf('C:\\Users')).toBe('C:\\')
    expect(parentOf('C:\\')).toBe('C:\\')
    expect(parentOf('C:\\Users\\')).toBe('C:\\')
  })
  it('joins with a backslash and never doubles it', () => {
    expect(joinPath('C:\\Users', 'a')).toBe('C:\\Users\\a')
    expect(joinPath('C:\\', 'a')).toBe('C:\\a')
  })
  it('knows the roots and absolute paths', () => {
    expect(isRoot('C:\\')).toBe(true)
    expect(isRoot('C:\\Users')).toBe(false)
    expect(isRoot('\\\\srv\\share')).toBe(true)
    expect(isRoot('/')).toBe(true)
    expect(isRoot('/home')).toBe(false)
    expect(isAbsolutePath('D:\\x')).toBe(true)
    expect(isAbsolutePath('docs')).toBe(false)
  })
  it('builds breadcrumbs from the drive', () => {
    expect(breadcrumbs('C:\\Users\\a').map(c => c.path)).toEqual(['C:\\', 'C:\\Users', 'C:\\Users\\a'])
    expect(breadcrumbs('/a/b').map(c => c.path)).toEqual(['/', '/a', '/a/b'])
  })
  it('rejects characters Windows does not allow, only for Windows folders', () => {
    expect(validName('a:b', 'C:\\x')).not.toBeNull()
    expect(validName('a:b', '/home')).toBeNull()
  })
  it('keeps POSIX behaviour', () => {
    expect(parentOf('/a/b')).toBe('/a')
    expect(parentOf('/a')).toBe('/')
    expect(joinPath('/', 'a')).toBe('/a')
  })
})

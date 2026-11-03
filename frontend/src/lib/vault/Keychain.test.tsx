import {fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {useState} from 'react'
import {KeychainPage, KeyMode, keyType} from './KeychainPage'
import {KeyPanel, readKeyFile} from './KeyPanel'
import {makeFake, Providers} from '../../test/fakeBackend'
import type {Identity} from '../types'

const ids: Identity[] = [
  {id: 'i1', name: 'deploy', auth_type: 'key', public_key: 'ssh-ed25519 AAAAB3Nza'} as Identity,
  {id: 'i2', name: 'legacy', auth_type: 'key', public_key: 'ssh-rsa AAAAB3'} as Identity,
  {id: 'i3', name: 'db', auth_type: 'password', username: 'root'} as Identity,
  {id: 'i4', name: 'agent', auth_type: 'agent'} as Identity,
]

function Harness({be}: {be: ReturnType<typeof makeFake>}) {
  const [mode, setMode] = useState<KeyMode | null>(null)
  const [draft, setDraft] = useState('')
  const [edit, setEdit] = useState<Identity | null>(null)
  const [list, setList] = useState<Identity[]>(be.state.identities)
  const refresh = async () => setList(await be.vault.listIdentities())
  return (
    <>
      <KeychainPage identities={list} draft={mode && !edit ? draft : undefined} onNew={m => { setDraft(''); setEdit(null); setMode(m) }} onEdit={i => { setEdit(i); setMode(i.auth_type) }} onChanged={refresh} />
      {mode && <KeyPanel key={edit?.id ?? mode} mode={mode} identity={edit ?? undefined} onClose={() => { setMode(null); setEdit(null) }} onChanged={refresh} onDraft={setDraft} />}
    </>
  )
}
function setup(identities: Identity[] = ids.map(i => ({...i}))) { // a copy each time: editing one must not change the next test
  const be = makeFake({identities})
  render(<Providers be={be}><Harness be={be} /></Providers>)
  return be
}
const calls = (be: ReturnType<typeof makeFake>, n: string) => be.state.calls.filter(c => c[0] === n)
const keyFile = (text: string, name = 'id_ed25519') => new File([text], name, {type: 'text/plain'})
const PEM = '-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n'

describe('Keychain page', () => {
  it('shows a toolbar and one card per identity with its type, and no secret material', () => {
    setup()
    expect(screen.getByRole('button', {name: 'New key'})).toBeInTheDocument()
    const cards = within(screen.getByRole('list', {name: 'Identities'})).getAllByRole('listitem')
    expect(cards.map(c => c.querySelector('.label')?.textContent)).toEqual(['agent', 'db', 'deploy', 'legacy']) // by name
    expect(cards.map(c => c.querySelector('.addr')?.textContent)).toEqual(['Type ssh-agent', 'Type Password · root', 'Type ED25519', 'Type RSA'])
    expect(document.body.textContent).not.toMatch(/PRIVATE KEY|secret_ref/)
  })

  it('names key types', () => {
    expect(keyType({auth_type: 'key', public_key: 'ecdsa-sha2-nistp256 AA'})).toBe('ECDSA')
    expect(keyType({auth_type: 'key'})).toBe('Key')
  })

  it('says what to do when there are no keys', () => {
    setup([])
    expect(screen.getByText(/No keys yet/)).toBeInTheDocument()
  })

  it('copies a public key and deletes after confirming', async () => {
    const be = setup()
    const write = vi.fn(async () => {})
    Object.defineProperty(navigator, 'clipboard', {value: {writeText: write}, configurable: true})
    await userEvent.click(screen.getByRole('button', {name: 'Copy public key of deploy'}))
    expect(write).toHaveBeenCalledWith('ssh-ed25519 AAAAB3Nza')
    expect(screen.queryByRole('button', {name: 'Copy public key of db'})).toBeNull()
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Delete db'}))
    expect(be.state.identities.map(i => i.id)).toContain('i3')
    await userEvent.click(screen.getByRole('button', {name: 'Delete db'}))
    await waitFor(() => expect(screen.queryByRole('button', {name: 'Delete db'})).toBeNull())
    expect(confirm).toHaveBeenCalledTimes(2)
    confirm.mockRestore()
  })
})

describe('New key panel', () => {
  it('opens docked next to the list with a draft card that follows the label', async () => {
    setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    const panel = screen.getByRole('dialog', {name: 'New Key'})
    expect(panel.tagName).toBe('ASIDE')
    expect(screen.getByTestId('draft-card')).toHaveTextContent('Add a label…')
    await userEvent.type(within(panel).getByLabelText('Label'), 'prod-key')
    expect(screen.getByTestId('draft-card')).toHaveTextContent('prod-key')
    expect(screen.getByRole('list', {name: 'Identities'})).toBeInTheDocument()
  })

  it('saves a pasted key with its passphrase', async () => {
    const be = setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    const save = screen.getByRole('button', {name: 'Save key'})
    expect(save).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Label'), 'k')
    expect(screen.queryByLabelText('Passphrase')).toBeNull() // only an encrypted key needs one
    fireEvent.change(screen.getByLabelText('Private key'), {target: {value: '-----BEGIN ENCRYPTED PRIVATE KEY-----'}})
    await userEvent.type(screen.getByLabelText('Passphrase'), 'pp')
    await userEvent.click(save)
    await waitFor(() => expect(calls(be, 'vault.addKeyPair')).toHaveLength(1))
    expect(calls(be, 'vault.addKeyPair')[0].slice(1)).toEqual(['k', '-----BEGIN ENCRYPTED PRIVATE KEY-----', 'pp', '', ''])
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.queryByTestId('draft-card')).toBeNull()
  })

  it('drag and drop: dropping a key file fills the private key and the label', async () => {
    setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    const zone = screen.getByTestId('key-drop')
    expect(zone).toHaveTextContent('Drag and drop a private key file to import')
    fireEvent.dragOver(zone)
    expect(zone).toHaveClass('over')
    fireEvent.drop(zone, {dataTransfer: {files: [keyFile(PEM, 'deploy_key.pem')]}})
    await waitFor(() => expect(screen.getByLabelText('Private key')).toHaveValue(PEM))
    expect(screen.getByLabelText('Label')).toHaveValue('deploy_key')
    expect(zone).not.toHaveClass('over')
    expect(screen.getByTestId('draft-card')).toHaveTextContent('deploy_key')
  })

  it('keeps a label that was already typed when a file is dropped', async () => {
    setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    await userEvent.type(screen.getByLabelText('Label'), 'mine')
    fireEvent.drop(screen.getByTestId('key-drop'), {dataTransfer: {files: [keyFile(PEM, 'other')]}})
    await waitFor(() => expect(screen.getByLabelText('Private key')).toHaveValue(PEM))
    expect(screen.getByLabelText('Label')).toHaveValue('mine')
  })

  it('refuses a dropped file that is not a private key, and one that is too large', async () => {
    setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    fireEvent.drop(screen.getByTestId('key-drop'), {dataTransfer: {files: [keyFile('hello world', 'notes.txt')]}})
    expect(await screen.findByRole('alert')).toHaveTextContent('does not look like a private key')
    expect(screen.getByLabelText('Private key')).toHaveValue('')
    await expect(readKeyFile(new File([new Uint8Array(300 * 1024)], 'big'))).rejects.toThrow(/too large/)
  })

  it('"Import from key file" reads the chosen file', async () => {
    setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    await userEvent.upload(screen.getByLabelText('Key file'), keyFile(PEM, 'id_rsa'))
    await waitFor(() => expect(screen.getByLabelText('Private key')).toHaveValue(PEM))
    expect(screen.getByLabelText('Label')).toHaveValue('id_rsa')
  })

  it('shows the error from the vault and keeps the panel open', async () => {
    const be = setup()
    be.vault.addKeyPair = async () => { throw 'the public key does not belong to this private key' }
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    await userEvent.type(screen.getByLabelText('Label'), 'k')
    await userEvent.type(screen.getByLabelText('Private key'), PEM)
    await userEvent.click(screen.getByRole('button', {name: 'Save key'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('does not belong to this private key')
    expect(screen.getByRole('dialog', {name: 'New Key'})).toBeInTheDocument()
  })

  it('generates a key and shows only the public key', async () => {
    const be = setup([])
    await userEvent.click(screen.getByRole('button', {name: 'Generate key'}))
    await userEvent.type(screen.getByLabelText('Label'), 'ci')
    await userEvent.click(screen.getByRole('button', {name: 'Generate'}))
    expect(await screen.findByTestId('pubkey')).toHaveTextContent('ssh-ed25519 AAAAgenerated')
    expect(be.state.identities).toHaveLength(1)
    await waitFor(() => expect(screen.getAllByText('ci', {selector: '.label'})).toHaveLength(2)) // draft card + the new card: the list refreshed behind the panel
    await userEvent.click(screen.getByRole('button', {name: 'Done'}))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('saves a password identity and an ssh-agent identity', async () => {
    const be = setup([])
    await userEvent.click(screen.getByRole('button', {name: 'New password'}))
    await userEvent.type(screen.getByLabelText('Label'), 'db-login')
    await userEvent.type(screen.getByLabelText('Username'), 'root')
    await userEvent.type(screen.getByLabelText('Password'), 's3cret')
    await userEvent.click(screen.getByRole('button', {name: 'Save password'}))
    await waitFor(() => expect(calls(be, 'vault.addPassword')[0].slice(1)).toEqual(['db-login', 'root', 's3cret']))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    await userEvent.click(screen.getByRole('button', {name: 'New ssh-agent identity'}))
    await userEvent.type(screen.getByLabelText('Label'), 'agent-1')
    await userEvent.click(screen.getByRole('button', {name: 'Add identity'}))
    await waitFor(() => expect(calls(be, 'vault.addAgent')).toHaveLength(1))
  })
})

describe('Vault health on the Keychain page', () => {
  it('says nothing when every secret can be read', async () => {
    const be = setup()
    await waitFor(() => expect(calls(be, 'vault.health').length).toBeGreaterThan(0))
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText("Can't be read")).toBeNull()
  })

  it('names the identities whose secret cannot be read, and the ones that belong to no identity', async () => {
    const be = makeFake({identities: ids.map(i => ({...i})), health: {checked: 4, unreadable: [
      {secret_id: 's3', name: 'db', identity_id: 'i3', kind: 'password'},
      {secret_id: 'telnet:h9', name: 'Telnet password of router', kind: 'telnet'},
    ]}})
    render(<Providers be={be}><Harness be={be} /></Providers>)
    const warn = await screen.findByRole('alert')
    expect(warn).toHaveTextContent("2 saved items can't be read with this vault's key")
    expect(warn).toHaveTextContent('Also: Telnet password of router')
    const cards = within(screen.getByRole('list', {name: 'Identities'})).getAllByRole('listitem')
    const marked = cards.filter(c => within(c).queryByText("Can't be read"))
    expect(marked.map(c => c.querySelector('.label')?.textContent)).toEqual(["dbCan't be read"]) // only that card
  })

  it('checks again when the list changes, so entering a secret again clears the mark', async () => {
    const be = makeFake({identities: ids.map(i => ({...i})), health: {checked: 4, unreadable: [{secret_id: 's3', name: 'db', identity_id: 'i3', kind: 'password'}]}})
    render(<Providers be={be}><Harness be={be} /></Providers>)
    await screen.findByRole('alert')
    be.state.health = {checked: 4, unreadable: []} // what the vault finds once it was entered again
    await userEvent.click(screen.getByRole('button', {name: 'Edit db'}))
    const dlg = await screen.findByRole('dialog', {name: 'Edit Password'})
    await userEvent.type(within(dlg).getByLabelText('New password'), 'typed again')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save changes'}))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
    expect(screen.queryByText("Can't be read")).toBeNull()
  })
})

describe('Editing an identity', () => {
  const edit = (name: string) => userEvent.click(screen.getByRole('button', {name: `Edit ${name}`}))

  it('every card has an Edit button (and a double click opens it too), and the panel shows no draft card', async () => {
    setup()
    for (const n of ['agent', 'db', 'deploy', 'legacy']) expect(screen.getByRole('button', {name: `Edit ${n}`})).toBeInTheDocument()
    await userEvent.dblClick(screen.getByText('db'))
    expect(await screen.findByRole('dialog', {name: 'Edit Password'})).toBeInTheDocument()
    expect(screen.queryByTestId('draft-card')).toBeNull()
  })

  it('renames a password identity and changes its user; the password is only sent when a new one is typed', async () => {
    const be = setup()
    await edit('db')
    const dlg = await screen.findByRole('dialog', {name: 'Edit Password'})
    const save = within(dlg).getByRole('button', {name: 'Save changes'})
    expect(save).toBeDisabled() // nothing changed yet
    expect(within(dlg).getByLabelText('Label')).toHaveValue('db')
    expect(within(dlg).getByLabelText('Username')).toHaveValue('root')
    await userEvent.clear(within(dlg).getByLabelText('Label'))
    await userEvent.type(within(dlg).getByLabelText('Label'), 'db-prod')
    await userEvent.clear(within(dlg).getByLabelText('Username'))
    await userEvent.type(within(dlg).getByLabelText('Username'), 'admin')
    await userEvent.click(save)
    await waitFor(() => expect(calls(be, 'vault.updateIdentity')).toHaveLength(1))
    expect(calls(be, 'vault.updateIdentity')[0]).toEqual(['vault.updateIdentity', 'i3', 'db-prod', 'admin', '', '', '', ''])
    await waitFor(() => expect(screen.queryByRole('dialog', {name: 'Edit Password'})).toBeNull())
    expect(screen.getByText('db-prod')).toBeInTheDocument()
    expect(be.state.identities.filter(i => i.name.startsWith('db'))).toHaveLength(1) // edited, not duplicated
  })

  it('replaces the password when a new one is typed', async () => {
    const be = setup()
    await edit('db')
    const dlg = await screen.findByRole('dialog', {name: 'Edit Password'})
    await userEvent.type(within(dlg).getByLabelText('New password'), 's3cret-new')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save changes'}))
    await waitFor(() => expect(calls(be, 'vault.updateIdentity')[0].slice(1, 5)).toEqual(['i3', 'db', 'root', 's3cret-new']))
  })

  it('a key shows its public key and keeps its private key unless a new one is given', async () => {
    const be = setup()
    await edit('deploy')
    const dlg = await screen.findByRole('dialog', {name: 'Edit Key'})
    expect(within(dlg).getByTestId('pubkey')).toHaveTextContent('ssh-ed25519 AAAAB3Nza')
    expect(within(dlg).queryByLabelText('Username')).toBeNull()
    expect(document.body.textContent).not.toMatch(/BEGIN .*PRIVATE KEY/)
    await userEvent.clear(within(dlg).getByLabelText('Label'))
    await userEvent.type(within(dlg).getByLabelText('Label'), 'deploy-2')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save changes'}))
    await waitFor(() => expect(calls(be, 'vault.updateIdentity')[0].slice(1, 5)).toEqual(['i1', 'deploy-2', '', '']))
  })

  it('replaces the private key with a chosen file, and shows the error of a key that is refused', async () => {
    const be = setup()
    await edit('deploy')
    const dlg = await screen.findByRole('dialog', {name: 'Edit Key'})
    fireEvent.change(within(dlg).getByLabelText('Key file'), {target: {files: [keyFile(PEM)]}})
    await waitFor(() => expect(within(dlg).getByLabelText('Private key')).toHaveValue(PEM))
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save changes'}))
    await waitFor(() => expect(calls(be, 'vault.updateIdentity')[0].slice(1, 5)).toEqual(['i1', 'deploy', '', PEM]))
    be.vault.updateIdentity = async () => { throw 'ssh: no key found' }
    await edit('deploy')
    const again = await screen.findByRole('dialog', {name: 'Edit Key'})
    await userEvent.type(within(again).getByLabelText('Private key'), 'junk')
    await userEvent.click(within(again).getByRole('button', {name: 'Save changes'}))
    expect(await within(again).findByRole('alert')).toHaveTextContent('ssh: no key found') // the panel stays open
  })

  it('an ssh-agent identity can be renamed', async () => {
    const be = setup()
    await edit('agent')
    const dlg = await screen.findByRole('dialog', {name: 'Edit ssh-agent identity'})
    await userEvent.type(within(dlg).getByLabelText('Username'), 'me')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save changes'}))
    await waitFor(() => expect(calls(be, 'vault.updateIdentity')[0].slice(1, 5)).toEqual(['i4', 'agent', 'me', '']))
  })
})

describe('New Key panel: the design', () => {
  const open = async () => { setup(); await userEvent.click(screen.getByRole('button', {name: 'New key'})) }

  it('has Label, Private key, Public key and Certificate in that order, then the drop area and the import button', async () => {
    await open()
    const order = [screen.getByLabelText('Label'), screen.getByLabelText('Private key'), screen.getByLabelText('Public key'), screen.getByLabelText('Certificate'),
      screen.getByTestId('key-drop'), screen.getByRole('button', {name: 'Import from key file'})]
    for (let i = 1; i < order.length; i++) {
      expect(order[i - 1].compareDocumentPosition(order[i]) & Node.DOCUMENT_POSITION_FOLLOWING, 'element ' + i).toBeTruthy()
    }
    expect(screen.getByText('Private key *', {selector: 'label'})).toBeInTheDocument() // required, marked
    expect(screen.getByText('Public key', {selector: 'label'})).toBeInTheDocument()
    expect(screen.getByTestId('key-drop').querySelector('svg')).not.toBeNull() // the icon
  })

  it('a field name floats onto its border once the field has something in it', async () => {
    await open()
    const field = screen.getByLabelText('Public key').closest('.float-field')!
    expect(field).not.toHaveClass('filled')
    await userEvent.type(screen.getByLabelText('Public key'), 'ssh-ed25519 AAAA')
    expect(field).toHaveClass('filled')
  })

  it('sends the public key and the certificate with the private key', async () => {
    const be = setup()
    await userEvent.click(screen.getByRole('button', {name: 'New key'}))
    await userEvent.type(screen.getByLabelText('Label'), 'ca-signed')
    await userEvent.type(screen.getByLabelText('Private key'), PEM)
    await userEvent.type(screen.getByLabelText('Public key'), 'ssh-ed25519 AAAApublic')
    await userEvent.type(screen.getByLabelText('Certificate'), 'ssh-ed25519-cert-v01@openssh.com AAAAcert')
    await userEvent.click(screen.getByRole('button', {name: 'Save key'}))
    await waitFor(() => expect(calls(be, 'vault.addKeyPair')).toHaveLength(1))
    expect(calls(be, 'vault.addKeyPair')[0].slice(1)).toEqual(['ca-signed', PEM, '', 'ssh-ed25519 AAAApublic', 'ssh-ed25519-cert-v01@openssh.com AAAAcert'])
    expect((await screen.findAllByText('ca-signed', {selector: '.label'})).length).toBeGreaterThan(0)
  })

  it('asks for a passphrase when the key is encrypted in the OpenSSH way too', async () => {
    await open()
    const body = btoa('openssh-key-v1\0\0\0\0\naes256-ctr\0\0\0\x06bcrypt')
    fireEvent.change(screen.getByLabelText('Private key'), {target: {value: '-----BEGIN OPENSSH PRIVATE KEY-----\n' + body + '\n-----END OPENSSH PRIVATE KEY-----'}})
    expect(screen.getByLabelText('Passphrase')).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Private key'), {target: {value: PEM}})
    expect(screen.queryByLabelText('Passphrase')).toBeNull()
  })
})

describe('Keychain page: filter, sort and view', () => {
  // the shared fixture is changed by the tests above, so these start from their own copies
  const base = (): Identity[] => [
    {id: 'i1', name: 'deploy', auth_type: 'key', public_key: 'ssh-ed25519 AAAAB3Nza'} as Identity,
    {id: 'i2', name: 'legacy', auth_type: 'key', public_key: 'ssh-rsa AAAAB3'} as Identity,
    {id: 'i3', name: 'db', auth_type: 'password', username: 'root'} as Identity,
    {id: 'i4', name: 'agent', auth_type: 'agent'} as Identity,
  ]
  const names = () => within(screen.getByRole('list', {name: 'Identities'})).getAllByRole('listitem').map(c => c.querySelector('.label')?.textContent)
  beforeEach(() => localStorage.clear())

  it('searches the name, the user and the type', async () => {
    setup(base())
    await userEvent.type(screen.getByLabelText('Search keys'), 'root')
    expect(names()).toEqual(['db'])
    await userEvent.clear(screen.getByLabelText('Search keys'))
    await userEvent.type(screen.getByLabelText('Search keys'), 'rsa')
    expect(names()).toEqual(['legacy'])
    await userEvent.clear(screen.getByLabelText('Search keys'))
    await userEvent.type(screen.getByLabelText('Search keys'), 'nothing')
    expect(screen.getByText('No key matches.')).toBeInTheDocument()
  })

  it('filters by kind: keys, passwords, ssh-agent', async () => {
    setup(base())
    await userEvent.click(screen.getByRole('button', {name: 'Keys'}))
    expect(names()).toEqual(['deploy', 'legacy'])
    await userEvent.click(screen.getByRole('button', {name: 'ssh-agent'}))
    expect(names()).toEqual(['agent', 'deploy', 'legacy'])
    await userEvent.click(screen.getByRole('button', {name: 'Keys'}))
    await userEvent.click(screen.getByRole('button', {name: 'ssh-agent'}))
    expect(names()).toEqual(['agent', 'db', 'deploy', 'legacy'])
  })

  it('sorts by name, reversed, or by type; and switches between grid and list', async () => {
    setup(base())
    await userEvent.click(screen.getByRole('button', {name: 'Sort'}))
    await userEvent.click(screen.getByRole('menuitemradio', {name: /Name Z–A/}))
    expect(names()).toEqual(['legacy', 'deploy', 'db', 'agent'])
    await userEvent.click(screen.getByRole('button', {name: 'Sort'}))
    await userEvent.click(screen.getByRole('menuitemradio', {name: /^Type/}))
    expect(names()).toEqual(['deploy', 'db', 'legacy', 'agent']) // ED25519, Password, RSA, ssh-agent
    await userEvent.click(screen.getByRole('button', {name: 'View'}))
    await userEvent.click(screen.getByRole('menuitemradio', {name: /List/}))
    expect(screen.getByRole('list', {name: 'Identities'})).toHaveClass('as-list')
  })

  it('offers kind chips only when there is more than one kind, and no toolbar when there are no keys', () => {
    setup(base().slice(0, 2))
    expect(screen.queryByRole('button', {name: 'Keys'})).toBeNull()
    expect(screen.getByLabelText('Search keys')).toBeInTheDocument()
  })

  it('keeps the draft card of a key being added, whatever the filter', async () => {
    const list = base()
    const be = makeFake({identities: list})
    render(<Providers be={be}><KeychainPage identities={list} draft="new key" onNew={vi.fn()} onChanged={vi.fn()} /></Providers>)
    await userEvent.type(screen.getByLabelText('Search keys'), 'zzz')
    expect(screen.getByTestId('draft-card')).toBeInTheDocument()
  })
})

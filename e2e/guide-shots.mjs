// Screenshots for the user guide of the features the numbered walkthrough does not cover: the protocols of a host (FTP, RDP,
// VNC), the protocol screen, Panel (Overview, Users, Incident, Services, Fleet), Run on hosts, the import of a cloud listing
// and the Kubernetes page. Against the real backend and two demo servers that answer like a small Ubuntu server (demoserver
// -linux). Started by guide-shots.sh. Names are not numbered, so the links of the docs do not shift.
import {chromium} from 'playwright'
import {spawn} from 'node:child_process'
import path from 'node:path'
import fs from 'node:fs'

const OUT = process.env.SHOT_DIR
const URL = process.env.VUTTY_URL
const DEMO = process.env.DEMOSERVER
const LISTING = process.env.LISTING
const MASTER = 'correct horse battery staple'
const sleep = ms => new Promise(r => setTimeout(r, ms))

const procs = ['web-01', 'db-01'].map((name, i) => spawn(DEMO, ['-addr', `127.0.0.1:${2203 + i}`, '-name', name, '-linux', '-sftp'], {stdio: 'ignore'}))
await sleep(600)

const browser = await chromium.launch({args: ['--disable-webgl', '--disable-3d-apis']})
const page = await (await browser.newContext({viewport: {width: 1360, height: 860}})).newPage()
page.setDefaultTimeout(15000)
page.on('dialog', d => void d.accept())
// the three window buttons of macOS, as in the numbered walkthrough
await page.addInitScript(() => {
  const draw = () => {
    if (document.getElementById('shot-traffic-lights')) return
    const box = document.createElement('div')
    box.id = 'shot-traffic-lights'
    box.setAttribute('aria-hidden', 'true')
    box.style.cssText = 'position:fixed;left:14px;top:16px;display:flex;gap:8px;z-index:2147483647;pointer-events:none'
    for (const c of ['#ff5f57', '#febc2e', '#28c840']) {
      const d = document.createElement('i')
      d.style.cssText = 'width:12px;height:12px;border-radius:50%;background:' + c
      box.appendChild(d)
    }
    document.body.appendChild(box)
  }
  if (document.body) draw()
  else document.addEventListener('DOMContentLoaded', draw)
})
const shot = async name => { await sleep(500); await page.screenshot({path: path.join(OUT, name + '.png')}); console.log('  screenshot', name) }
const L = label => page.getByLabel(label, {exact: true})
const nav = name => page.getByRole('navigation', {name: 'Main'}).getByRole('button', {name, exact: true})
const goHome = () => page.getByRole('tab', {name: 'Vaults'}).click()

// a host with a user name and password; returns once the New host form is filled, before Save or Connect
async function fillHost(label, port) {
  await L('New host').click()
  await L('Hostname').fill('127.0.0.1')
  await L('Label').fill(label)
  await L('Port').fill(String(port))
  await L('Username').fill('demo')
  await L('Password').fill('demo-password')
}
const form = () => page.getByRole('dialog', {name: 'New host'})
const openPanel = async label => {
  await goHome()
  await nav('Hosts').click()
  await page.getByRole('button', {name: new RegExp('^(Connect|Open) ' + label)}).first().dblclick().catch(async () => { await page.getByText(label, {exact: true}).first().dblclick() })
  await page.getByRole('radiogroup', {name: 'Protocol'}).waitFor()
  await page.getByRole('radio', {name: /Panel/}).click()
  await page.getByRole('button', {name: 'Continue'}).click()
  const trust = page.getByRole('alertdialog', {name: 'Host key confirmation'})
  if (await trust.waitFor({timeout: 6000}).then(() => true, () => false)) await trust.getByRole('button', {name: 'Trust and connect'}).click()
  await page.getByRole('region', {name: new RegExp('Server panel of ' + label)}).waitFor()
}

try {
  await page.goto(URL)
  await page.getByRole('form', {name: 'Create vault'}).waitFor()
  await L('Master password').fill(MASTER)
  await L('Confirm password').fill(MASTER)
  await page.getByRole('button', {name: 'Create vault'}).click()
  await page.getByPlaceholder(/Find a host or ssh user@hostname/).waitFor()

  // two hosts; the first one has every protocol
  await fillHost('web-01', 2203)
  await page.getByRole('button', {name: 'Add Telnet'}).click()
  await page.getByRole('button', {name: 'Add FTP'}).click()
  await page.getByRole('button', {name: 'Add Remote Desktop'}).click()
  await page.getByRole('button', {name: 'Add VNC'}).click()
  await L('FTP username').fill('web')
  await L('Remote Desktop username').fill('Administrator')
  await form().getByLabel('VNC port').scrollIntoViewIfNeeded()
  await shot('host-more-protocols')
  await form().getByRole('button', {name: 'Save', exact: true}).click()
  await form().waitFor({state: 'hidden'})
  await fillHost('db-01', 2204)
  await form().getByRole('button', {name: 'Save', exact: true}).click()
  await form().waitFor({state: 'hidden'})

  // the protocol screen of a host that has them all
  await page.getByText('web-01', {exact: true}).first().dblclick()
  await page.getByRole('radiogroup', {name: 'Protocol'}).waitFor()
  await shot('protocol-choice')
  await page.getByRole('radio', {name: /Panel/}).click()
  await page.getByRole('button', {name: 'Continue'}).click()
  const trust = page.getByRole('alertdialog', {name: 'Host key confirmation'})
  await trust.waitFor()
  await trust.getByRole('button', {name: 'Trust and connect'}).click()
  await page.getByRole('region', {name: /Server panel of web-01/}).waitFor()

  // Panel: Overview with the sections on the left, then the new sections
  await page.getByText(/Ubuntu 24\.04/).first().waitFor()
  await sleep(6500) // a second reading, for the charts and the network speed
  await shot('panel-overview')
  await page.getByRole('tab', {name: 'Services', exact: true}).click()
  await page.getByText('backup-sync').first().waitFor()
  await shot('panel-services')
  await page.getByRole('tab', {name: 'Users', exact: true}).click()
  await page.getByText('contractor').first().waitFor()
  await shot('panel-users')
  await page.getByRole('tab', {name: 'Incident', exact: true}).click()
  await L('Time range').selectOption('6h')
  await page.getByText(/backup-sync/).first().waitFor()
  await shot('panel-incident')

  // Fleet with both hosts (the second one has its key trusted by opening its panel once)
  await openPanel('db-01')
  await goHome()
  await nav('Fleet').click()
  await page.getByText('db-01').first().waitFor()
  await sleep(2500)
  await shot('fleet')

  // Run on hosts
  await nav('Snippets').click()
  await page.getByRole('button', {name: 'Run on hosts'}).first().click()
  const run = page.getByRole('dialog', {name: 'Run on hosts'})
  await run.getByLabel('Command').fill('hostname && uptime')
  await run.getByLabel('Select all shown').check()
  await run.getByRole('button', {name: /^Run on 2 hosts/}).click()
  await run.getByText(/Finished/).waitFor()
  await run.getByRole('button', {name: /^web-01/}).click().catch(() => {})
  await shot('run-on-hosts')
  await run.getByRole('button', {name: 'Close'}).last().click()

  // Import a cloud listing
  await nav('Import / Export').click()
  await page.getByRole('button', {name: 'Cloud and inventory'}).click()
  await page.getByText('How to make the file').click()
  await L('Inventory path').fill(LISTING)
  await L('Group name').fill('aws')
  await shot('import-cloud')
  await page.getByRole('button', {name: 'Import', exact: true}).click()
  await page.getByTestId('import-report').waitFor()
  await shot('import-cloud-report')
  await page.keyboard.press('Escape')

  // Kubernetes
  await nav('Kubernetes').click()
  await shot('kubernetes-page')
  await page.getByRole('button', {name: 'Add cluster'}).click()
  await shot('kubernetes-add-cluster')
  await page.keyboard.press('Escape')
} catch (e) {
  await page.screenshot({path: path.join(OUT, 'FAILURE.png')}).catch(() => {})
  console.error('✘ guide shots failed:', e)
  process.exitCode = 1
} finally {
  await browser.close()
  for (const p of procs) p.kill('SIGINT')
}

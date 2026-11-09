// The end-to-end run numbers its screenshots, so a new step shifts the numbers and the links of the docs go stale.
// This points every link to a numbered screenshot that is gone at the file with the same name and its new number.
//   node scripts/fix-shot-links.mjs
import fs from 'node:fs'
import path from 'node:path'

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
const dir = path.join(root, 'docs/assets/screenshots')
const byName = new Map(fs.readdirSync(dir).filter(f => /^\d+-.*\.png$/.test(f)).map(f => [f.replace(/^\d+-/, ''), f]))
const walk = d => fs.readdirSync(d, {withFileTypes: true}).flatMap(e => {
  const p = path.join(d, e.name)
  return e.isDirectory() ? (e.name === 'node_modules' ? [] : walk(p)) : p.endsWith('.md') ? [p] : []
})
for (const f of [path.join(root, 'README.md'), ...walk(path.join(root, 'docs'))]) {
  const s = fs.readFileSync(f, 'utf8')
  const t = s.replace(/(screenshots\/)(\d+-)([A-Za-z0-9-]+\.png)/g, (m, a, num, name) => {
    if (fs.existsSync(path.join(dir, num + name))) return m
    const now = byName.get(name)
    return now ? a + now : m
  })
  if (t !== s) { fs.writeFileSync(f, t); console.log('updated', path.relative(root, f)) }
}

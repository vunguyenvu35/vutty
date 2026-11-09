// Checks every relative link and image in README.md, the other top-level Markdown files and docs/**/*.md:
// the file must exist and, when the link has an #anchor, the target must have a heading that makes it.
// The end-to-end run numbers its screenshots, so a new step shifts the numbers and breaks image links: this tells which ones.
import fs from 'node:fs'
import path from 'node:path'

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
const walk = d => fs.readdirSync(d, {withFileTypes: true}).flatMap(e => {
  const p = path.join(d, e.name)
  return e.isDirectory() ? (e.name === 'node_modules' ? [] : walk(p)) : p.endsWith('.md') ? [p] : []
})
const files = [...fs.readdirSync(root).filter(f => f.endsWith('.md')).map(f => path.join(root, f)), ...walk(path.join(root, 'docs'))]

const slug = h => h.trim().toLowerCase().replace(/<[^>]+>/g, '').replace(/[`*_]/g, '').replace(/[^\p{L}\p{N}\s-]/gu, '').replace(/\s/g, '-')
const anchors = new Map()
const anchorsOf = f => {
  if (!anchors.has(f)) {
    const set = new Set()
    let fence = false
    for (const l of fs.readFileSync(f, 'utf8').split('\n')) {
      if (/^```/.test(l)) fence = !fence
      const m = !fence && /^#{1,6}\s+(.*)$/.exec(l)
      if (m) set.add(slug(m[1]))
    }
    anchors.set(f, set)
  }
  return anchors.get(f)
}

let bad = 0
for (const f of files) {
  let text = fs.readFileSync(f, 'utf8').replace(/```[\s\S]*?```/g, '').replace(/<!--[\s\S]*?-->/g, '')
  for (const m of text.matchAll(/(?:\]\(|(?:src|href)=")([^)"\s]+)/g)) {
    const t = m[1]
    if (/^(https?:|mailto:|#$)/.test(t)) continue
    if (path.dirname(f) === root && t.startsWith('../../')) continue // ../../releases and ../../issues are GitHub paths, not files
    const [file, anchor] = t.split('#')
    const target = file ? path.resolve(path.dirname(f), file) : f
    const rel = path.relative(root, f)
    if (!fs.existsSync(target)) { console.log(`broken link in ${rel}: ${t}`); bad = 1; continue }
    if (anchor && target.endsWith('.md') && !anchorsOf(target).has(anchor)) { console.log(`missing anchor in ${rel}: ${t}`); bad = 1 }
  }
}
process.exit(bad)

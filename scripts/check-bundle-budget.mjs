import { gzipSync } from 'node:zlib'
import { existsSync, readFileSync } from 'node:fs'
import { dirname, join, normalize, relative, resolve } from 'node:path'

const root = resolve(import.meta.dirname, '..')
const dist = join(root, 'dist')
const pages = [
  { name: 'documents', file: 'index.html', maxKiB: 130 },
  { name: 'history', file: 'history/index.html', maxKiB: 100 },
  { name: 'scans', file: 'scans/index.html', maxKiB: 100 },
  { name: 'ai', file: 'ai/index.html', maxKiB: 100 },
  { name: 'ai-config', file: 'ai/config/index.html', maxKiB: 100 },
  { name: 'ai-diagnostics', file: 'ai/diagnostics/index.html', maxKiB: 100 },
  { name: 'settings', file: 'settings/index.html', maxKiB: 100 },
  { name: 'html-preview', file: 'html-preview/index.html', maxKiB: 100 }
]

const forbiddenInitialModules = ['mermaid', 'marked', 'purify', 'html-preview']
let failed = false

for (const page of pages) {
  const htmlPath = join(dist, page.file)
  if (!existsSync(htmlPath)) {
    fail(`${page.name}: missing ${page.file}`)
    continue
  }
  const html = readFileSync(htmlPath, 'utf8')
  const entries = htmlModuleEntries(html)
  const modules = collectStaticModules(entries)
  const inlineBytes = [...html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/gi)].reduce(
    (total, match) => total + gzipSync(match[1]).length,
    0
  )
  const moduleBytes = [...modules].reduce((total, file) => total + gzipSync(readFileSync(file)).length, 0)
  const totalKiB = (inlineBytes + moduleBytes) / 1024

  if (totalKiB > page.maxKiB) {
    fail(`${page.name}: ${totalKiB.toFixed(1)} KiB gzip exceeds ${page.maxKiB} KiB`)
  }

  if (page.name !== 'html-preview') {
    const forbidden = [...modules]
      .map((file) => relative(dist, file))
      .filter((file) => forbiddenInitialModules.some((term) => file.toLowerCase().includes(term)))
    if (forbidden.length) fail(`${page.name}: forbidden initial modules: ${forbidden.join(', ')}`)
  }

  if (page.name === 'html-preview' && [...modules].some((file) => /\bApp\.[^.]+\.js$/.test(file))) {
    fail('html-preview: standalone page unexpectedly loads the shared application bundle')
  }

  console.log(`${page.name.padEnd(15)} ${totalKiB.toFixed(1).padStart(6)} KiB gzip  ${modules.size} modules`)
}

if (failed) process.exit(1)

function htmlModuleEntries(html) {
  const entries = new Set()
  for (const match of html.matchAll(/(?:component-url|renderer-url|src)="([^"]+\.js)"/g)) {
    const file = fromPublicPath(match[1])
    if (existsSync(file)) entries.add(file)
  }
  return entries
}

function collectStaticModules(entries) {
  const found = new Set()
  const queue = [...entries]
  while (queue.length) {
    const file = queue.pop()
    if (!file || found.has(file)) continue
    found.add(file)
    const source = readFileSync(file, 'utf8')
    for (const match of source.matchAll(/(?:\bfrom\s*|\bimport\s*)["']([^"']+\.js)["']/g)) {
      const dependency = normalize(join(dirname(file), match[1]))
      if (dependency.startsWith(dist) && existsSync(dependency) && !found.has(dependency)) queue.push(dependency)
    }
  }
  return found
}

function fromPublicPath(value) {
  return join(dist, value.replace(/^\//, ''))
}

function fail(message) {
  failed = true
  console.error(`bundle budget: ${message}`)
}

import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import puppeteer from 'puppeteer'
import { renderMermaid } from '@mermaid-js/mermaid-cli'
import { hash, loadCache, reusable, writeChanged } from './render-cache.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const output = path.resolve(process.argv[2] || path.join(root, 'output/architecture/rendered'))
const sourceDirectory = path.resolve(process.argv[3] || path.join(root, 'docs/architecture/generated'))
const executablePath = process.env.PUPPETEER_EXECUTABLE_PATH ||
  (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined)
const settings = { mermaidConfig: { securityLevel: 'strict', theme: 'neutral' } }
const rendererHash = hash(Buffer.concat(await Promise.all(
  ['render.mjs', 'render-cache.mjs', 'package-lock.json'].map(name =>
    fs.readFile(new URL(name, import.meta.url)))
)))
const manifest = JSON.parse(await fs.readFile(path.join(sourceDirectory, 'manifest.json'), 'utf8'))
const views = Object.keys(manifest.views).sort()
if (!views.length || views.some(view => !/^[a-z][a-z-]*$/.test(view))) {
  throw new Error('Invalid architecture view manifest')
}
const previous = await loadCache(output)
const current = {}
let browser
try {
  await fs.mkdir(output, { recursive: true })
  for (const view of views) {
    const source = await fs.readFile(path.join(sourceDirectory, `${view}.mmd`), 'utf8')
    const inputHash = hash(JSON.stringify({ source, rendererHash, settings, executablePath }))
    if (await reusable(output, view, inputHash, previous[view])) {
      current[view] = previous[view]
      console.log(`Unchanged ${view}.svg`)
      continue
    }
    browser ||= await puppeteer.launch({
      executablePath,
      headless: executablePath ? true : 'shell',
      args: process.env.CI ? ['--no-sandbox'] : []
    })
    const { data } = await renderMermaid(browser, source, 'svg', settings)
    if (data.byteLength < 1000) throw new Error(`Blank diagram: ${view}`)
    await writeChanged(path.join(output, `${view}.svg`), data)
    current[view] = { input_hash: inputHash, output_hash: hash(data) }
    console.log(`Rendered ${view}.svg (${data.byteLength} bytes)`)
  }
  // Remove only renderer-owned views from the previous manifest, never other files.
  for (const view of Object.keys(previous)) {
    if (!current[view] && /^[a-z][a-z-]*$/.test(view)) {
      await fs.rm(path.join(output, `${view}.svg`), { force: true })
    }
  }
  await writeChanged(path.join(output, 'render-manifest.json'),
    JSON.stringify({ schema_version: 1, views: current }, null, 2) + '\n')
} finally {
  if (browser) await browser.close()
}

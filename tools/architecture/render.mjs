import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import puppeteer from 'puppeteer'
import { renderMermaid } from '@mermaid-js/mermaid-cli'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const output = path.resolve(process.argv[2] || path.join(root, 'output/architecture/rendered'))
const sourceDirectory = path.resolve(process.argv[3] || path.join(root, 'docs/architecture/generated'))
const executablePath = process.env.PUPPETEER_EXECUTABLE_PATH ||
  (process.platform === 'darwin' ? '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' : undefined)
const browser = await puppeteer.launch({
  executablePath,
  headless: executablePath ? true : 'shell',
  args: process.env.CI ? ['--no-sandbox'] : []
})
try {
  await fs.mkdir(output, { recursive: true })
  for (const view of ['system', 'backend', 'frontend', 'agents']) {
    const source = await fs.readFile(path.join(sourceDirectory, `${view}.mmd`), 'utf8')
    const { data } = await renderMermaid(browser, source, 'svg', {
      mermaidConfig: { securityLevel: 'strict', theme: 'neutral' }
    })
    if (data.byteLength < 1000) throw new Error(`Blank diagram: ${view}`)
    await fs.writeFile(path.join(output, `${view}.svg`), data)
    console.log(`Rendered ${view}.svg (${data.byteLength} bytes)`)
  }
} finally {
  await browser.close()
}

import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { hash, loadCache, reusable, writeChanged } from './render-cache.mjs'

test('missing or corrupt manifest never claims a cache hit', async t => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'architecture-render-'))
  t.after(() => fs.rm(dir, { recursive: true, force: true }))
  assert.deepEqual(await loadCache(dir), {})
  await fs.writeFile(path.join(dir, 'render-manifest.json'), 'truncated')
  assert.deepEqual(await loadCache(dir), {})
  assert.equal(await reusable(dir, 'backend', 'input', undefined), false)
})

test('cached view requires matching source, toolchain and actual SVG bytes', async t => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'architecture-render-'))
  t.after(() => fs.rm(dir, { recursive: true, force: true }))
  const svg = `<svg>${'x'.repeat(1000)}</svg>`
  const entry = { input_hash: 'input', output_hash: hash(svg) }
  const file = path.join(dir, 'backend.svg')
  await fs.writeFile(file, svg)
  assert.equal(await reusable(dir, 'backend', 'input', entry), true)
  assert.equal(await reusable(dir, 'backend', 'changed', entry), false)
  await fs.writeFile(file, `${svg}tampered`)
  assert.equal(await reusable(dir, 'backend', 'input', entry), false)
  await fs.writeFile(file, 'blank')
  assert.equal(await reusable(dir, 'backend', 'input', entry), false)
})

test('unchanged writes preserve timestamp and failed/missing SVG is not reusable', async t => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'architecture-render-'))
  t.after(() => fs.rm(dir, { recursive: true, force: true }))
  const file = path.join(dir, 'backend.svg')
  const data = Buffer.from(`<svg>${'x'.repeat(1000)}</svg>`)
  assert.equal(await writeChanged(file, data), true)
  const before = await fs.stat(file, { bigint: true })
  assert.equal(await writeChanged(file, data), false)
  assert.equal((await fs.stat(file, { bigint: true })).mtimeNs, before.mtimeNs)
  await fs.rm(file)
  assert.equal(await reusable(dir, 'backend', 'input',
    { input_hash: 'input', output_hash: hash(data) }), false)
})

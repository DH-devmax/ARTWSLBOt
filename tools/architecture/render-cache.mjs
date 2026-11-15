import { createHash } from 'node:crypto'
import fs from 'node:fs/promises'
import path from 'node:path'

export const hash = value => createHash('sha256').update(value).digest('hex')

export async function loadCache(output) {
  try {
    const value = JSON.parse(await fs.readFile(path.join(output, 'render-manifest.json'), 'utf8'))
    return value.schema_version === 1 && value.views ? value.views : {}
  } catch (error) {
    if (error.code === 'ENOENT' || error instanceof SyntaxError) return {}
    throw error
  }
}

export async function reusable(output, view, inputHash, entry) {
  if (!entry || entry.input_hash !== inputHash) return false
  try {
    const data = await fs.readFile(path.join(output, `${view}.svg`))
    return data.byteLength >= 1000 && hash(data) === entry.output_hash
  } catch (error) {
    if (error.code === 'ENOENT') return false
    throw error
  }
}

export async function writeChanged(file, data) {
  try {
    if (Buffer.from(await fs.readFile(file)).equals(Buffer.from(data))) return false
  } catch (error) {
    if (error.code !== 'ENOENT') throw error
  }
  const temp = `${file}.${process.pid}.tmp`
  try {
    await fs.writeFile(temp, data)
    await fs.rename(temp, file)
  } finally {
    await fs.rm(temp, { force: true })
  }
  return true
}

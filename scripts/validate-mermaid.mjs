#!/usr/bin/env node
//
// Validate every ```mermaid block in docs/*.md by passing each one through
// the mermaid CLI (mmdc). Prints a summary of which diagrams parse cleanly
// and which fail.
//
// Usage:
//   node scripts/validate-mermaid.mjs

import { readFile, readdir, mkdir, writeFile, rm } from 'node:fs/promises'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'

const __dirname = dirname(fileURLToPath(import.meta.url))
const DOCS_DIR = join(__dirname, '..', 'docs')
const TMP_DIR = join(__dirname, 'tmp-mermaid')
const MMDC = join(__dirname, 'node_modules', '.bin', 'mmdc')

async function extractBlocks(filePath) {
  const text = await readFile(filePath, 'utf8')
  const lines = text.split('\n')
  const blocks = []
  let current = null
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (line.trim() === '```mermaid') {
      current = { start: i + 1, end: -1, body: [] }
    } else if (current && line.trim() === '```') {
      current.end = i + 1
      blocks.push(current)
      current = null
    } else if (current) {
      current.body.push(line)
    }
  }
  return blocks
}

async function validateBlock(filePath, block, idx) {
  const inputFile = join(TMP_DIR, `block.mmd`)
  const outputFile = join(TMP_DIR, `block.svg`)
  await writeFile(inputFile, block.body.join('\n'))
  try {
    execFileSync(MMDC, ['-i', inputFile, '-o', outputFile, '-q'], {
      stdio: 'pipe',
      timeout: 30_000,
    })
    return { ok: true }
  } catch (e) {
    const stderr = e.stderr ? e.stderr.toString() : ''
    const stdout = e.stdout ? e.stdout.toString() : ''
    return { ok: false, error: (stderr || stdout || e.message).split('\n').slice(0, 8).join('\n') }
  }
}

async function main() {
  await mkdir(TMP_DIR, { recursive: true })

  const files = (await readdir(DOCS_DIR))
    .filter((f) => f.endsWith('.md'))
    .map((f) => join(DOCS_DIR, f))

  let totalOk = 0
  let totalFail = 0

  for (const file of files) {
    const blocks = await extractBlocks(file)
    if (blocks.length === 0) continue
    const rel = file.replace(DOCS_DIR + '/', '')
    console.log(`\n=== ${rel} (${blocks.length} diagram(s)) ===`)
    for (let i = 0; i < blocks.length; i++) {
      const block = blocks[i]
      const result = await validateBlock(file, block, i + 1)
      if (result.ok) {
        console.log(`  ✓ block #${i + 1} (lines ${block.start}-${block.end})`)
        totalOk++
      } else {
        console.log(`  ✗ block #${i + 1} (lines ${block.start}-${block.end})`)
        console.log(result.error.split('\n').map((l) => '       ' + l).join('\n'))
        totalFail++
      }
    }
  }

  await rm(TMP_DIR, { recursive: true, force: true })

  console.log(`\nTotal: ${totalOk} OK, ${totalFail} failed`)
  if (totalFail > 0) process.exit(1)
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})

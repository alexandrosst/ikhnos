// Runs the plain-logic tests in tests/: each file is bundled with esbuild (so it can import from src/ with the `@` alias) and run in node.
//
//   node scripts/unit.mjs [name ...]     every tests/*.test.ts, or only those whose file name contains a given word
//
// The bundle is written next to package.json (a test may read files relative to the repository), and removed afterwards.
import { build } from 'esbuild'
import { spawnSync } from 'node:child_process'
import { readdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const words = process.argv.slice(2)
const files = readdirSync(path.join(root, 'tests'))
  .filter((f) => f.endsWith('.test.ts') && (words.length === 0 || words.some((w) => f.includes(w))))
  .sort()

let failed = 0
for (const [i, file] of files.entries()) {
  const out = path.join(root, `.tmp-unit-${i}.mjs`)
  try {
    await build({ entryPoints: [path.join(root, 'tests', file)], bundle: true, platform: 'node', format: 'esm', alias: { '@': path.join(root, 'src') }, outfile: out, logLevel: 'error' })
    const run = spawnSync(process.execPath, [out], { cwd: root, stdio: 'inherit' })
    if (run.status !== 0) { failed++; console.error(`FAIL ${file}`) }
  } catch {
    failed++
    console.error(`FAIL ${file} (did not build)`)
  } finally {
    rmSync(out, { force: true })
  }
}
console.log(`${files.length - failed}/${files.length} unit test files passed`)
process.exit(failed === 0 ? 0 : 1)

import { readFile } from 'node:fs/promises'
import { isAbsolute, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

const distDir = resolve(fileURLToPath(new URL('../dist', import.meta.url)))
const manifest = await readJson('.vite/manifest.json')
const evidence = await readJson('.vite/module-evidence.json')

assertRecord(manifest, 'Vite manifest')
assertRecord(evidence, 'module evidence')
assert(evidence.version === 1, 'module evidence version must be 1')
assert(Array.isArray(evidence.chunks), 'module evidence chunks must be an array')

for (const [key, chunk] of Object.entries(manifest)) {
  assertRecord(chunk, `manifest chunk ${key}`)
  assert(typeof chunk.file === 'string' && chunk.file.length > 0, `manifest chunk ${key} must have a file`)
  validateRelativeFile(chunk.file)
  validateStringArray(chunk.imports, `manifest chunk ${key} imports`)
  validateStringArray(chunk.css, `manifest chunk ${key} css`)
}

const evidenceByFile = new Map()
for (const [index, chunk] of evidence.chunks.entries()) {
  assertRecord(chunk, `module evidence chunk ${index}`)
  assert(typeof chunk.file === 'string' && chunk.file.length > 0, `module evidence chunk ${index} must have a file`)
  validateRelativeFile(chunk.file)
  assert(Array.isArray(chunk.modules), `module evidence chunk ${chunk.file} modules must be an array`)
  assert(chunk.modules.every((moduleId) => typeof moduleId === 'string'), `module evidence chunk ${chunk.file} modules must contain strings`)
  assert(!evidenceByFile.has(chunk.file), `duplicate module evidence for ${chunk.file}`)
  evidenceByFile.set(chunk.file, new Set(chunk.modules))
}

const sources = {
  dashboard: 'src/views/DashboardView.vue',
  team: 'src/views/TeamOverviewView.vue',
  login: 'src/views/LoginView.vue',
  oauthAuthorize: 'src/views/oauth/AuthorizePage.vue',
  oauthDevice: 'src/views/oauth/DevicePage.vue',
  adminUsers: 'src/views/admin/AdminUsersView.vue',
  english: 'src/locales/en-US.ts',
  chinese: 'src/locales/zh-CN.ts',
  elementChinese: 'node_modules/element-plus/es/locale/lang/zh-cn.mjs',
  workItemsStore: 'src/stores/workItems.ts',
  workItemsApi: 'src/api/workItems.ts',
  lineCanvas: 'src/components/charts/LineChartCanvas.vue',
  doughnutCanvas: 'src/components/charts/DoughnutChartCanvas.vue',
}

const app = uniqueManifestEntry(
  'app entry',
  (_key, chunk) => chunk.isEntry === true && chunk.src === 'index.html',
)
const dashboard = manifestEntryForSource(sources.dashboard)
const team = manifestEntryForSource(sources.team)
const login = manifestEntryForSource(sources.login)
const oauthAuthorize = manifestEntryForSource(sources.oauthAuthorize)
const oauthDevice = manifestEntryForSource(sources.oauthDevice)
const adminUsers = manifestEntryForSource(sources.adminUsers)
const english = manifestEntryForSource(sources.english)
const chinese = manifestEntryForSource(sources.chinese)
const lineCanvas = manifestEntryForSource(sources.lineCanvas)
const doughnutCanvas = manifestEntryForSource(sources.doughnutCanvas)

for (const entry of [dashboard, team, login, oauthAuthorize, oauthDevice, adminUsers, english, chinese, lineCanvas, doughnutCanvas]) {
  assert(entry.chunk.isDynamicEntry === true, `${entry.chunk.src} must be a dynamic entry`)
}

const entryClosure = staticClosure([app.key])
const dashboardClosure = staticClosure([dashboard.key])
const teamClosure = staticClosure([team.key])
const loginClosure = staticClosure([login.key])
const oauthAuthorizeClosure = staticClosure([oauthAuthorize.key])
const oauthDeviceClosure = staticClosure([oauthDevice.key])
const adminUsersClosure = staticClosure([app.key, adminUsers.key, english.key])
const usageClosure = staticClosure([app.key, dashboard.key, english.key])
const englishClosure = staticClosure([english.key])
const chineseClosure = staticClosure([chinese.key])
const lineCanvasClosure = staticClosure([lineCanvas.key])
const doughnutCanvasClosure = staticClosure([doughnutCanvas.key])
const canvasClosure = new Set([...lineCanvasClosure, ...doughnutCanvasClosure])

const entryModules = modulesForClosure(entryClosure)
const dashboardModules = modulesForClosure(dashboardClosure)
const teamModules = modulesForClosure(teamClosure)
const loginModules = modulesForClosure(loginClosure)
const oauthAuthorizeModules = modulesForClosure(oauthAuthorizeClosure)
const oauthDeviceModules = modulesForClosure(oauthDeviceClosure)
const usageModules = modulesForClosure(usageClosure)
const lineCanvasModules = modulesForClosure(lineCanvasClosure)
const doughnutCanvasModules = modulesForClosure(doughnutCanvasClosure)
const canvasModules = modulesForClosure(canvasClosure)

assertAbsent(
  entryModules,
  [sources.english, sources.chinese, sources.workItemsStore, sources.workItemsApi],
  'initial entry static closure',
)
for (const [label, modules] of [
  ['initial entry static closure', entryModules],
  ['Dashboard static closure', dashboardModules],
  ['Team Overview static closure', teamModules],
]) {
  assertNoCanvasOrChartRuntime(modules, label)
}

assert(lineCanvasModules.has(sources.lineCanvas), 'line canvas closure must contain its source module')
assert(doughnutCanvasModules.has(sources.doughnutCanvas), 'doughnut canvas closure must contain its source module')
assert([...canvasModules].some(isChartJsModule), 'canvas closures must contain Chart.js modules')
assert([...canvasModules].some(isVueChartJsModule), 'canvas closures must contain vue-chartjs modules')

assert(usageModules.has(sources.english), 'default English /usage closure must contain en-US')
assertAbsent(
  usageModules,
  [
    sources.chinese,
    sources.elementChinese,
    sources.lineCanvas,
    sources.doughnutCanvas,
  ],
  'default English /usage closure',
)
assertNoChartRuntime(usageModules, 'default English /usage closure')

assert(
  [...loginModules].some(isElementPlusModule),
  'Login static closure must contain on-demand Element Plus modules',
)

for (const [file, modules] of evidenceByFile) {
  const fullLibraryModule = [...modules].find(isElementPlusFullLibraryModule)
  assert(!fullLibraryModule, `chunk ${file} must not contain full Element Plus entry ${fullLibraryModule}`)
}

const canvasChunkFiles = jsFilesForClosure(canvasClosure)
const chartRuntimeFiles = new Set()
for (const [file, modules] of evidenceByFile) {
  if (![...modules].some(isChartRuntimeModule)) continue
  assert(canvasChunkFiles.has(file), `chart runtime chunk ${file} must belong to a canvas static closure`)
  chartRuntimeFiles.add(file)
  for (const chunk of Object.values(manifest)) {
    if (chunk.file !== file) continue
    for (const cssFile of chunk.css ?? []) chartRuntimeFiles.add(cssFile)
  }
}
assert(chartRuntimeFiles.size > 0, 'at least one emitted Chart.js/vue-chartjs runtime file is required')

const indexHtml = await readEmittedFile('index.html')
assert(!indexHtml.toString('utf8').includes('/vite.svg'), 'emitted index.html must not contain /vite.svg')

const initialShellRow = await measure('Initial shell', filesForClosure(entryClosure, true))
const defaultUsageRow = await measure('Default English `/usage`', filesForClosure(usageClosure, true))
const adminUsersRow = await measure('Default English `/admin/users`', filesForClosure(adminUsersClosure, true))
const rows = [
  initialShellRow,
  defaultUsageRow,
  adminUsersRow,
  await measure('`en-US`', filesForClosure(englishClosure)),
  await measure('`zh-CN`', filesForClosure(chineseClosure)),
  await measure('Line canvas', filesForClosure(lineCanvasClosure)),
  await measure('Doughnut canvas', filesForClosure(doughnutCanvasClosure)),
  await measure('Chart runtime', chartRuntimeFiles),
]

// Keep sub-percent headroom above the Node 20 CI baseline so zlib patch-level
// output differences do not masquerade as application bundle regressions.
for (const [row, maximum] of [
  [initialShellRow, 73_000],
  [defaultUsageRow, 159_000],
  [adminUsersRow, 253_909],
]) {
  assert(row.gzip <= maximum, `${row.label} gzip ${row.gzip} exceeds budget ${maximum}`)
}

console.log('| Aggregate | Raw bytes | Gzip bytes |')
console.log('| --- | ---: | ---: |')
for (const row of rows) {
  console.log(`| ${row.label} | ${row.raw} | ${row.gzip} |`)
}
console.log('')
console.log('Structural assertions: PASS')

function assert(condition, message) {
  if (!condition) throw new Error(message)
}

function assertRecord(value, label) {
  assert(value !== null && typeof value === 'object' && !Array.isArray(value), `${label} must be an object`)
}

function validateStringArray(value, label) {
  if (value === undefined) return
  assert(Array.isArray(value) && value.every((item) => typeof item === 'string'), `${label} must be an array of strings`)
}

async function readJson(file) {
  const bytes = await readEmittedFile(file)
  try {
    return JSON.parse(bytes.toString('utf8'))
  } catch (error) {
    throw new Error(`cannot parse ${file}: ${error.message}`, { cause: error })
  }
}

function validateRelativeFile(file) {
  const normalized = file.replaceAll('\\', '/')
  assert(normalized === file, `emitted file must use normalized separators: ${file}`)
  assert(!isAbsolute(file) && !/^[A-Za-z]:\//.test(file), `emitted file must be relative: ${file}`)
  const segments = file.split('/')
  assert(!segments.includes('..') && segments.every(Boolean), `emitted file must stay beneath dist: ${file}`)
  return file
}

function emittedFilePath(file) {
  validateRelativeFile(file)
  const path = resolve(distDir, ...file.split('/'))
  assert(path.startsWith(`${distDir}${sep}`), `emitted file must stay beneath dist: ${file}`)
  return path
}

function readEmittedFile(file) {
  return readFile(emittedFilePath(file))
}

function uniqueManifestEntry(label, predicate) {
  const matches = Object.entries(manifest)
    .filter(([key, chunk]) => predicate(key, chunk))
    .map(([key, chunk]) => ({ key, chunk }))
  assert(matches.length === 1, `${label} must have exactly one manifest entry, found ${matches.length}`)
  return matches[0]
}

function manifestEntryForSource(source) {
  return uniqueManifestEntry(source, (_key, chunk) => chunk.src === source)
}

function staticClosure(seedKeys) {
  const visited = new Set()
  const pending = [...seedKeys]
  while (pending.length > 0) {
    const key = pending.pop()
    if (visited.has(key)) continue
    const chunk = manifest[key]
    assertRecord(chunk, `manifest import ${key}`)
    visited.add(key)
    for (const importedKey of chunk.imports ?? []) pending.push(importedKey)
  }
  return visited
}

function modulesForClosure(closure) {
  const modules = new Set()
  for (const key of closure) {
    const file = manifest[key].file
    const chunkModules = evidenceByFile.get(file)
    assert(chunkModules, `missing module evidence for ${file}`)
    for (const moduleId of chunkModules) modules.add(moduleId)
  }
  return modules
}

function jsFilesForClosure(closure) {
  return new Set([...closure].map((key) => manifest[key].file))
}

function filesForClosure(closure, includeHtml = false) {
  const files = jsFilesForClosure(closure)
  for (const key of closure) {
    for (const cssFile of manifest[key].css ?? []) files.add(cssFile)
  }
  if (includeHtml) files.add('index.html')
  return files
}

function assertAbsent(modules, forbiddenModules, label) {
  for (const moduleId of forbiddenModules) {
    assert(!modules.has(moduleId), `${label} must not contain ${moduleId}`)
  }
}

function assertNoCanvasOrChartRuntime(modules, label) {
  assertAbsent(modules, [sources.lineCanvas, sources.doughnutCanvas], label)
  assertNoChartRuntime(modules, label)
}

function assertNoChartRuntime(modules, label) {
  const found = [...modules].find(isChartRuntimeModule)
  assert(!found, `${label} must not contain chart runtime module ${found}`)
}

function isChartJsModule(moduleId) {
  return moduleId === 'node_modules/chart.js' || moduleId.startsWith('node_modules/chart.js/')
}

function isVueChartJsModule(moduleId) {
  return moduleId === 'node_modules/vue-chartjs' || moduleId.startsWith('node_modules/vue-chartjs/')
}

function isChartRuntimeModule(moduleId) {
  return isChartJsModule(moduleId) || isVueChartJsModule(moduleId)
}

function isElementPlusModule(moduleId) {
  return moduleId === 'node_modules/element-plus' || moduleId.startsWith('node_modules/element-plus/')
}

function isElementPlusFullLibraryModule(moduleId) {
  return moduleId === 'node_modules/element-plus/es/index.mjs'
    || moduleId === 'node_modules/element-plus/lib/index.js'
    || moduleId === 'node_modules/element-plus/dist/index.full.mjs'
}

async function measure(label, files) {
  assert(files.size > 0, `${label} must include at least one emitted file`)
  let raw = 0
  let gzip = 0
  for (const file of [...files].sort()) {
    const bytes = await readEmittedFile(file)
    raw += bytes.length
    gzip += gzipSync(bytes).length
  }
  assert(gzip < raw, `${label} gzip bytes must be smaller than raw bytes (${gzip} >= ${raw})`)
  return { label, raw, gzip }
}

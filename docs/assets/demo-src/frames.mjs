// usage: node frames.mjs <demo.html> <outDir>   (chrome must listen on :9333)
// Writes one PNG per frame plus an ffmpeg concat list with per-frame durations.
import { writeFileSync } from 'node:fs'
const [page, outDir] = process.argv.slice(2)
const tabs = await (await fetch('http://127.0.0.1:9333/json')).json()
const ws = new WebSocket(tabs.find((t) => t.type === 'page').webSocketDebuggerUrl)
await new Promise((r) => (ws.onopen = r))
let id = 0
const pending = new Map()
ws.onmessage = (m) => {
  const d = JSON.parse(m.data)
  if (pending.has(d.id)) pending.get(d.id)(d.result)
}
const send = (method, params = {}) =>
  new Promise((r) => {
    pending.set(++id, r)
    ws.send(JSON.stringify({ id, method, params }))
  })
const evaluate = async (expression) => (await send('Runtime.evaluate', { expression, returnByValue: true })).result.value
await send('Emulation.setDeviceMetricsOverride', { width: 940, height: 520, deviceScaleFactor: 1, mobile: false })
await send('Page.navigate', { url: `file://${page}` })
await new Promise((r) => setTimeout(r, 800))
const n = await evaluate('window.frameCount')
let list = ''
for (let k = 0; k < n; k++) {
  await evaluate(`window.render(${k})`)
  const { data } = await send('Page.captureScreenshot', { format: 'png' })
  const name = `f${String(k).padStart(4, '0')}.png`
  writeFileSync(`${outDir}/${name}`, Buffer.from(data, 'base64'))
  list += `file '${name}'\nduration ${(await evaluate(`window.frameMs(${k})`)) / 1000}\n`
  if (k === n - 1) list += `file '${name}'\n` // concat demuxer needs the last file repeated
}
writeFileSync(`${outDir}/list.txt`, list)
console.log('frames', n)
ws.close()

/**
 * LSM Engine — demo GIF capture script
 * Usage: node scripts/capture-demo.mjs [--frames-dir /tmp/lsm-frames]
 *
 * Assumes the backend is already running on :8080 and the BFF on :3001.
 * Captures frames while executing a scripted tour of the 7 dashboard panels,
 * then hands off to ffmpeg (called by the wrapper shell script).
 */

import { chromium } from '/opt/homebrew/lib/node_modules/playwright/index.mjs';
import { writeFile, mkdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import { join } from 'node:path';

const FRAMES_DIR = process.argv[2] ?? '/tmp/lsm-demo-frames';
const BACKEND    = 'http://localhost:8080';
const DASHBOARD  = 'http://localhost:3001';
const FPS        = 12;
const FRAME_MS   = Math.round(1000 / FPS);
const WIDTH      = 1280;
const HEIGHT     = 800;

// ─── helpers ───────────────────────────────────────────────────────────────

let frameIndex = 0;
let captureHandle = null;

async function startCapture(page) {
  captureHandle = setInterval(async () => {
    const name = `frame_${String(frameIndex++).padStart(5, '0')}.png`;
    const buf = await page.screenshot({ type: 'png' });
    await writeFile(join(FRAMES_DIR, name), buf);
  }, FRAME_MS);
}

function stopCapture() {
  clearInterval(captureHandle);
  captureHandle = null;
}

async function hold(ms) {
  return new Promise(r => setTimeout(r, ms));
}

async function apiPut(key, value) {
  await fetch(`${BACKEND}/db/put`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ key, value }),
  }).catch(() => {});
}

async function apiBatch(ops) {
  // ops: [{op:'put'|'delete', key, value?}]  → backend: {entries:[{key,value,delete}]}
  const entries = ops.map(o => ({ key: o.key, value: o.value ?? '', delete: o.op === 'delete' }));
  await fetch(`${BACKEND}/db/batch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entries }),
  }).catch(() => {});
}

// ─── main ──────────────────────────────────────────────────────────────────

async function main() {
  if (!existsSync(FRAMES_DIR)) {
    await mkdir(FRAMES_DIR, { recursive: true });
  }

  console.log(`[capture] Launching browser → ${DASHBOARD}`);
  const browser = await chromium.launch({ headless: true });
  const page    = await browser.newPage();
  await page.setViewportSize({ width: WIDTH, height: HEIGHT });
  page.on('pageerror', e => console.error('[page error]', e.message));

  await page.goto(DASHBOARD, { waitUntil: 'networkidle', timeout: 20_000 });
  await page.waitForSelector('header', { timeout: 10_000 });
  await hold(1500); // let WebSocket connect and initial data load

  // ── ACT 0: Hold on the initial state (1.5 s) ────────────────────────────
  console.log('[capture] Act 0 — initial state');
  await startCapture(page);
  await hold(1500);

  // ── ACT 1: Type & PUT a key in the Command Deck (2 s) ───────────────────
  console.log('[capture] Act 1 — PUT key via UI');
  const keyInput   = page.locator('#wb-key');
  const valueInput = page.locator('#wb-value');
  const putBtn     = page.locator('button.term-btn.primary').first();

  await keyInput.click();
  await page.keyboard.type('engine:status', { delay: 60 });
  await hold(200);
  await valueInput.click();
  await page.keyboard.type('operational', { delay: 55 });
  await hold(200);
  await putBtn.click();
  await hold(1200);

  // ── ACT 2: Rapid batch writes via API (watch WAL + write-feed animate) ───
  console.log('[capture] Act 2 — batch writes');
  const keys = [
    ['lsm:compaction', 'leveled'],
    ['lsm:bloom_fpr',  '0.01'],
    ['lsm:block_size', '4096'],
    ['lsm:l0_limit',   '4'],
    ['lsm:l1_bytes',   '10485760'],
  ];
  for (const [k, v] of keys) {
    await apiPut(k, v);
    await hold(250);
  }
  // type another key in the UI while writes are flowing
  await keyInput.click({ clickCount: 3 });
  await page.keyboard.type('bench:run:1');
  await valueInput.click({ clickCount: 3 });
  await page.keyboard.type('start');
  await putBtn.click();
  await hold(800);

  // ── ACT 3: Burst of 20 rapid API writes to fill memtable bar ─────────────
  console.log('[capture] Act 3 — burst writes');
  const burstOps = Array.from({ length: 20 }, (_, i) => ({
    op: 'put',
    key: `burst:${String(i).padStart(3, '0')}`,
    value: `value-${Math.random().toString(36).slice(2)}`,
  }));
  await apiBatch(burstOps);
  await hold(800);

  // ── ACT 4: Read Inspector — trace a key ─────────────────────────────────
  console.log('[capture] Act 4 — read trace');
  const traceInput = page.locator('#ri-key');
  const traceBtn   = page.locator('button', { hasText: /trace read/i });
  if (await traceInput.count() > 0 && await traceBtn.count() > 0) {
    await traceInput.click();
    await page.keyboard.type('lsm:compaction', { delay: 50 });
    await traceBtn.click();
    await hold(1200);
  }

  // ── ACT 5: Compaction Studio — switch strategy, then Force L0 ─────────────
  console.log('[capture] Act 5 — compaction studio');
  const sizeTieredBtn = page.locator('button', { hasText: /size.tiered/i });
  if (await sizeTieredBtn.count() > 0) {
    await sizeTieredBtn.click();
    await hold(700);
    const leveledBtn = page.locator('button', { hasText: /^leveled$/i });
    if (await leveledBtn.count() > 0) {
      await leveledBtn.click();
      await hold(500);
    }
  }

  const forceL0 = page.locator('button', { hasText: /force l0/i });
  if (await forceL0.count() > 0) {
    await forceL0.click();
    await hold(1800); // let compaction events stream in
  }

  // ── ACT 6: More burst writes to show amplification gauges move ───────────
  console.log('[capture] Act 6 — amplification watch');
  const burst2 = Array.from({ length: 30 }, (_, i) => ({
    op: i % 7 === 0 ? 'delete' : 'put',
    key: `amp:test:${String(i).padStart(3, '0')}`,
    value: `data-${i}`,
  }));
  await apiBatch(burst2);
  await hold(1500);

  // ── ACT 7: Scroll to Operations Lab + bench ─────────────────────────────
  console.log('[capture] Act 7 — operations lab');
  const benchBtn = page.locator('button', { hasText: /run bench/i });
  if (await benchBtn.count() > 0) {
    await benchBtn.scrollIntoViewIfNeeded();
    await hold(600);
    await benchBtn.click();
    await hold(3000); // bench takes a moment
  }

  // ── ACT 8: Scroll back up, hold on full dashboard (1.5 s) ────────────────
  console.log('[capture] Act 8 — return to top');
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'smooth' }));
  await hold(1500);

  stopCapture();
  await browser.close();

  console.log(`[capture] Done — ${frameIndex} frames written to ${FRAMES_DIR}`);
}

main().catch(e => { console.error('[fatal]', e); process.exit(1); });

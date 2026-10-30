// Shared constants of the pixel harness. Every path and address can be overridden by an environment variable.
import {existsSync, readFileSync} from 'node:fs';
import {dirname, join, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

export const PIXEL_DIR = dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = resolve(PIXEL_DIR, '..', '..');
// tests/pixel/.env (not committed) holds the local paths as KEY=value lines; real environment variables win.
const envFile = join(PIXEL_DIR, '.env');
if (existsSync(envFile)) {
  for (const line of readFileSync(envFile, 'utf8').split(/\r?\n/)) {
    const m = /^\s*([A-Z_]+)\s*=\s*(.*?)\s*$/.exec(line);
    if (m && !line.trim().startsWith('#') && !(m[1] in process.env)) process.env[m[1]] = m[2];
  }
}

export const PORT = Number(process.env.PIXEL_PORT || 3610);
export const HUB_URL = (process.env.PIXEL_HUB_URL || `http://127.0.0.1:${PORT}`).replace(/\/$/, '');
export const ADMIN = process.env.PIXEL_ADMIN || 'maya';
export const PASSWORD = process.env.PIXEL_PASSWORD || 'Pixel-Passw0rd-9137';
/** Where the .dc.html boards live (the design project). */
export const BOARDS_DIR = process.env.PIXEL_BOARDS_DIR || join(REPO_ROOT, 'design', 'project-v3');
/** The report, the screenshots and the seed state go here. */
export const OUT_DIR = resolve(process.env.PIXEL_REPORT_DIR || join(PIXEL_DIR, 'report'));
export const STATE_FILE = join(OUT_DIR, 'state.json');
/** The instant the seeded data is dated from, and the instant the browser clock is frozen at. */
export const ANCHOR = new Date('2026-10-30T14:00:00Z');
export const HUB_BIN = process.env.PIXEL_HUB_BIN || '';

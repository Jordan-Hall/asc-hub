// A port of pixelmatch (https://github.com/mapbox/pixelmatch, ISC licence, Copyright (c) 2019 Mapbox): the same
// YIQ colour-distance metric and the same anti-aliasing detection, working on raw RGBA buffers. It is vendored
// so the harness needs no package beyond the ones the hub already depends on (sharp decodes the PNG files).

const MAX_YIQ_DELTA = 35215; // 35215 is the maximum possible value for the YIQ difference metric

/**
 * @param {Uint8Array} img1 RGBA pixels of the first image
 * @param {Uint8Array} img2 RGBA pixels of the second image (same size)
 * @param {Uint8Array|null} output RGBA buffer for the diff image, or null
 * @returns {number} number of mismatched pixels
 */
export function pixelmatch(img1, img2, output, width, height, {threshold = 0.1, includeAA = false, alpha = 0.1, diffColor = [255, 0, 0], aaColor = [255, 255, 0]} = {}) {
  const maxDelta = MAX_YIQ_DELTA * threshold * threshold;
  let diff = 0;
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const pos = (y * width + x) * 4;
      const delta = colorDelta(img1, img2, pos, pos, false);
      if (Math.abs(delta) > maxDelta) {
        if (!includeAA && (antialiased(img1, x, y, width, height, img2) || antialiased(img2, x, y, width, height, img1))) {
          if (output) drawPixel(output, pos, ...aaColor);
        } else {
          if (output) drawPixel(output, pos, ...diffColor);
          diff++;
        }
      } else if (output) {
        const gray = blend(grayPixel(img1, pos), alpha * img1[pos + 3] / 255);
        drawPixel(output, pos, gray, gray, gray);
      }
    }
  }
  return diff;
}

function antialiased(img, x1, y1, width, height, img2) {
  const x0 = Math.max(x1 - 1, 0);
  const y0 = Math.max(y1 - 1, 0);
  const x2 = Math.min(x1 + 1, width - 1);
  const y2 = Math.min(y1 + 1, height - 1);
  const pos = (y1 * width + x1) * 4;
  let zeroes = x1 === x0 || x1 === x2 || y1 === y0 || y1 === y2 ? 1 : 0;
  let min = 0;
  let max = 0;
  let minX = 0, minY = 0, maxX = 0, maxY = 0;
  for (let x = x0; x <= x2; x++) {
    for (let y = y0; y <= y2; y++) {
      if (x === x1 && y === y1) continue;
      const delta = colorDelta(img, img, pos, (y * width + x) * 4, true);
      if (delta === 0) {
        zeroes++;
        if (zeroes > 2) return false;
      } else if (delta < min) {
        min = delta; minX = x; minY = y;
      } else if (delta > max) {
        max = delta; maxX = x; maxY = y;
      }
    }
  }
  if (min === 0 || max === 0) return false;
  return (hasManySiblings(img, minX, minY, width, height) && hasManySiblings(img2, minX, minY, width, height)) ||
         (hasManySiblings(img, maxX, maxY, width, height) && hasManySiblings(img2, maxX, maxY, width, height));
}

function hasManySiblings(img, x1, y1, width, height) {
  const x0 = Math.max(x1 - 1, 0);
  const y0 = Math.max(y1 - 1, 0);
  const x2 = Math.min(x1 + 1, width - 1);
  const y2 = Math.min(y1 + 1, height - 1);
  const pos = (y1 * width + x1) * 4;
  let zeroes = x1 === x0 || x1 === x2 || y1 === y0 || y1 === y2 ? 1 : 0;
  for (let x = x0; x <= x2; x++) {
    for (let y = y0; y <= y2; y++) {
      if (x === x1 && y === y1) continue;
      const pos2 = (y * width + x) * 4;
      if (img[pos] === img[pos2] && img[pos + 1] === img[pos2 + 1] && img[pos + 2] === img[pos2 + 2] && img[pos + 3] === img[pos2 + 3]) zeroes++;
      if (zeroes > 2) return true;
    }
  }
  return false;
}

function colorDelta(img1, img2, k, m, yOnly) {
  let r1 = img1[k], g1 = img1[k + 1], b1 = img1[k + 2];
  const a1 = img1[k + 3];
  let r2 = img2[m], g2 = img2[m + 1], b2 = img2[m + 2];
  const a2 = img2[m + 3];
  if (a1 === a2 && r1 === r2 && g1 === g2 && b1 === b2) return 0;
  if (a1 < 255) { const a = a1 / 255; r1 = blend(r1, a); g1 = blend(g1, a); b1 = blend(b1, a); }
  if (a2 < 255) { const a = a2 / 255; r2 = blend(r2, a); g2 = blend(g2, a); b2 = blend(b2, a); }
  const y1 = rgb2y(r1, g1, b1);
  const y2 = rgb2y(r2, g2, b2);
  const y = y1 - y2;
  if (yOnly) return y;
  const i = rgb2i(r1, g1, b1) - rgb2i(r2, g2, b2);
  const q = rgb2q(r1, g1, b1) - rgb2q(r2, g2, b2);
  const delta = 0.5053 * y * y + 0.299 * i * i + 0.1957 * q * q;
  return y1 > y2 ? -delta : delta;
}

const rgb2y = (r, g, b) => r * 0.29889531 + g * 0.58662247 + b * 0.11448223;
const rgb2i = (r, g, b) => r * 0.59597799 - g * 0.2741761 - b * 0.32180189;
const rgb2q = (r, g, b) => r * 0.21147017 - g * 0.52261711 + b * 0.31114694;
const blend = (c, a) => 255 + (c - 255) * a;
function grayPixel(img, i) {
  const a = img[i + 3] / 255;
  return rgb2y(blend(img[i], a), blend(img[i + 1], a), blend(img[i + 2], a));
}
function drawPixel(output, pos, r, g, b) {
  output[pos] = r; output[pos + 1] = g; output[pos + 2] = b; output[pos + 3] = 255;
}

import {exportFileName, formatSize, showExportBusy, verifySlices} from './asc-audit.js';

test('verifySlices verifies a short log in one post', () => {
  expect(verifySlices(1, 0)).toEqual([[0, 0]]);
  expect(verifySlices(1, 12, 2000)).toEqual([[0, 0]]);
  expect(verifySlices(1, 2000, 2000)).toEqual([[0, 0]]);
  expect(verifySlices(9, 3, 2000)).toEqual([[0, 0]]);
});

test('verifySlices covers every event once and ends at the head', () => {
  expect(verifySlices(1, 2001, 2000)).toEqual([[1, 2000], [2001, 0]]);
  expect(verifySlices(1, 10, 4)).toEqual([[1, 4], [5, 8], [9, 0]]);
  // After retention the slices start at the first event kept.
  expect(verifySlices(7, 16, 4)).toEqual([[7, 10], [11, 14], [15, 0]]);
});

test('exportFileName prefers the UTF-8 name and falls back', () => {
  expect(exportFileName(`attachment; filename="a.jsonl"; filename*=UTF-8''asc-audit-acme-20261104.jsonl`, 'x')).toBe('asc-audit-acme-20261104.jsonl');
  expect(exportFileName('attachment; filename="a \\"b\\".csv"', 'x')).toBe('a "b".csv');
  expect(exportFileName(`attachment; filename*=UTF-8''%E0%A4%A`, 'x')).toBe('x');
  expect(exportFileName(null, 'x')).toBe('x');
});

test('formatSize writes the size as the board does', () => {
  expect(formatSize(820, 'en')).toBe('820 B');
  expect(formatSize(8200, 'en')).toBe('8.2 kB');
  expect(formatSize(8_200_000, 'en')).toBe('8.2 MB');
  expect(formatSize(120_000_000, 'en')).toBe('120 MB');
});

test('showExportBusy spins the Export button and says the file is coming', () => {
  document.body.innerHTML = '<details open><summary class="ds-btn">Export</summary></details><span data-text="Preparing the export."></span>';
  const button = document.querySelector('summary');
  const status = document.querySelector('span');
  showExportBusy(button, status, true);
  expect(button.classList.contains('is-loading')).toBe(true);
  expect(status.textContent).toBe('Preparing the export.');
  showExportBusy(button, status, false);
  expect(button.classList.contains('is-loading')).toBe(false);
  expect(status.textContent).toBe('');
});

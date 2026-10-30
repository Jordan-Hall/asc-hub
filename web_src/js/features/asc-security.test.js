import {uuidv7, fill, stateName, showUndoToast} from './asc-security.js';

test('uuidv7 has the version and variant bits and the time in front', () => {
  const id = uuidv7(1700000000000, (n) => new Uint8Array(n).fill(0xff));
  expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(id.replaceAll('-', '').slice(0, 12)).toBe((1700000000000).toString(16).padStart(12, '0'));
  expect(uuidv7()).not.toBe(uuidv7());
});

test('fill puts words in the places and never reads markup', () => {
  expect(fill('%s will leave the %s list.', 'a.env:3', 'Open')).toBe('a.env:3 will leave the Open list.');
  expect(fill('%s', '<b>x</b>')).toBe('<b>x</b>');
  expect(fill('no places', 'x')).toBe('no places');
  expect(fill('%s and %s', 'one')).toBe('one and ');
});

test('stateName reads the data-i-state attribute of the state', () => {
  expect(stateName({iStateFalse_positive: 'False positive'}, 'false_positive')).toBe('False positive');
  expect(stateName({}, 'revoked')).toBe('revoked');
});

test('the toast says what happened as text, offers Undo, and replaces the one before', async () => {
  document.body.innerHTML = '';
  let undone = 0;
  showUndoToast({title: 'Finding set to <i>Revoked</i>', detail: 'x', undoLabel: 'Undo', onUndo: () => { undone++ }});
  const toast = showUndoToast({title: 'Finding set to <i>Revoked</i>', detail: 'config/prod.env:3', undoLabel: 'Undo', onUndo: () => { undone++ }});
  expect(document.querySelectorAll('.rv-undo-toast')).toHaveLength(1);
  expect(toast.getAttribute('role')).toBe('status');
  expect(toast.querySelector('b').textContent).toBe('Finding set to <i>Revoked</i>');
  expect(toast.querySelector('b i')).toBeNull();
  toast.querySelector('button').click();
  await Promise.resolve();
  expect(undone).toBe(1);
  expect(toast.querySelector('button').disabled).toBe(true);
});

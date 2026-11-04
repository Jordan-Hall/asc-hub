import {parseWhere, pointerText, readPointer, writeWhere} from './asc-where.js';

const heading = 'Where to look first';

test('parseWhere reads the pointers of the section up to the next heading', () => {
  const text = 'Intro\n\n### Where to look first\n\n- `src/a.rs:42` the retry loop\n* `b.go`\nnot a pointer\n\n### Tests\n\n- `c.rs:1` other';
  const {pointers, start, end} = parseWhere(text, heading);
  expect(pointers).toEqual([
    {path: 'src/a.rs', line: 42, note: 'the retry loop'},
    {path: 'b.go', line: 0, note: ''},
  ]);
  expect(text.slice(start, end).startsWith('### Where to look first')).toBe(true);
  expect(text.slice(end).startsWith('### Tests')).toBe(true);
});

test('parseWhere finds no section when the heading is missing', () => {
  expect(parseWhere('- `a.rs:1` x', heading)).toEqual({pointers: [], start: -1, end: -1});
});

test('pointerText leaves out a zero line', () => {
  expect(pointerText({path: 'a.rs', line: 3})).toBe('a.rs:3');
  expect(pointerText({path: 'a.rs', line: 0})).toBe('a.rs');
});

test('writeWhere adds the section at the end', () => {
  expect(writeWhere('Body\n\n', heading, [{path: 'a.rs', line: 3, note: 'x'}])).toBe(
    'Body\n\n### Where to look first\n\n- `a.rs:3` x\n',
  );
  expect(writeWhere('', heading, [{path: 'a.rs', line: 0, note: ''}])).toBe('### Where to look first\n\n- `a.rs`\n');
});

test('writeWhere replaces the section and keeps what follows', () => {
  const text = 'Body\n\n### Where to look first\n\n- `old.rs:1` gone\n\n### Tests\n\nran';
  expect(writeWhere(text, heading, [{path: 'new.rs', line: 9, note: ''}])).toBe(
    'Body\n\n### Where to look first\n\n- `new.rs:9`\n\n### Tests\n\nran',
  );
});

test('writeWhere removes the section when no pointer is left', () => {
  expect(writeWhere('Body\n\n### Where to look first\n\n- `a.rs:1`\n', heading, [])).toBe('Body\n');
  expect(writeWhere('Body\n\n### Where to look first\n\n- `a.rs:1`\n\n### Tests\n\nran', heading, [])).toBe(
    'Body\n\n### Tests\n\nran',
  );
  expect(writeWhere('Body', heading, [])).toBe('Body');
});

test('writeWhere round-trips through parseWhere', () => {
  const pointers = [{path: 'a.rs', line: 1, note: 'one'}, {path: 'dir/b.go', line: 0, note: ''}];
  expect(parseWhere(writeWhere('x', heading, pointers), heading).pointers).toEqual(pointers);
});

test('readPointer checks the path, line and note', () => {
  expect(readPointer(' src/a.rs ', ' 42 ', '  the   loop ')).toEqual({path: 'src/a.rs', line: 42, note: 'the loop'});
  expect(readPointer('a.rs', '', '')).toEqual({path: 'a.rs', line: 0, note: ''});
  expect(readPointer('', '1', '')).toBeNull();
  expect(readPointer('a b.rs', '1', '')).toBeNull();
  expect(readPointer('a`.rs', '1', '')).toBeNull();
  expect(readPointer('a.rs', '1x', '')).toBeNull();
  expect(readPointer('a.rs', '1234567890', '')).toBeNull();
  expect(readPointer('x'.repeat(1025), '', '')).toBeNull();
  expect(readPointer('a.rs', '', 'n'.repeat(400)).note).toHaveLength(300);
});

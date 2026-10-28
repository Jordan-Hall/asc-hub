/* eslint-disable no-template-curly-in-string, @eslint-community/eslint-comments/disable-enable-pair -- the sources are TSRX text with ${...} */
import {StringStream} from '@codemirror/language';
import {tsrxParser} from './codemirror-tsrx.ts';

// Runs the tokenizer over source text line by line, as the editor does, and returns [style, text] pairs without the
// blank ones. A null style is written as "-".
function tokenize(src) {
  const state = tsrxParser.startState(2);
  const out = [];
  for (const line of src.split('\n')) {
    const stream = new StringStream(line, 2, 2);
    while (!stream.eol()) {
      const style = tsrxParser.token(stream, state);
      const text = stream.current();
      stream.start = stream.pos;
      if (text.trim() !== '') out.push([style ?? '-', text]);
    }
  }
  return out;
}

const styleOf = (tokens, text) => tokens.find(([, t]) => t === text)?.[0];

test('a component with the statement container, a fragment and a scoped style', () => {
  const t = tokenize([
    'export function Greeting({ name }: { name?: string }) @{',
    '  const message = name ? `Hello, ${name}` : \'Hello, stranger\';',
    '  <>',
    '    <div class="card"><p>{message}</p></div>',
    '    <style>',
    '      .card { padding: 1rem; }',
    '    </style>',
    '  </>',
    '}',
    'const after = 1;',
  ].join('\n'));
  expect(styleOf(t, '@{')).toBe('meta');
  expect(styleOf(t, 'function')).toBe('definitionKeyword');
  expect(styleOf(t, 'Greeting')).toBe('variableName.definition');
  expect(styleOf(t, 'string')).toBe('typeName');
  expect(styleOf(t, 'div')).toBe('tagName');
  expect(styleOf(t, 'class')).toBe('attributeName');
  expect(styleOf(t, '"card"')).toBe('string');
  expect(styleOf(t, 'message')).toBe('variableName');
  expect(styleOf(t, '.card')).toBe('className');
  expect(styleOf(t, 'padding')).toBe('propertyName');
  expect(styleOf(t, '1rem')).toBe('number');
  // the lexer is back at top level after the component: `const after = 1` is TypeScript
  expect(t.slice(-5)).toEqual([['definitionKeyword', 'const'], ['variableName', 'after'], ['operator', '='], ['number', '1'], ['punctuation', ';']]);
});

test('@for and @if are control flow with their conditions and blocks', () => {
  const t = tokenize([
    'function L({users}: P) @{',
    '  @for (const u of users) {',
    '    @if (u.bio && ok({a: 1})) {',
    '      <b onClick={go}>{u.name}</b>',
    '    }',
    '  }',
    '}',
    'let done = true;',
  ].join('\n'));
  expect(t.filter(([s]) => s === 'controlKeyword').map(([, x]) => x)).toEqual(['@for', '@if']);
  expect(styleOf(t, 'of')).toBe('keyword');
  expect(styleOf(t, 'b')).toBe('tagName');
  expect(styleOf(t, 'onClick')).toBe('attributeName');
  expect(styleOf(t, 'true')).toBe('atom');
  expect(styleOf(t, 'let')).toBe('definitionKeyword');
});

test('a generic and a comparison are TypeScript, an element after = is markup', () => {
  let t = tokenize('const a = useState<string>(0); const b = n < 3 && m > 1;');
  expect(t.some(([s]) => s === 'tagName')).toBe(false);
  expect(styleOf(t, 'string')).toBe('typeName');
  t = tokenize('const el = (<Foo.Bar a="1" b={2} />);');
  expect(styleOf(t, 'Foo.Bar')).toBe('tagName');
  expect(styleOf(t, 'a')).toBe('attributeName');
});

test('a dollar sign in markup text is text, a template literal interpolates', () => {
  let t = tokenize('function C() @{\n  <p>Subtotal: ${subtotal}</p>\n}');
  expect(t.find(([, x]) => x.startsWith('Subtotal'))?.[0]).toBe('-');
  expect(styleOf(t, 'subtotal')).toBe('variableName');
  t = tokenize('const m = `Hi ${a ? b : \'c\'}!`;');
  expect(t.filter(([s]) => s === 'string').length).toBeGreaterThanOrEqual(3);
  expect(styleOf(t, 'a')).toBe('variableName');
});

test('comments, also across lines', () => {
  const t = tokenize('// one\n/* two\n three */ const x = 1;');
  expect(t[0]).toEqual(['lineComment', '// one']);
  expect(t.filter(([s]) => s === 'blockComment').length).toBe(3);
  expect(styleOf(t, 'const')).toBe('definitionKeyword');
});

test('copyState keeps the stacks apart and indent follows the nesting', () => {
  const s = tsrxParser.startState(2);
  s.stack.push('braces', 'children');
  const c = tsrxParser.copyState(s);
  c.stack.pop();
  expect(s.stack).toEqual(['braces', 'children']);
  const cx = {unit: 2};
  expect(tsrxParser.indent(s, '<p/>', cx)).toBe(4);
  expect(tsrxParser.indent(s, '</div>', cx)).toBe(2);
  expect(tsrxParser.indent(c, '}', cx)).toBe(0);
});

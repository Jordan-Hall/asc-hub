// Every stylesheet of the hub must parse: an unclosed block or a stray brace breaks the frontend build (webpack) and
// the whole image with it, so CI fails here first.
import {readdirSync, readFileSync, statSync} from 'node:fs';
import {join} from 'node:path';
import postcss from 'postcss';

function cssFiles(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return cssFiles(path);
    return name.endsWith('.css') ? [path] : [];
  });
}

const root = join(process.cwd(), 'web_src', 'css');

for (const file of cssFiles(root)) {
  test(`${file.slice(root.length)} parses and its braces balance`, () => {
    const src = readFileSync(file, 'utf8');
    expect(() => postcss.parse(src, {from: file})).not.toThrow();
    const code = src.replace(/\/\*[\s\S]*?\*\//g, '');
    expect(code.split('{').length).toBe(code.split('}').length);
  });
}

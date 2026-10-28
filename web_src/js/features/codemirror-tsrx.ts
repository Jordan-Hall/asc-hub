/* eslint-disable unicorn/prefer-regexp-test, @eslint-community/eslint-comments/disable-enable-pair -- stream.match consumes input, it is not String#match */
// TSRX (https://tsrx.dev/) for the web editor: a CodeMirror 6 stream language with the same states as the
// server-side highlighter (modules/highlight/tsrx). It knows the statement container `@{ ... }`, the `@if` and `@for`
// control flow, JSX-like elements and fragments with `{expression}` slots, template literals, a scoped `<style>`
// whose body is CSS, and TypeScript everywhere else. It also gives the editor line and block comments, bracket
// closing and the indent of the nesting.

import {StreamLanguage, LanguageSupport, type StreamParser, type StringStream, type IndentContext} from '@codemirror/language';

type Mode = 'code' | 'braces' | 'parens' | 'control' | 'template' | 'tag' | 'attrs' | 'closetag' | 'children' | 'style' | 'comment';

export type TsrxState = {
  stack: Mode[];
  // the word after `function`, `class`, ... is a definition
  def: '' | 'function' | 'class';
  // depth of `{ }` inside a `<style>` body: 0 is the selector, above 0 the declarations
  css: number;
};

const keywords = new Set([
  'abstract', 'as', 'async', 'await', 'break', 'case', 'catch', 'continue', 'debugger', 'declare', 'default', 'delete', 'do',
  'else', 'export', 'extends', 'finally', 'for', 'from', 'get', 'if', 'implements', 'import', 'in', 'instanceof', 'is', 'keyof',
  'namespace', 'new', 'of', 'private', 'protected', 'public', 'readonly', 'return', 'satisfies', 'set', 'static', 'super', 'switch',
  'throw', 'try', 'typeof', 'void', 'while', 'with', 'yield', 'infer', 'override', 'accessor', 'using',
]);
const declarations = new Set(['const', 'let', 'var', 'function', 'class', 'interface', 'type', 'enum']);
const constants = new Set(['true', 'false', 'null', 'undefined', 'this', 'NaN', 'Infinity']);
const builtinTypes = new Set(['string', 'number', 'boolean', 'any', 'unknown', 'never', 'object', 'symbol', 'bigint']);

const top = (s: TsrxState): Mode => s.stack[s.stack.length - 1] ?? 'code';
const replaceTop = (s: TsrxState, m: Mode) => {
  if (s.stack.length) s.stack[s.stack.length - 1] = m;
  else s.stack.push(m);
};

// "<" starts markup only where an expression can start: not after a word, ")" or "]" (a generic or a comparison).
function markupAllowed(stream: StringStream): boolean {
  const before = stream.pos > 0 ? stream.string[stream.pos - 1] : ' ';
  return !/[\w$)\]]/.test(before);
}

function codeToken(stream: StringStream, state: TsrxState): string | null {
  const mode = top(state);
  if (stream.match('//')) {
    stream.skipToEnd();
    return 'lineComment';
  }
  if (stream.match('/*')) {
    state.stack.push('comment');
    return 'blockComment';
  }
  if (stream.match('@{')) {
    state.stack.push('braces');
    return 'meta';
  }
  if (stream.match(/^@(?:if|for)\b/)) {
    state.stack.push('control');
    return 'controlKeyword';
  }
  if (stream.peek() === '<' && markupAllowed(stream)) {
    if (stream.match(/^<style(?:\s[^>]*)?>/)) {
      state.stack.push('style');
      state.css = 0;
      return 'tagName';
    }
    if (stream.match('<>')) {
      state.stack.push('children');
      return 'angleBracket';
    }
    if (stream.match(/^<(?=[A-Za-z_])/)) {
      state.stack.push('tag');
      return 'angleBracket';
    }
  }
  if (stream.match('`')) {
    state.stack.push('template');
    return 'string';
  }
  if (stream.match(/^'(?:[^'\\\n]|\\.)*'/) || stream.match(/^"(?:[^"\\\n]|\\.)*"/)) return 'string';
  if (stream.match(/^0[xX][\da-fA-F_]+n?/) || stream.match(/^0[bB][01_]+n?/) || stream.match(/^\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d+)?n?/)) return 'number';
  if (stream.match(/^[A-Za-z_$][\w$]*/)) {
    const word = stream.current();
    if (state.def) {
      const kind = state.def;
      state.def = '';
      return kind === 'function' ? 'variableName.definition' : 'className';
    }
    if (word === 'function') state.def = 'function';
    else if (word === 'class' || word === 'interface' || word === 'enum' || word === 'type' || word === 'namespace') state.def = 'class';
    if (declarations.has(word)) return 'definitionKeyword';
    if (constants.has(word)) return 'atom';
    if (builtinTypes.has(word)) return 'typeName';
    if (keywords.has(word)) return 'keyword';
    return 'variableName';
  }
  if (stream.match('}')) {
    if (mode === 'braces') state.stack.pop();
    return 'brace';
  }
  if (stream.match('{')) {
    state.stack.push('braces');
    return 'brace';
  }
  if (mode === 'parens' && stream.match(')')) {
    state.stack.pop();
    return 'paren';
  }
  if (mode === 'parens' && stream.match('(')) {
    state.stack.push('parens');
    return 'paren';
  }
  if (stream.match(/^(?:=>|\.\.\.|[-+*/%&|^!~=<>?:]+)/)) return 'operator';
  if (stream.match(/^[()[\];,.]/)) return 'punctuation';
  stream.next();
  return null;
}

function cssToken(stream: StringStream, state: TsrxState): string | null {
  if (stream.match('</')) {
    replaceTop(state, 'closetag');
    return 'angleBracket';
  }
  if (stream.match('/*')) {
    state.stack.push('comment');
    return 'blockComment';
  }
  if (stream.match('{')) {
    state.css++;
    return 'brace';
  }
  if (stream.match('}')) {
    state.css = Math.max(0, state.css - 1);
    return 'brace';
  }
  if (stream.match(/^'(?:[^'\\\n]|\\.)*'/) || stream.match(/^"(?:[^"\\\n]|\\.)*"/)) return 'string';
  if (stream.match(/^@[\w-]+/)) return 'keyword';
  if (stream.match(/^#[\da-fA-F]{3,8}\b/)) return 'number';
  if (stream.match(/^-?(?:\d+(?:\.\d*)?|\.\d+)[a-z%]*/)) return 'number';
  if (state.css > 0) {
    if (stream.match(/^[\w-]+(?=\s*:)/)) return 'propertyName';
    if (stream.match(/^[\w-]+/)) return 'atom';
  } else {
    if (stream.match(/^\.[\w-]+/)) return 'className';
    if (stream.match(/^#[\w-]+/)) return 'labelName';
    if (stream.match(/^::?[\w-]+/)) return 'typeName';
    if (stream.match(/^[\w-]+/)) return 'tagName';
  }
  if (stream.match(/^[;:,>+~()[\]=*]/)) return 'punctuation';
  stream.next();
  return null;
}

function token(stream: StringStream, state: TsrxState): string | null {
  const mode = top(state);
  if (mode === 'comment') {
    if (stream.match(/^[\s\S]*?\*\//)) state.stack.pop();
    else stream.skipToEnd();
    return 'blockComment';
  }
  if (mode === 'children') {
    if (stream.match('</')) {
      replaceTop(state, 'closetag');
      return 'angleBracket';
    }
    if (stream.match(/^<style(?:\s[^>]*)?>/)) {
      state.stack.push('style');
      state.css = 0;
      return 'tagName';
    }
    if (stream.match('<>')) {
      state.stack.push('children');
      return 'angleBracket';
    }
    if (stream.match(/^<(?=[A-Za-z_])/)) {
      state.stack.push('tag');
      return 'angleBracket';
    }
    if (stream.match('{')) {
      state.stack.push('braces');
      return 'brace';
    }
    if (stream.match(/^@(?:if|for)\b/)) {
      state.stack.push('control');
      return 'controlKeyword';
    }
    if (!stream.match(/^[^<{@}]+/)) stream.next();
    return null;
  }
  if (stream.eatSpace()) return null;
  switch (mode) {
    case 'style':
      return cssToken(stream, state);
    case 'template':
      if (stream.match('`')) {
        state.stack.pop();
        return 'string';
      }
      if (stream.match('${')) {
        state.stack.push('braces');
        return 'punctuation';
      }
      if (stream.match(/^\\[\s\S]?/)) return 'string';
      if (!stream.match(/^[^`\\$]+/)) stream.next();
      return 'string';
    case 'tag':
      if (stream.match(/^[A-Za-z_][\w.:$-]*/)) {
        replaceTop(state, 'attrs');
        return 'tagName';
      }
      state.stack.pop();
      return token(stream, state);
    case 'closetag':
      if (stream.match(/^[A-Za-z_][\w.:$-]*/)) return 'tagName';
      if (stream.match('>')) {
        state.stack.pop();
        return 'angleBracket';
      }
      stream.next();
      return null;
    case 'attrs':
      if (stream.match('/>')) {
        state.stack.pop();
        return 'angleBracket';
      }
      if (stream.match('>')) {
        replaceTop(state, 'children');
        return 'angleBracket';
      }
      if (stream.match('=')) return 'operator';
      if (stream.match(/^'[^']*'/) || stream.match(/^"[^"]*"/)) return 'string';
      if (stream.match('{')) {
        state.stack.push('braces');
        return 'brace';
      }
      if (stream.match(/^[A-Za-z_][\w:.$-]*/)) return 'attributeName';
      stream.next();
      return null;
    case 'control':
      if (stream.match('(')) {
        state.stack.push('parens');
        return 'paren';
      }
      if (stream.match('{')) {
        replaceTop(state, 'braces');
        return 'brace';
      }
      state.stack.pop();
      return token(stream, state);
    default:
      return codeToken(stream, state);
  }
}

// Nesting that indents the lines inside it.
const indentingModes = new Set<Mode>(['braces', 'parens', 'children', 'attrs']);

export const tsrxParser: StreamParser<TsrxState> = {
  name: 'tsrx',
  startState: () => ({stack: [], def: '', css: 0}),
  copyState: (s) => ({stack: Array.from(s.stack), def: s.def, css: s.css}),
  token,
  indent(state: TsrxState, textAfter: string, cx: IndentContext): number {
    let depth = state.stack.filter((m) => indentingModes.has(m)).length;
    if (/^\s*(?:\}|\)|\]|<\/|\/>|>)/.test(textAfter) && depth > 0) depth--;
    return depth * cx.unit;
  },
  languageData: {
    commentTokens: {line: '//', block: {open: '/*', close: '*/'}},
    closeBrackets: {brackets: ['(', '[', '{', "'", '"', '`']},
    indentOnInput: /^\s*(?:\}|\)|\]|<\/)$/,
  },
};

export function tsrx(): LanguageSupport {
  return new LanguageSupport(StreamLanguage.define(tsrxParser));
}

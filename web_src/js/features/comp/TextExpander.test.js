import {initTextExpander} from './TextExpander.js';

// expanderIn is a text expander in a comment form of new lines, with value typed in its textarea.
function expanderIn(suggest, value) {
  document.body.innerHTML = `<form data-asc-suggest-label="Suggest a change"><text-expander><textarea></textarea></text-expander></form>`;
  const form = document.querySelector('form');
  if (suggest !== null) form.setAttribute('data-asc-suggest', suggest);
  const expander = document.querySelector('text-expander');
  const textarea = expander.querySelector('textarea');
  textarea.value = value;
  textarea.selectionStart = textarea.selectionEnd = value.length;
  initTextExpander(expander);
  return expander;
}

// offered is what the expander offers for "/" and text: the value of the item, or null.
function offered(expander, text) {
  let got = null;
  expander.dispatchEvent(new CustomEvent('text-expander-change', {detail: {key: '/', text, provide: (r) => {
    got = r.matched ? r.fragment.querySelector('li').getAttribute('data-value') : null;
  }}}));
  return got;
}

test('"/" puts the lines in a suggestion block', () => {
  expect(offered(expanderIn('  let a = 1;\n  let b = 2;', '/'), '')).toEqual('```suggestion\n  let a = 1;\n  let b = 2;\n```\n');
  expect(offered(expanderIn('x', 'Use this:\n/sug'), 'sug')).toEqual('```suggestion\nx\n```\n');
});

test('a block that does not start a line starts one', () => {
  expect(offered(expanderIn('x', 'Like this /'), '')).toEqual('\n```suggestion\nx\n```\n');
});

test('"/" offers nothing for other text or without the lines', () => {
  expect(offered(expanderIn('x', '/foo'), 'foo')).toBeNull();
  expect(offered(expanderIn(null, '/'), '')).toBeNull();
});

test('the item keeps its value when chosen', () => {
  const expander = expanderIn('x', '/');
  const item = document.createElement('li');
  item.setAttribute('data-value', '```suggestion\nx\n```\n');
  const detail = {key: '/', item};
  expander.dispatchEvent(new CustomEvent('text-expander-value', {detail}));
  expect(detail.value).toEqual('```suggestion\nx\n```\n');
});

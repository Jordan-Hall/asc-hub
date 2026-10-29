import {svg} from '../svg.js';

export function makeCodeCopyButton() {
  const button = document.createElement('button');
  button.classList.add('code-copy', 'ui', 'button');
  button.innerHTML = svg('octicon-copy');
  return button;
}

// A block of commands written with the shell prompt ("$ asc sync") is copied without it, so the lines run when pasted.
// Only a block in which every line starts with the prompt is changed: a block that mixes commands and their output
// is copied as it is.
export function stripShellPrompt(text) {
  const lines = text.split('\n');
  const filled = lines.filter((l) => l.trim() !== '');
  if (!filled.length || !filled.every((l) => /^\$ /.test(l))) return text;
  return lines.map((l) => l.replace(/^\$ /, '')).join('\n');
}

export function renderCodeCopy() {
  const els = document.querySelectorAll('.markup .code-block code');
  if (!els.length) return;

  for (const el of els) {
    if (!el.textContent) continue;
    const btn = makeCodeCopyButton();
    // remove final trailing newline introduced during HTML rendering
    btn.setAttribute('data-clipboard-text', stripShellPrompt(el.textContent.replace(/\r?\n$/, '')));
    el.after(btn);
  }
}

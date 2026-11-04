// "Where to look" of a new change (board NewChange): the lines that deserve the reviewer's attention, shown as pills under
// the description. The pointers live in the description itself, in the section headed data-heading ("Where to look first")
// with one "- `path:line` note" line each, so the draft autosave, the summary and the review page all read the same text.
// The editor rewrites that section; a person who edits the section by hand sees the pills follow.

const maxNote = 300;

// parseWhere finds the section headed `### heading` in text and reads its pointers. start and end are the offsets of the
// section (the heading line up to the next heading or the end), -1 when there is none.
export function parseWhere(text, heading) {
  const lines = text.split('\n');
  let offset = 0;
  let start = -1;
  let end = -1;
  const pointers = [];
  for (const line of lines) {
    const next = offset + line.length + 1;
    const trimmed = line.trim();
    if (start === -1) {
      if (/^#{1,6}\s/.test(trimmed) && trimmed.replace(/^#{1,6}\s+/, '') === heading) start = offset;
    } else if (/^#{1,6}\s/.test(trimmed)) {
      end = offset;
      break;
    } else {
      const p = parsePointerLine(trimmed);
      if (p) pointers.push(p);
    }
    offset = next;
  }
  if (start !== -1 && end === -1) end = text.length;
  return {pointers, start, end};
}

function parsePointerLine(line) {
  const m = /^[-*]\s+`([^`]+)`\s*(.*)$/.exec(line);
  if (!m) return null;
  return {...splitTarget(m[1].trim()), note: m[2].trim()};
}

function splitTarget(target) {
  const m = /^(.+):(\d{1,9})$/.exec(target);
  if (m) return {path: m[1], line: Number(m[2])};
  return {path: target, line: 0};
}

export function pointerText(p) {
  return `${p.path}${p.line ? `:${p.line}` : ''}`;
}

function pointerLine(p) {
  return `- \`${pointerText(p)}\`${p.note ? ` ${p.note}` : ''}`;
}

// writeWhere puts pointers into the section of text: it replaces the section, adds it at the end, or removes it when
// there are no pointers.
export function writeWhere(text, heading, pointers) {
  const {start, end} = parseWhere(text, heading);
  const section = pointers.length ? `### ${heading}\n\n${pointers.map(pointerLine).join('\n')}\n` : '';
  if (start === -1) {
    if (!section) return text;
    const body = text.replace(/\s+$/, '');
    return body ? `${body}\n\n${section}` : section;
  }
  const before = text.slice(0, start).replace(/\s+$/, '');
  const after = text.slice(end).replace(/^\s+/, '');
  if (!section) return after ? [before, after].filter(Boolean).join('\n\n') : before && `${before}\n`;
  return `${before ? `${before}\n\n` : ''}${section}${after ? `\n${after}` : ''}`;
}

// readPointer checks what the person typed: a path without spaces or backticks, a line number or none, a short note.
export function readPointer(path, line, note) {
  const p = (path || '').trim();
  if (!p || p.length > 1024 || /[\s`]/.test(p)) return null;
  const l = (line || '').trim();
  if (l && !/^\d{1,9}$/.test(l)) return null;
  const n = (note || '').replace(/\s+/g, ' ').trim().slice(0, maxNote);
  return {path: p, line: l ? Number(l) : 0, note: n};
}

function fileBoxOf(path) {
  for (const box of document.querySelectorAll('.diff-file-box[data-new-filename]')) {
    if (box.getAttribute('data-new-filename') === path) return box;
  }
  return null;
}

function initWhere(el) {
  const form = el.closest('form');
  const body = form?.querySelector('textarea[name=content]');
  if (!body) return;
  const heading = el.getAttribute('data-heading');
  const max = Number(el.getAttribute('data-max')) || 20;
  const list = el.querySelector('[data-asc-where-list]');
  const addBtn = el.querySelector('[data-asc-where-add]');
  const row = el.querySelector('[data-asc-where-row]');
  const [pathIn, lineIn, noteIn] = ['path', 'line', 'note'].map((n) => row.querySelector(`[data-asc-where-${n}]`));
  const error = row.querySelector('[data-asc-where-error]');
  const files = el.querySelector('datalist');

  for (const box of document.querySelectorAll('.diff-file-box[data-new-filename]')) {
    const opt = document.createElement('option');
    opt.value = box.getAttribute('data-new-filename');
    files?.append(opt);
  }

  const save = (pointers) => {
    body.value = writeWhere(body.value, heading, pointers);
    body.dispatchEvent(new Event('input', {bubbles: true}));
    render();
  };

  function render() {
    const {pointers} = parseWhere(body.value, heading);
    list.replaceChildren();
    for (const [i, p] of pointers.entries()) {
      const pill = document.createElement('span');
      pill.className = 'rp-where-pill';
      const box = fileBoxOf(p.path);
      const link = document.createElement(box ? 'a' : 'span');
      link.className = 'rp-where-link';
      if (box) link.href = `#${box.id}`;
      if (p.note) link.title = p.note;
      link.append(el.querySelector('[data-asc-where-icon]').content.cloneNode(true));
      link.append(pointerText(p));
      const rm = document.createElement('button');
      rm.type = 'button';
      rm.className = 'rp-where-rm';
      rm.setAttribute('aria-label', el.getAttribute('data-remove').replace('%s', pointerText(p)));
      rm.append(el.querySelector('[data-asc-where-x]').content.cloneNode(true));
      rm.addEventListener('click', () => save(pointers.filter((_, j) => j !== i)));
      pill.append(link, rm);
      list.append(pill);
    }
    addBtn.disabled = pointers.length >= max;
  }

  const close = () => {
    row.hidden = true;
    error.textContent = '';
    addBtn.focus();
  };
  const add = () => {
    const p = readPointer(pathIn.value, lineIn.value, noteIn.value);
    if (!p) {
      error.textContent = el.getAttribute('data-invalid');
      pathIn.focus();
      return;
    }
    const {pointers} = parseWhere(body.value, heading);
    save([...pointers, p].slice(0, max));
    pathIn.value = '';
    lineIn.value = '';
    noteIn.value = '';
    close();
  };

  addBtn.addEventListener('click', () => {
    row.hidden = false;
    pathIn.focus();
  });
  row.querySelector('[data-asc-where-ok]').addEventListener('click', add);
  row.querySelector('[data-asc-where-cancel]').addEventListener('click', close);
  row.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      add();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      close();
    }
  });
  let timer;
  body.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(render, 150);
  });
  render();
}

export function initAscWhere() {
  for (const el of document.querySelectorAll('[data-asc-where]')) initWhere(el);
}

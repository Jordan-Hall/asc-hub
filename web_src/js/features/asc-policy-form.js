// The Form view of the repository policy (board RepoSettingsPolicy): the approvals stepper and the list fields as chips.
// Both work without this script: the number field takes the approvals, and the Add field of a list is sent with the form.
// The script shows the stepper buttons and the Add buttons, and Add (or Enter in its field) makes ticked chips at once.

// entriesOf splits the text of an Add field into entries: one a line, commas also split, blanks left out.
export function entriesOf(text) {
  return text.split(/[\n\r,]/).map((s) => s.trim()).filter(Boolean);
}

// addChips adds a ticked chip to the chips field for each new entry of its Add field and empties that field.
// An entry that is already a chip is ticked again rather than repeated.
export function addChips(field) {
  const input = field.querySelector('[data-chips-input]');
  const list = field.querySelector('[data-chips-list]');
  const name = field.getAttribute('data-policy-chips');
  if (!input || !list || !name) return;
  const have = new Map();
  for (const box of list.querySelectorAll('input[type=checkbox]')) have.set(box.value, box);
  for (const entry of entriesOf(input.value)) {
    const old = have.get(entry);
    if (old) {
      old.checked = true;
      continue;
    }
    const li = document.createElement('li');
    const label = document.createElement('label');
    label.className = 'ast-chip';
    const box = document.createElement('input');
    box.type = 'checkbox';
    box.name = name;
    box.value = entry;
    box.checked = true;
    const text = document.createElement('span');
    text.className = 'ds-mono';
    text.textContent = entry;
    label.append(box, text);
    li.append(label);
    list.append(li);
    have.set(entry, box);
  }
  input.value = '';
}

// step moves the number field of a stepper by delta within its min and max.
export function step(input, delta) {
  const min = input.min === '' ? -Infinity : Number(input.min);
  const max = input.max === '' ? Infinity : Number(input.max);
  const now = Number.parseInt(input.value);
  const next = Math.min(max, Math.max(min, (Number.isNaN(now) ? 0 : now) + delta));
  input.value = String(next);
  input.dispatchEvent(new Event('input', {bubbles: true}));
}

export function initAscPolicyForm(root = document) {
  for (const stepper of root.querySelectorAll('[data-policy-stepper]')) {
    const input = stepper.querySelector('input[type=number]');
    if (!input || stepper.dataset.ready) continue;
    stepper.dataset.ready = '1';
    for (const btn of stepper.querySelectorAll('[data-step]')) {
      btn.hidden = false;
      btn.addEventListener('click', () => step(input, Number(btn.getAttribute('data-step'))));
    }
  }
  for (const field of root.querySelectorAll('[data-policy-chips]')) {
    const input = field.querySelector('[data-chips-input]');
    const add = field.querySelector('[data-chips-add]');
    if (!input || !add || field.dataset.ready) continue;
    field.dataset.ready = '1';
    add.hidden = false;
    add.addEventListener('click', () => {
      addChips(field);
      input.focus();
    });
    input.addEventListener('keydown', (e) => {
      if (e.key !== 'Enter') return;
      e.preventDefault();
      addChips(field);
    });
  }
}

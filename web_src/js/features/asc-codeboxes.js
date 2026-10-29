// Six boxes that act as one code input. The page keeps its normal field (name, autocomplete one-time-code, required);
// this replaces the way it looks and is typed: the boxes show the digits, the field holds them, a digit moves to the next
// box, backspace to the previous, a paste fills them all, and the form is sent when the sixth digit is in.
// The field is [data-code-field]; without script it stays as it is.

const LENGTH = 6;

export function digitsOf(text) {
  return String(text ?? '').replace(/\D/g, '').slice(0, LENGTH);
}

export function initAscCodeBoxes() {
  const field = document.querySelector('input[data-code-field]');
  if (!field) return;
  const group = document.createElement('div');
  group.className = 'ds-codeboxes';
  group.setAttribute('role', 'group');
  group.setAttribute('aria-label', field.closest('.ds-field')?.querySelector('label')?.textContent?.trim() ?? '');
  const boxes = [];
  for (let i = 0; i < LENGTH; i++) {
    const box = document.createElement('input');
    box.type = 'text';
    box.inputMode = 'numeric';
    box.autocomplete = i === 0 ? 'one-time-code' : 'off';
    box.maxLength = 1;
    box.className = 'ds-input ds-codebox';
    box.setAttribute('aria-label', `${i + 1} / ${LENGTH}`);
    boxes.push(box);
    group.append(box);
  }
  const sync = () => {
    field.value = boxes.map((b) => b.value).join('');
    if (field.value.length === LENGTH) field.form?.requestSubmit();
  };
  const fill = (text, from) => {
    const digits = digitsOf(text);
    for (let k = 0; k < digits.length && from + k < LENGTH; k++) boxes[from + k].value = digits[k];
    boxes[Math.min(from + digits.length, LENGTH - 1)].focus();
    sync();
  };
  for (const [i, box] of boxes.entries()) {
    box.addEventListener('input', () => {
      const digits = digitsOf(box.value);
      box.value = '';
      if (digits) fill(digits, i);
      else sync();
    });
    box.addEventListener('keydown', (e) => {
      if (e.key === 'Backspace' && box.value === '' && i > 0) {
        boxes[i - 1].value = '';
        boxes[i - 1].focus();
        sync();
        e.preventDefault();
      } else if (e.key === 'ArrowLeft' && i > 0) {
        boxes[i - 1].focus();
      } else if (e.key === 'ArrowRight' && i < LENGTH - 1) {
        boxes[i + 1].focus();
      }
    });
    box.addEventListener('paste', (e) => {
      e.preventDefault();
      fill(e.clipboardData?.getData('text'), i);
    });
    box.addEventListener('focus', () => box.select());
  }
  field.hidden = true;
  field.removeAttribute('autofocus');
  field.before(group);
  field.closest('.ds-field')?.querySelector('label')?.removeAttribute('for');
  boxes[0].focus();
}

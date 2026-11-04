import {matchEmoji, matchMention} from '../../utils/match.js';
import {emojiHTML, emojiString} from '../emoji.js';
const {customEmojis} = window.config;

export function initTextExpander(expander) {
  expander?.addEventListener('text-expander-change', ({detail: {key, provide, text}}) => {
    if (key === ':') {
      const matches = matchEmoji(text);
      if (!matches.length) return provide({matched: false});

      const ul = document.createElement('ul');
      ul.classList.add('suggestions');
      for (const name of matches) {
        const li = document.createElement('li');
        li.setAttribute('id', `combobox-emoji-${name}`);
        li.setAttribute('role', 'option');
        li.setAttribute('data-value', emojiString(name));
        if (customEmojis.has(name)) {
          li.style.gap = '0.25rem';
          li.innerHTML = emojiHTML(name);
          li.append(name);
        } else {
          li.textContent = `${emojiString(name)} ${name}`;
        }
        ul.append(li);
      }

      provide({matched: true, fragment: ul});
    } else if (key === '@') {
      const matches = matchMention(text);
      if (!matches.length) return provide({matched: false});

      const ul = document.createElement('ul');
      ul.classList.add('suggestions');
      for (const {value, name, fullname, avatar} of matches) {
        const li = document.createElement('li');
        li.setAttribute('id', `combobox-user-${name}`);
        li.setAttribute('role', 'option');
        li.setAttribute('data-value', `${key}${value}`);

        const img = document.createElement('img');
        img.setAttribute('aria-hidden', 'true');
        img.src = avatar;
        li.append(img);

        const nameSpan = document.createElement('span');
        nameSpan.textContent = name;
        li.append(nameSpan);

        if (fullname && fullname.toLowerCase() !== name) {
          const fullnameSpan = document.createElement('span');
          fullnameSpan.classList.add('fullname');
          fullnameSpan.textContent = fullname;
          li.append(fullnameSpan);
        }

        ul.append(li);
      }

      provide({matched: true, fragment: ul});
    } else if (key === '/') {
      const ul = ascSuggestions(expander, text);
      if (!ul) return provide({matched: false});
      provide({matched: true, fragment: ul});
    }
  });
  expander?.addEventListener('text-expander-value', ({detail}) => {
    if (detail?.item) {
      // add a space after @mentions as it's likely the user wants one
      const suffix = detail.key === '@' ? ' ' : '';
      detail.value = `${detail.item.getAttribute('data-value')}${suffix}`;
    }
  });
}

// ascSuggestions offers "Suggest a change" for "/" in the comment form of new lines of a diff (board Review): it puts
// the lines in a suggestion block, to change them there. The form holds the lines in data-asc-suggest.
function ascSuggestions(expander, text) {
  const form = expander.closest('form[data-asc-suggest]');
  const label = form?.getAttribute('data-asc-suggest-label') ?? '';
  if (!form || !label.toLowerCase().startsWith(text.toLowerCase())) return null;
  // The block must start a line: the expander replaces "/" and the text typed after it.
  const textarea = expander.querySelector('textarea');
  const at = textarea.selectionStart - text.length - 1;
  const before = at > 0 && textarea.value[at - 1] !== '\n' ? '\n' : '';
  const fence = '```';
  const li = document.createElement('li');
  li.setAttribute('id', 'combobox-asc-suggest');
  li.setAttribute('role', 'option');
  li.setAttribute('data-value', `${before}${fence}suggestion\n${form.getAttribute('data-asc-suggest')}\n${fence}\n`);
  li.textContent = label;
  const ul = document.createElement('ul');
  ul.classList.add('suggestions');
  ul.append(li);
  return ul;
}

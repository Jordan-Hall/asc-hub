import $ from 'jquery';
import {minimatch} from 'minimatch';
import {onInputDebounce, toggleElem} from '../utils/dom.js';
import {POST} from '../modules/fetch.js';
import {createCodemirror} from './codemirror.ts';
import {showDsToast} from './ds-toast.js';

const {appSubUrl} = window.config;

// The settings form that carries a save bar: the bar names how many fields changed, Discard puts them back.
export function initRepoSettingsSaveBar() {
  for (const bar of document.querySelectorAll('[data-savebar]')) {
    const form = bar.closest('form');
    if (!form) continue;
    const note = bar.querySelector('[data-savebar-note]');
    const submit = bar.querySelector('[data-savebar-submit]');
    const fields = [...form.elements].filter((el) => el.name && el.type !== 'hidden' && el.type !== 'submit' && el.type !== 'button');
    const initial = new Map(fields.map((el) => [el, el.type === 'checkbox' || el.type === 'radio' ? el.checked : el.value]));
    const update = () => {
      let changed = 0;
      for (const [el, was] of initial) {
        const now = el.type === 'checkbox' || el.type === 'radio' ? el.checked : el.value;
        if (now !== was) changed++;
      }
      bar.classList.toggle('is-dirty', changed > 0);
      // the save button is active only once a field differs from the saved value
      if (submit) submit.disabled = changed === 0;
      if (note) note.textContent = changed === 0 ? '' : (changed === 1 ? bar.getAttribute('data-savebar-one') : bar.getAttribute('data-savebar-n').replace('%d', String(changed)));
    };
    form.addEventListener('input', update);
    form.addEventListener('change', update);
    bar.querySelector('[data-savebar-discard]')?.addEventListener('click', () => {
      for (const [el, was] of initial) {
        if (el.type === 'checkbox' || el.type === 'radio') el.checked = was;
        else el.value = was;
      }
      update();
    });
    update();
  }
}

// Changing the visibility of an existing repository asks first: the dialog names the new level and what it means. It is
// on the settings form only (data-visibility-form); the create form has nothing to change from. Without
// <dialog>.showModal (an old browser) the browser's own confirm asks the same question.
export function initVisibilityConfirm() {
  for (const form of document.querySelectorAll('[data-visibility-form]')) {
    const dialog = form.querySelector('[data-visibility-dialog]');
    const radios = [...form.querySelectorAll('input[type=radio][name=visibility]')];
    if (!dialog || radios.length === 0) continue;
    const initial = radios.find((r) => r.defaultChecked)?.value;
    let confirmed = false;
    form.addEventListener('submit', (e) => {
      const now = radios.find((r) => r.checked);
      if (confirmed || !now || now.value === initial) return;
      e.preventDefault();
      const title = now.getAttribute('data-confirm-title') || '';
      const body = now.getAttribute('data-confirm-body') || '';
      dialog.querySelector('[data-visibility-title]').textContent = title;
      dialog.querySelector('[data-visibility-body]').textContent = body;
      const go = () => {
        confirmed = true;
        form.requestSubmit();
      };
      if (typeof dialog.showModal !== 'function') {
        if (window.confirm(`${title} ${body}`)) go();
        return;
      }
      dialog.querySelector('[data-visibility-confirm]').onclick = () => {
        dialog.close();
        go();
      };
      dialog.querySelector('[data-visibility-cancel]').onclick = () => dialog.close();
      dialog.showModal();
    });
  }
}

// The Topics field of General: the comma separated list is posted to the topics endpoint and the answer is said in
// words next to the button; the page is not reloaded.
// The topics as chips: the typed text becomes a chip at a comma or Enter, a chip is removed with its button, and the field
// that is posted (name="topics") holds the comma separated list. At most `max` chips.
export function enhanceTopicChips(form, max = 20) {
  const input = form.querySelector('input[name=topics]');
  if (!input || input.dataset.chips) return;
  input.dataset.chips = '1';
  const hidden = document.createElement('input');
  hidden.type = 'hidden';
  hidden.name = 'topics';
  input.removeAttribute('name');
  input.after(hidden);
  const list = document.createElement('ul');
  list.className = 'rp-chips';
  list.setAttribute('aria-label', input.getAttribute('aria-label') || 'Topics');
  input.before(list);
  const topics = [];
  const sync = () => {
    hidden.value = topics.join(',');
    list.textContent = '';
    for (const t of topics) {
      const li = document.createElement('li');
      li.className = 'rp-chip';
      li.append(document.createTextNode(t));
      const rm = document.createElement('button');
      rm.type = 'button';
      rm.className = 'rp-chip-x';
      rm.setAttribute('aria-label', `${form.getAttribute('data-topics-remove') || 'Remove'} ${t}`);
      rm.textContent = '×';
      rm.addEventListener('click', () => {
        topics.splice(topics.indexOf(t), 1);
        sync();
        input.focus();
      });
      li.append(rm);
      list.append(li);
    }
    input.disabled = topics.length >= max;
  };
  const add = (text) => {
    for (const raw of text.split(',')) {
      const t = raw.trim().toLowerCase();
      if (t && !topics.includes(t) && topics.length < max) topics.push(t);
    }
    input.value = '';
    sync();
  };
  add(input.value);
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      add(input.value);
    } else if (e.key === 'Backspace' && input.value === '' && topics.length > 0) {
      topics.pop();
      sync();
    }
  });
  input.addEventListener('blur', () => add(input.value));
  form.addEventListener('submit', () => add(input.value), true);
}

export function initRepoSettingsTopics() {
  for (const form of document.querySelectorAll('[data-topics-form]')) {
    enhanceTopicChips(form);
    const status = form.querySelector('[data-topics-status]');
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (status) status.textContent = '';
      let message = '';
      try {
        const resp = await POST(form.getAttribute('action'), {data: new FormData(form)});
        if (!resp.ok) {
          const body = await resp.json().catch(() => ({}));
          message = body.message || String(resp.status);
        }
      } catch (err) {
        message = String(err?.message ?? err);
      }
      if (status) {
        status.textContent = message ? form.getAttribute('data-topics-failed').replace('%s', message) : form.getAttribute('data-topics-saved');
      }
    });
  }
}

// Says that a person's access changed and offers to put it back: the change was applied when the role was picked, so
// the toast reports it and Undo applies the old role again (board RepoSettingsAccess).
export function announceAccessChange(el, newText, undo) {
  const message = (el.getAttribute('data-changed') || '').replace('%s', newText);
  if (!message) return null;
  return showDsToast(message, {actionLabel: el.getAttribute('data-undo') || '', onAction: undo, closeLabel: el.getAttribute('data-close') || ''});
}

export function initRepoSettingsCollaboration() {
  // Change collaborator access mode
  $('.page-content.repository .ui.dropdown.access-mode').each((_, el) => {
    const $dropdown = $(el);
    const $text = $dropdown.find('> .text');
    $dropdown.dropdown({
      async action(_text, value) {
        const lastValue = el.getAttribute('data-last-value');
        const lastText = $text.text();
        try {
          el.setAttribute('data-last-value', value);
          $dropdown.dropdown('hide');
          const data = new FormData();
          data.append('uid', el.getAttribute('data-uid'));
          data.append('mode', value);
          await POST(el.getAttribute('data-url'), {data});
          if (lastValue && lastValue !== value) {
            announceAccessChange(el, _text, async () => {
              const back = new FormData();
              back.append('uid', el.getAttribute('data-uid'));
              back.append('mode', lastValue);
              await POST(el.getAttribute('data-url'), {data: back});
              el.setAttribute('data-last-value', lastValue);
              $dropdown.dropdown('set selected', lastValue);
              $text.text(lastText);
            });
          }
        } catch {
          $text.text('(error)'); // prevent from misleading users when error occurs
          el.setAttribute('data-last-value', lastValue);
        }
      },
      onChange(_value, text, _$choice) {
        $text.text(text); // update the text when using keyboard navigating
      },
      onHide() {
        // set to the really selected value, defer to next tick to make sure `action` has finished its work because the calling order might be onHide -> action
        setTimeout(() => {
          const $item = $dropdown.dropdown('get item', el.getAttribute('data-last-value'));
          if ($item) {
            $dropdown.dropdown('set selected', el.getAttribute('data-last-value'));
          } else {
            $text.text('(none)'); // prevent from misleading users when the access mode is undefined
          }
        }, 0);
      },
    });
  });
}

export function initRepoSettingSearchTeamBox() {
  const searchTeamBox = document.getElementById('search-team-box');
  if (!searchTeamBox) return;

  $(searchTeamBox).search({
    minCharacters: 2,
    apiSettings: {
      url: `${appSubUrl}/org/${searchTeamBox.getAttribute('data-org-name')}/teams/-/search?q={query}`,
      onResponse(response) {
        const items = [];
        for (const item of response.data) {
          items.push({
            title: item.name,
            description: `${item.permission} access`, // TODO: translate this string
          });
        }
        return {results: items};
      },
    },
    searchFields: ['name', 'description'],
    showNoResults: false,
  });
}

export function initRepoSettingGitHook() {
  if (!$('.edit.githook').length) return;
  const filename = document.querySelector('.hook-filename').textContent;
  const _promise = createCodemirror($('#content')[0], filename, {language: 'shell'});
}

export function initRepoSettingBranches() {
  if (!document.querySelector('.repository.settings.branches')) return;

  for (const el of document.getElementsByClassName('toggle-target-enabled')) {
    el.addEventListener('change', function () {
      const target = document.querySelector(this.getAttribute('data-target'));
      target?.classList.toggle('disabled', !this.checked);
    });
  }

  for (const el of document.getElementsByClassName('toggle-target-disabled')) {
    el.addEventListener('change', function () {
      const target = document.querySelector(this.getAttribute('data-target'));
      if (this.checked) target?.classList.add('disabled'); // only disable, do not auto enable
    });
  }

  document.getElementById('dismiss_stale_approvals')?.addEventListener('change', function () {
    document.getElementById('ignore_stale_approvals_box')?.classList.toggle('disabled', this.checked);
  });

  // show the `Matched` mark for the status checks that match the pattern
  const markMatchedStatusChecks = () => {
    const patterns = (document.getElementById('status_check_contexts').value || '').split(/[\r\n]+/);
    const validPatterns = patterns.map((item) => item.trim()).filter(Boolean);
    const marks = document.getElementsByClassName('status-check-matched-mark');

    for (const el of marks) {
      let matched = false;
      const statusCheck = el.getAttribute('data-status-check');
      for (const pattern of validPatterns) {
        if (minimatch(statusCheck, pattern)) {
          matched = true;
          break;
        }
      }
      toggleElem(el, matched);
    }
  };
  markMatchedStatusChecks();
  document.getElementById('status_check_contexts').addEventListener('input', onInputDebounce(markMatchedStatusChecks));
}

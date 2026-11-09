// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {initGlobalShowModal} from './show-modal.ts';
import {showModal} from '../modules/modal.ts';

vi.mock('../modules/modal.ts', () => ({showModal: vi.fn()}));

test('show-modal ticks and clears checkboxes from data-modal attributes', () => {
  document.body.innerHTML = `
    <dialog id="m">
      <input type="checkbox" id="scope-checks">
      <input type="checkbox" id="scope-agents" checked>
      <input type="radio" id="pick">
      <input id="name">
      <span class="title"></span>
    </dialog>
    <button class="show-modal" data-modal="#m"
      data-modal-scope-checks="true" data-modal-scope-agents="false" data-modal-pick="true"
      data-modal-name="REGION" data-modal-title="Edit"></button>`;
  initGlobalShowModal();
  document.querySelector('button').click();

  expect(document.querySelector('#scope-checks').checked).toBe(true);
  expect(document.querySelector('#scope-agents').checked).toBe(false);
  expect(document.querySelector('#pick').checked).toBe(true);
  expect(document.querySelector('#name').value).toBe('REGION');
  expect(document.querySelector('.title').textContent).toBe('Edit');
  expect(showModal).toHaveBeenCalledTimes(1);
});

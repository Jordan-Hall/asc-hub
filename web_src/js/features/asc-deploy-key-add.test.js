import {initAscDeployKeyAdd} from './asc-deploy-key-add.js';

function page(reopen) {
  document.body.innerHTML = `<button data-dk-add-open>Add deploy key</button>
<dialog id="dk-add-dlg"${reopen ? ' data-dk-add-reopen' : ''}><form><input name="title">
<textarea name="content"${reopen ? ' aria-invalid="true"' : ''}></textarea><button type="button" data-dk-add-close>Cancel</button></form></dialog>`;
  const dialog = document.querySelector('dialog');
  dialog.showModal = vi.fn();
  dialog.close = vi.fn();
  return dialog;
}

test('Add deploy key opens the dialog on the title and Cancel closes it', () => {
  const dialog = page(false);
  initAscDeployKeyAdd();
  expect(dialog.showModal).not.toHaveBeenCalled();
  document.querySelector('[data-dk-add-open]').click();
  expect(dialog.showModal).toHaveBeenCalled();
  expect(document.activeElement).toBe(dialog.querySelector('input[name="title"]'));
  dialog.querySelector('[data-dk-add-close]').click();
  expect(dialog.close).toHaveBeenCalled();
});

test('a page sent back with a refused field opens the dialog on that field', () => {
  const dialog = page(true);
  initAscDeployKeyAdd();
  expect(dialog.showModal).toHaveBeenCalled();
  expect(document.activeElement).toBe(dialog.querySelector('textarea'));
});

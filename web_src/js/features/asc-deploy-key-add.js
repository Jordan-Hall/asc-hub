// The Add deploy key dialog of the access page (templates/repo/settings/deploy_keys_section.tmpl). Add deploy key opens
// it and the form posts to /settings/keys. When the server sends the page back with a refused field the dialog opens
// again on it.

export function initAscDeployKeyAdd() {
  const dialog = document.querySelector('#dk-add-dlg');
  if (!dialog) return;
  const title = dialog.querySelector('input[name="title"]');
  const open = () => {
    dialog.showModal();
    (dialog.querySelector('[aria-invalid="true"]') ?? title).focus();
  };
  for (const btn of document.querySelectorAll('[data-dk-add-open]')) btn.addEventListener('click', open);
  for (const btn of dialog.querySelectorAll('[data-dk-add-close]')) btn.addEventListener('click', () => dialog.close());
  if (dialog.hasAttribute('data-dk-add-reopen')) open();
}

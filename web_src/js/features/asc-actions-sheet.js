// The actions of a change on a phone (templates/repo/pulls/asc_panel.tmpl, board Mobile-Review): the dots button of the
// pinned verdict bar opens a sheet with Approve, Request changes, the queue, Undo restack and the asc command. Each item
// keeps its own behaviour (a form, the review box of asc-review.js, the confirmation sheet, a link, a copy); the sheet
// only closes first so the next step is not under it.

export function initAscActionsSheet(doc = document) {
  const sheet = doc.querySelector('[data-asc-actions-sheet]');
  const open = doc.querySelector('[data-asc-actions-open]');
  if (!sheet || !open) return;
  open.addEventListener('click', () => {
    if (sheet.open) return;
    sheet.showModal();
    sheet.querySelector('.rv-menu-item')?.focus();
  });
  sheet.addEventListener('click', (e) => {
    if (e.target === sheet) {
      sheet.close(); // a tap on the backdrop
      return;
    }
    if (e.target.closest('.rv-menu-item')) sheet.close();
  });
  sheet.addEventListener('close', () => open.focus());
}

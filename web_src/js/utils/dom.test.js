import {createElementFromHTML, formSubmitAction} from './dom.js';

test('createElementFromHTML', () => {
  expect(createElementFromHTML('<a>foo<span>bar</span></a>').outerHTML).toEqual('<a>foo<span>bar</span></a>');
});

test('formSubmitAction', () => {
  const form = createElementFromHTML(`<form method="post" action="/o/r/pulls/3/files/reviews/submit">
    <button type="submit" formaction="/-/asc/inbox/ask">Ask agent to change</button>
    <button type="submit" name="type" value="comment">Reply</button></form>`);
  const [ask, reply] = form.querySelectorAll('button');
  expect(formSubmitAction(form, ask)).toEqual('/-/asc/inbox/ask');
  expect(formSubmitAction(form, reply)).toEqual('/o/r/pulls/3/files/reviews/submit');
  expect(formSubmitAction(form, null)).toEqual('/o/r/pulls/3/files/reviews/submit');
});

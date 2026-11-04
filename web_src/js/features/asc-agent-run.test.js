import {applyWorkflowState, reconnectText} from './asc-agent-run.ts';

test('the reconnect line names the time of the last answer', () => {
  const at = new Date(2026, 10, 4, 10, 14, 30);
  expect(reconnectText('Showing the log as of {time}. It catches up on its own.', at, 'en-GB')).toBe('Showing the log as of 10:14. It catches up on its own.');
  expect(reconnectText('{time}', at, 'en-US')).toMatch(/^10:14\sAM$/);
  expect(reconnectText('no time here', at, 'en-GB')).toBe('no time here');
});

function workflowPage() {
  document.body.innerHTML = `<div id="r">
    <span data-run-state><span class="asc-pill asc-tone-run" data-state="running">Running</span></span>
    <div data-run-failure class="tw-hidden"></div>
    <ol>
      <li class="wf-node wf-tone-ok" data-flow-step="scan"><span data-flow-state><span class="asc-pill"></span></span><div data-flow-sub>Passed in 4 s</div></li>
      <li class="wf-node wf-tone-run is-active" data-flow-step="update"><span data-flow-state><span class="asc-pill"></span></span><div data-flow-sub>Running for 2 s</div></li>
    </ol>
    <ol>
      <li data-step="scan"><span data-step-state><span class="asc-pill" data-state="succeeded"></span></span><div data-step-error></div></li>
      <li data-step="update"><span data-step-state><span class="asc-pill" data-state="running"></span></span><div data-step-error></div></li>
    </ol>
  </div>`;
  return document.querySelector('#r');
}

const runningState = {
  state: 'running', label: 'Running', tone: 'run', active: true, meters: [],
  steps: [
    {id: 'scan', state: 'succeeded', label: 'Succeeded', tone: 'ok', active: false, sub: 'Passed in 4 s', linked: false},
    {id: 'update', state: 'succeeded', label: 'Succeeded', tone: 'ok', active: false, sub: '3 changes proposed, $1.12', linked: true},
  ],
};

test('the flow redraws the tone, the line and the connector of each step', () => {
  const root = workflowPage();
  expect(applyWorkflowState(root, runningState)).toBe(false);
  const node = root.querySelector('[data-flow-step="update"]');
  expect(node.className).toBe('wf-node wf-tone-ok is-linked');
  expect(node.querySelector('[data-flow-sub]').textContent).toBe('3 changes proposed, $1.12');
  expect(node.querySelector('.asc-pill').getAttribute('data-state')).toBe('succeeded');
  expect(root.querySelector('[data-flow-step="scan"]').className).toBe('wf-node wf-tone-ok');
});

test('the flow keeps an unknown tone out of the class', () => {
  const root = workflowPage();
  const st = structuredClone(runningState);
  st.steps[1].tone = 'x" onclick="';
  applyWorkflowState(root, st);
  expect(root.querySelector('[data-flow-step="update"]').className).toBe('wf-node wf-tone-idle is-linked');
});

test('a step that starts waiting for a decision asks for a redraw', () => {
  const root = workflowPage();
  const st = structuredClone(runningState);
  st.steps[1] = {...st.steps[1], state: 'waiting', tone: 'wait', active: true, linked: true};
  expect(applyWorkflowState(root, st)).toBe(true);
});

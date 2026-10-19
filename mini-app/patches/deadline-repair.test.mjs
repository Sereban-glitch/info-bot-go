import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

// Fixed local calendar and the real shipped date/selector helpers keep these
// fixtures independent of today's date and of a simplified workday mock.
process.env.TZ = 'UTC';
const now = Date.parse('2026-10-19T10:00:00Z');
class FixedDate extends Date {
  constructor(...args) { super(...(args.length ? args : [now])); }
  static now() { return now; }
}
const source = name => fs.readFileSync(new URL(name, import.meta.url), 'utf8');
const html = fs.readFileSync(new URL('../../internal/web/static/index.html', import.meta.url), 'utf8');
const asset = html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
const bundle = fs.readFileSync(new URL('../../internal/web/static/assets/' + asset, import.meta.url), 'utf8');
function shipped(start, end) {
  const offset = bundle.lastIndexOf(start);
  assert.ok(offset >= 0, 'Missing shipped helper: ' + start);
  const limit = bundle.indexOf(end, offset);
  assert.ok(limit > offset, 'Missing helper boundary: ' + end);
  return bundle.slice(offset, limit);
}
function app(requests = []) {
  const state = {requests, profile: {firstName: 'Тест'}};
  const context = vm.createContext({
    Date: FixedDate, URL, Promise, console,
    F: {jsx: (type, props) => ({type, props})},
    _: {
      createContext: value => ({Provider: 'ContextProvider', value}),
      useContext: () => ({ready: true, error: false, loading: false, retry() {}}),
      useState: initial => [initial, () => {}], useEffect: () => {},
      useRef: initial => ({current: initial}),
    },
    go: selector => selector(state),
    Vr: 'RouterLink', _s: 'StatusBadge', Ds: value => value,
  });
  context.go.getState = () => state;
  vm.runInContext(shipped('function co(', 'var po='), context);
  for (const name of ['request-status.js', 'request-summary.js', 'blue-shell.js', 'bot-navigation.js', 'blue-request-card.js', 'blue-home.js']) {
    vm.runInContext(source(name), context, {filename: name});
  }
  vm.runInContext(shipped('function Cs(', 'var Ts='), context);
  return context;
}
function nodes(node) {
  if (node == null || typeof node === 'boolean') return [];
  if (Array.isArray(node)) return node.flatMap(nodes);
  if (typeof node !== 'object') return [node];
  if (typeof node.type === 'function') return nodes(node.type(node.props));
  return [node, ...nodes(node.props?.children)];
}
const text = node => nodes(node).filter(value => typeof value === 'string' || typeof value === 'number').join(' ');
const byClass = (node, className) => nodes(node).filter(value => value?.props?.className?.split(' ').includes(className));
function fixture(context, id, createdAt, extra = {}) {
  return {
    id, subject: 'Fixture ' + id, recipientName: 'Fixture authority',
    createdAt, deadline: context.fo(new FixedDate(createdAt), 5).toISOString(),
    status: 'pending', channel: 'dostup', ...extra,
  };
}
const cases = [
  ['future', '2026-10-19T09:00:00Z', '7 днів', 'pending'],
  ['today', '2026-10-12T12:00:00Z', 'Сьогодні', 'pending'],
  ['tomorrow', '2026-10-13T12:00:00Z', '1 день', 'pending'],
  ['overdue', '2026-10-05T09:00:00Z', '+7 днів', 'overdue'],
];

for (const [id, createdAt, label, status] of cases) {
  test(id + ' card restores relative deadline and a separate labelled sent date', () => {
    const context = app(), request = fixture(context, id, createdAt), card = context.xs({req: request});
    assert.equal(card.props['data-status'], status);
    assert.equal(card.type, 'RouterLink');
    assert.equal(card.props.to, '/requests/$requestId');
    assert.equal(card.props.params.requestId, id);
    const countdown = byClass(card, 'sz-request-countdown');
    assert.equal(countdown.length, 1, 'outstanding card needs one visible estimate');
    assert.ok(text(countdown[0]).includes('Орієнтир'));
    assert.ok(text(countdown[0]).includes(label), text(countdown[0]));
    assert.ok(countdown[0].props['aria-label'].includes(context.co(request.deadline)));
    if (id === 'future') assert.equal(countdown[0].props['data-tone'], 'muted');
    if (id === 'overdue') assert.equal(countdown[0].props['data-tone'], 'bad');
    if (id === 'today' || id === 'tomorrow') assert.equal(countdown[0].props['data-tone'], 'warn');
    const sent = byClass(card, 'sz-request-date');
    assert.equal(sent.length, 1);
    assert.match(text(sent[0]), /Надіслано/i);
    assert.ok(text(sent[0]).includes(context.co(createdAt)));
  });
}

test('every received outcome and both bounce forms suppress deadline countdowns', () => {
  const context = app();
  for (const extra of [
    {status: 'replied', lastStatus: 'successful'},
    {status: 'replied', lastStatus: 'rejected'},
    {status: 'replied', lastStatus: 'partially_successful'},
    {status: 'replied', lastStatus: 'waiting_classification'},
    {status: 'unrated'}, {status: 'refused'}, {status: 'partial'},
    {status: 'bounced'}, {status: 'pending', lastStatus: 'error_message'},
  ]) {
    const request = fixture(context, 'closed', '2026-10-05T09:00:00Z', {...extra, repliedAt: '2026-10-18T09:00:00Z'});
    const card = context.xs({req: request});
    assert.equal(context.szDeadlineInfo(request), null, JSON.stringify(extra));
    assert.equal(byClass(card, 'sz-request-countdown').length, 0, JSON.stringify(extra));
    assert.ok(!text(card).includes('+7 днів'));
    const sent = byClass(card, 'sz-request-date');
    assert.equal(sent.length, 1);
    assert.match(text(sent[0]), /Надіслано/i);
    assert.ok(text(sent[0]).includes(context.co(request.createdAt)));
    if (context._o(request) === 'bounced') {
      assert.ok(!text(card).includes(context.co(request.repliedAt)), 'bounce timestamp cannot be a reply date');
    }
  }
});

test('received records without a receipt date never acquire a countdown', () => {
  const context = app();
  for (const status of ['replied', 'unrated', 'refused', 'partial', 'bounced']) {
    const request = fixture(context, status, '2026-10-05T09:00:00Z', {status});
    assert.equal(context.szDeadlineInfo(request), null);
    assert.equal(byClass(context.xs({req: request}), 'sz-request-countdown').length, 0);
  }
});

test('home keeps nearest outstanding deadline independently from an older latest reply', () => {
  const context = app();
  const pending = fixture(context, 'pending', '2026-10-19T09:00:00Z');
  const reply = fixture(context, 'reply', '2026-10-05T09:00:00Z', {status: 'replied', lastStatus: 'successful', repliedAt: '2026-10-18T09:00:00Z'});
  context.go.getState().requests = [reply, pending];
  const home = context.Es(), nearest = byClass(home, 'sz-nearest-deadline');
  assert.equal(byClass(home, 'sz-next-action')[0].props.params.requestId, reply.id, 'existing hero choice stays intact');
  assert.equal(nearest.length, 1, 'latest reply must not conceal the pending deadline');
  assert.equal(nearest[0].props.to, '/requests/$requestId');
  assert.equal(nearest[0].props.params.requestId, pending.id);
  assert.ok(text(nearest[0]).includes('Найближчий строк'));
  assert.ok(text(nearest[0]).includes(pending.subject));
  assert.ok(text(nearest[0]).includes('7 днів'));
  assert.ok(text(nearest[0]).includes(context.co(pending.deadline)));
  assert.ok(text(nearest[0]).includes('5 робочих днів'));
  assert.ok(text(nearest[0]).includes('можливе продовження'));
  assert.equal(byClass(home, 'sz-latest-event')[0].props.params.requestId, reply.id);
});

test('nearest outstanding panel selects actual earliest deadline even outside recent three', () => {
  const context = app();
  const early = fixture(context, 'earliest', '2026-10-05T09:00:00Z');
  const later = ['2026-10-06T09:00:00Z', '2026-10-07T09:00:00Z', '2026-10-08T09:00:00Z'].map((date, index) => fixture(context, 'later-' + index, date));
  context.go.getState().requests = [...later, early];
  const home = context.Es(), nearest = byClass(home, 'sz-nearest-deadline');
  assert.equal(nearest.length, 1);
  assert.equal(nearest[0].props.params.requestId, early.id);
  assert.ok(text(nearest[0]).includes('+7 днів'));
  assert.ok(!byClass(home, 'sz-request-card').some(card => card.props.params.requestId === early.id), 'fixture must keep earliest outside recent three');
});

test('home has no nearest deadline panel for received-only, bounced-only or empty accounts', () => {
  const context = app();
  for (const requests of [
    [],
    [fixture(context, 'reply', '2026-10-05T09:00:00Z', {status: 'replied', repliedAt: '2026-10-18T09:00:00Z'})],
    [fixture(context, 'bounce', '2026-10-05T09:00:00Z', {status: 'bounced', repliedAt: '2026-10-18T09:00:00Z'})],
  ]) {
    context.go.getState().requests = requests;
    assert.equal(byClass(context.Es(), 'sz-nearest-deadline').length, 0);
    assert.equal(byClass(context.Es(), 'sz-request-countdown').length, 0);
  }
});

test('missing or malformed deadline never produces a fabricated relative countdown', () => {
  const context = app();
  for (const deadline of [undefined, '', 'not-a-date']) {
    const request = fixture(context, 'invalid-date', '2026-10-19T09:00:00Z', {deadline});
    assert.equal(context.szDeadlineInfo(request), null);
    assert.equal(byClass(context.xs({req: request}), 'sz-request-countdown').length, 0);
  }
});

test('deadline source changes are embedded in the served content-hashed bundle', () => {
  for (const name of ['blue-request-card.js', 'blue-home.js']) {
    assert.equal(bundle.split(source(name)).length, 2, name + ' must be embedded exactly once');
  }
});

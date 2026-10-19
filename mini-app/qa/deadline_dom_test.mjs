// Deadline regressions against the exact served React/router bundle. Synthetic
// read-only APIs only; jsdom does not establish screenshot or layout correctness.
// SMARTZAPYT_QA_NODE_MODULES points at an external package containing jsdom.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import {fileURLToPath} from 'node:url';

process.env.TZ = 'UTC';
const require = createRequire(process.env.SMARTZAPYT_QA_NODE_MODULES
  ? path.join(process.env.SMARTZAPYT_QA_NODE_MODULES, '../package.json') : import.meta.url);
const {JSDOM, VirtualConsole} = require('jsdom');
const root = fileURLToPath(new URL('../../', import.meta.url));
const output = path.join(root, 'mini-app/qa/results');
fs.mkdirSync(output, {recursive: true});
const html = fs.readFileSync(path.join(root, 'internal/web/static/index.html'), 'utf8');
const asset = html.match(/src="\/assets\/(index-status-[a-f0-9]+\.js)"/)[1];
const bundle = fs.readFileSync(path.join(root, 'internal/web/static/assets', asset), 'utf8');
const fixed = Date.parse('2026-10-19T10:00:00Z');
const common = {recipientName: 'Fixture authority', delivered: true, channel: 'dostup', url: 'https://dostup.org.ua/request/fixture'};
const request = (id, date, extra = {}) => ({...common, id, subject: 'Fixture ' + id, date, status: 'pending', ...extra});
const future = request('future', '2026-10-19T09:00:00Z');
const today = request('today', '2026-10-12T12:00:00Z');
const tomorrow = request('tomorrow', '2026-10-13T12:00:00Z');
const overdue = request('overdue', '2026-10-05T09:00:00Z');
const reply = request('reply', '2026-10-05T09:00:00Z', {status: 'replied', lastStatus: 'successful', replyReceivedAt: '2026-10-18T09:00:00Z', responseExcerpt: 'Literal fixture answer'});
const bounced = request('bounced', '2026-10-05T09:00:00Z', {status: 'bounced', lastStatus: 'error_message', replyReceivedAt: '2026-10-18T09:00:00Z'});
const report = {passed: false, engine: 'jsdom', visualValidation: false, bundle: asset, fixedTime: new Date(fixed).toISOString(), checks: [], requests: [], errors: []};
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function waitFor(predicate, label) {
  for (let attempt = 0; attempt < 500; attempt++) {
    if (predicate()) return;
    await delay(10);
  }
  throw new Error('Timed out: ' + label);
}
const date = value => new Date(value).toLocaleDateString('uk-UA', {day: 'numeric', month: 'short', year: 'numeric'});
async function mount(rows, pathname, verify) {
  const errors = [], virtualConsole = new VirtualConsole();
  virtualConsole.on('jsdomError', error => errors.push(error.stack || String(error)));
  virtualConsole.on('error', (...args) => errors.push(args.map(String).join(' ')));
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
    url: 'https://smartzapyt.test' + pathname, runScripts: 'outside-only', pretendToBeVisual: true, virtualConsole,
  });
  const {window} = dom, document = window.document, RealDate = window.Date;
  window.Date = class extends RealDate {
    constructor(...args) { super(...(args.length ? args : [fixed])); }
    static now() { return fixed; }
  };
  window.scrollTo = () => {};
  window.matchMedia = media => ({matches: false, media, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}});
  window.Request = Request; window.Response = Response; window.Headers = Headers;
  window.structuredClone = structuredClone;
  window.Telegram = {WebApp: {initData: 'SYNTHETIC_NO_REAL_TOKEN', initDataUnsafe: {user: {first_name: 'Тест'}}}};
  const api = {
    '/api/requests': rows, '/api/me': {firstName: 'Тест'},
    '/api/portal-stats': {requests: 100, successful: 40, organs: 20},
    '/api/rating': {items: [{name: 'Fixture authority', requests: 100, successful: 40, overdue: 7, slug: 'fixture'}]},
    '/api/analytics': {total: rows.length, answered: 1, ackOnly: 1, awaiting: 1, bounced: 1},
  };
  window.fetch = async (input, options = {}) => {
    const url = new URL(String(input), window.location.href), method = options.method || 'GET';
    report.requests.push({scene: pathname, path: url.pathname, method});
    assert.equal(url.origin, 'https://smartzapyt.test', 'no external network is permitted');
    assert.equal(method, 'GET', 'all fixture requests must be read-only');
    assert.ok(Object.hasOwn(api, url.pathname), 'unexpected API ' + url.pathname);
    return new Response(JSON.stringify({ok: true, data: api[url.pathname]}), {status: 200, headers: {'Content-Type': 'application/json'}});
  };
  window.addEventListener('error', event => errors.push(String(event.error || event.message)));
  window.addEventListener('unhandledrejection', event => errors.push(String(event.reason)));
  const click = element => {
    assert.ok(element, 'click target missing');
    element.dispatchEvent(new window.MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  };
  try {
    window.eval(bundle);
    await waitFor(() => rows.length
      ? [...document.querySelectorAll('.sz-request-card')].some(card => card.textContent.includes(rows[0].subject))
      : pathname === '/requests' ? document.querySelector('.sz-main')?.textContent.includes('Запитів немає')
        : document.querySelector('.sz-empty-hero')?.textContent.includes('Ваша інформація починається із запиту'), 'synchronized ' + pathname);
    await verify({window, document, click});
    assert.deepEqual(errors, [], 'runtime errors');
  } finally {
    report.errors.push(...errors);
    window.close();
  }
}
function assertCard(card, row, label, tone) {
  assert.ok(card, 'missing card: ' + row.id);
  assert.equal(card.getAttribute('href'), '/requests/' + row.id);
  const sent = card.querySelector('.sz-request-date');
  assert.ok(sent, 'missing sent date: ' + row.id);
  assert.match(sent.textContent, /Надіслано/i);
  assert.ok(sent.textContent.includes(date(row.date)));
  const countdown = card.querySelector('.sz-request-countdown');
  if (label) {
    assert.ok(countdown, 'missing countdown: ' + row.id);
    assert.ok(countdown.textContent.includes('Орієнтир'));
    assert.ok(countdown.textContent.includes(label));
    assert.equal(countdown.dataset.tone, tone);
    assert.match(countdown.getAttribute('aria-label'), /Розрахунковий строк/);
  } else assert.equal(countdown, null, 'false countdown on ' + row.id);
}

try {
  await mount([reply, future], '/', async ({window, document, click}) => {
    const hero = document.querySelector('.sz-next-action'), nearest = document.querySelector('.sz-nearest-deadline');
    assert.equal(hero.getAttribute('href'), '/requests/reply');
    assert.ok(nearest, 'nearest deadline must remain visible beside an older reply hero');
    assert.equal(nearest.getAttribute('href'), '/requests/future');
    for (const value of ['Найближчий строк', future.subject, '7 днів', date('2026-10-26T09:00:00Z'), '5 робочих днів', 'можливе продовження']) {
      assert.ok(nearest.textContent.includes(value), 'nearest panel missing ' + value);
    }
    assert.equal(document.querySelector('.sz-latest-event').getAttribute('href'), '/requests/reply');
    assertCard(document.querySelector('.sz-request-card[href="/requests/future"]'), future, '7 днів', 'muted');
    assertCard(document.querySelector('.sz-request-card[href="/requests/reply"]'), reply);
    const botCalls = [];
    window.Telegram.WebApp.openTelegramLink = (...args) => botCalls.push(['openTelegramLink', ...args]);
    window.Telegram.WebApp.close = (...args) => botCalls.push(['close', ...args]);
    window.Telegram.WebApp.sendData = () => assert.fail('generic bot navigation must never send request data');
    const homeBot = document.querySelector('.sz-home .sz-primary-action');
    assert.equal(homeBot.getAttribute('href'), 'https://t.me/Infozaputbot');
    click(homeBot);
    assert.deepEqual(botCalls, [['openTelegramLink', 'https://t.me/Infozaputbot'], ['close']]);
    click(nearest);
    await waitFor(() => window.location.pathname === '/requests/future' && document.querySelector('.sz-detail h1')?.textContent === future.subject, 'nearest deadline detail navigation');
    assert.ok(document.querySelector('.sz-timeline').textContent.includes('Розрахунковий строк'));
    assert.equal(document.querySelector('.sz-response-excerpt'), null);
    botCalls.length = 0;
    const detailBot = document.querySelector('.sz-detail .sz-primary-action');
    assert.equal(detailBot.getAttribute('href'), 'https://t.me/Infozaputbot');
    assert.equal(detailBot.textContent.trim(), 'Відкрити чат бота');
    click(detailBot);
    assert.deepEqual(botCalls, [['openTelegramLink', 'https://t.me/Infozaputbot'], ['close']]);
    report.checks.push('Home and detail bot CTAs call openTelegramLink then close with only the generic bot URL and no request payload');
    report.checks.push('mixed older reply and pending: independent nearest panel, date/caveat, latest reply, exact detail navigation');
  });

  const rows = [future, today, tomorrow, overdue, reply, bounced,
    request('refusal', '2026-10-05T09:00:00Z', {status: 'replied', lastStatus: 'rejected', replyReceivedAt: '2026-10-17T09:00:00Z'}),
    request('partial', '2026-10-05T09:00:00Z', {status: 'replied', lastStatus: 'partially_successful', replyReceivedAt: '2026-10-17T09:00:00Z'}),
    request('unrated', '2026-10-05T09:00:00Z', {status: 'replied', lastStatus: 'waiting_classification'}),
  ];
  await mount(rows, '/requests', async ({document, click}) => {
    assert.equal(document.querySelectorAll('.sz-request-card').length, rows.length);
    for (const [row, label, tone] of [[future, '7 днів', 'muted'], [today, 'Сьогодні', 'warn'], [tomorrow, '1 день', 'warn'], [overdue, '+7 днів', 'bad'], ...rows.slice(4).map(row => [row])]) {
      const card = document.querySelector('.sz-request-card[href="/requests/' + row.id + '"]');
      assertCard(card, row, label, tone);
      if (row.id === 'bounced') {
        assert.equal(card.querySelector('.sz-request-reply-date'), null);
        assert.ok(!card.textContent.includes(date(row.replyReceivedAt)), 'bounce timestamp must not appear as an answer');
      }
    }
    report.checks.push('Requests: today/tomorrow/future/overdue labels, urgency, sent dates; reply/refusal/partial/unrated/bounce without false countdown');
    for (const [label, ids] of [
      ['Очікують', ['future', 'today', 'tomorrow']], ['Прострочені', ['overdue']],
      ['Відповіді', ['reply', 'refusal', 'partial', 'unrated']], ['Відмови', ['refusal']],
      ['Частково', ['partial']], ['Усі', rows.map(row => row.id)],
    ]) {
      click([...document.querySelectorAll('button')].find(button => button.textContent.trim() === label));
      await waitFor(() => document.querySelectorAll('.sz-request-card').length === ids.length, label + ' filter');
      const actual = [...document.querySelectorAll('.sz-request-card')].map(card => card.getAttribute('href').split('/').pop()).sort();
      assert.deepEqual(actual, [...ids].sort());
    }
    report.checks.push('all six existing request filters preserve status semantics');
    click(document.querySelector('.sz-sidebar a[href="/rating"]'));
    await waitFor(() => document.querySelector('.sz-rating-success'), 'rating percentage');
    assert.ok(document.querySelector('.sz-rating-success').textContent.includes('40%'));
    assert.ok(document.querySelector('.sz-main').textContent.includes('Успішних: 40'));
    click(document.querySelector('.sz-sidebar a[href="/analytics"]'));
    await waitFor(() => document.querySelector('.sz-main')?.textContent.includes('Тільки автовідповідь'), 'corrected analytics label');
    assert.ok(!document.querySelector('.sz-main').textContent.includes('Тільки автоавтовідповідь'));
    report.checks.push('rating shows successful/all 40% and analytics retains corrected auto-reply label');
  });

  await mount([future], '/requests', async ({document, click}) => {
    click([...document.querySelectorAll('button')].find(button => button.textContent.trim() === 'Відмови'));
    await waitFor(() => document.querySelector('.sz-reset-filter'), 'empty category reset action');
    assert.equal(document.querySelectorAll('.sz-request-card').length, 0);
    assert.ok(document.querySelector('.sz-main').textContent.includes('У цій категорії запитів немає'));
    assert.ok(!document.querySelector('.sz-main').textContent.includes('Створіть запит у Telegram-боті'));
    assert.equal([...document.querySelectorAll('button[aria-pressed]')].length, 6, 'all category controls remain available');
    click(document.querySelector('.sz-reset-filter'));
    await waitFor(() => document.querySelector('.sz-request-card[href="/requests/future"]'), 'reset to all requests');
    assert.equal([...document.querySelectorAll('button')].find(button => button.textContent.trim() === 'Усі').getAttribute('aria-pressed'), 'true');
  });
  await mount([], '/requests', async ({document}) => {
    assert.equal(document.querySelector('.sz-reset-filter'), null);
    assert.ok(document.querySelector('.sz-main').textContent.includes('Запитів немає'));
    assert.ok(!document.querySelector('.sz-main').textContent.includes('У цій категорії запитів немає'));
  });
  report.checks.push('empty category keeps all six filters and functional reset; empty account remains distinct');

  for (const [row, label, tone] of [[today, 'Сьогодні', 'warn'], [tomorrow, '1 день', 'warn'], [overdue, '+7 днів', 'bad']]) {
    await mount([row, reply], '/', async ({document}) => {
      const nearest = document.querySelector('.sz-nearest-deadline');
      assert.ok(nearest);
      assert.equal(nearest.getAttribute('href'), '/requests/' + row.id);
      assert.ok(nearest.textContent.includes(label));
      assert.equal(nearest.querySelector('[data-tone]').dataset.tone, tone);
      assertCard(document.querySelector('.sz-request-card[href="/requests/' + row.id + '"]'), row, label, tone);
    });
  }
  report.checks.push('Home repeats today/tomorrow/overdue countdown and urgency with a separate latest reply');

  for (const rows of [[reply], [bounced], []]) {
    await mount(rows, '/', async ({document}) => {
      assert.equal(document.querySelector('.sz-nearest-deadline'), null);
      assert.equal(document.querySelector('.sz-request-countdown'), null);
    });
  }
  report.checks.push('received-only, bounced-only and empty accounts have no fabricated outstanding deadline');

  const structured = [
    {...reply, id: 'table-preview', subject: 'Fixture table preview', responseExcerpt: 'Назва;Значення\n<img src=x onerror=alert(1)>;[ email address ]\nНеповний…'},
    {...reply, id: 'flat-preview', subject: 'Fixture flat preview', responseExcerpt: 'Назва;Значення;[ email address ];обірвано�'},
  ];
  await mount(structured, '/requests', async ({document, click}) => {
    click(document.querySelector('.sz-request-card[href="/requests/table-preview"]'));
    await waitFor(() => document.querySelector('.sz-response-table'), 'structured table preview');
    assert.equal(document.querySelectorAll('.sz-response-table tr').length, 2);
    assert.equal(document.querySelectorAll('.sz-response-table th').length, 0, 'no inferred column headers');
    assert.equal(document.querySelector('.sz-response-table img'), null, 'source content remains escaped');
    assert.ok(document.querySelector('.sz-response-table').textContent.includes('<img src=x onerror=alert(1)>'));
    assert.ok(document.querySelector('.sz-response-table').textContent.includes('[ email address ]'));
    assert.equal(document.querySelector('.sz-response-tail pre').textContent, 'Неповний…');
    assert.equal(document.querySelector('.sz-response-raw pre').textContent, structured[0].responseExcerpt);
    click(document.querySelector('.sz-back-link'));
    await waitFor(() => document.querySelector('.sz-request-card[href="/requests/flat-preview"]'), 'return to preview fixtures');
    click(document.querySelector('.sz-request-card[href="/requests/flat-preview"]'));
    await waitFor(() => document.querySelector('.sz-response-fields'), 'flat field preview');
    assert.equal(document.querySelector('.sz-response-table'), null, 'flattened CSV cannot invent a table');
    assert.equal(document.querySelectorAll('.sz-response-fields li').length, 4);
    assert.ok(document.querySelector('.sz-response-fields').textContent.includes('[ email address ]'));
    assert.ok(document.querySelector('.sz-response-notice').textContent.includes('Межі рядків не збережено'));
    assert.ok(document.querySelector('.sz-response-notice').textContent.includes('пошкоджені символи'));
    assert.equal(document.querySelector('.sz-response-raw pre').textContent, structured[1].responseExcerpt);
    assert.equal(document.querySelector('.sz-source-link').getAttribute('href'), reply.url);
  });
  report.checks.push('structured response preview keeps escaped cells, source redactions, exact raw data and incomplete tail; flat data remains sequential fields');
  report.passed = true;
  process.stdout.write('Deadline production-bundle DOM fixtures passed (' + report.checks.length + ' groups). Visual layout remains unverified.\n');
} catch (error) {
  report.error = error.stack || String(error);
  throw error;
} finally {
  fs.writeFileSync(path.join(output, 'deadline-dom-report.json'), JSON.stringify(report, null, 2));
}

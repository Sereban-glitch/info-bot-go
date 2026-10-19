// No new API, generated analysis, invented files, or assumed delivery events.
function szPortalURL(value) {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && ['dostup.org.ua', 'dostup.pravda.com.ua'].includes(url.hostname)
      && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}
function SmartRequestDetail() {
  const pathname = pi({select: state => state.location.pathname});
  const requests = go(state => state.requests);
  const {ready, error} = _.useContext(SmartSyncContext);
  let id = '';
  try { id = decodeURIComponent(pathname.split('/').filter(Boolean).at(-1) || ''); } catch {}
  const request = requests.find(item => item.id === id);
  if (!request) return sz('div', {className: 'sz-detail'},
    sz(Vr, {to: '/requests', className: 'sz-back-link'}, sz(SzIcon, {name: 'back'}), 'Усі запити'),
    sz('h1', {}, error ? 'Не вдалося завантажити запит' : ready ? 'Запит недоступний' : 'Завантаження запиту…'),
    sz('p', {className: 'sz-empty-copy', role: 'status'}, error ? 'Спробуйте оновити дані кнопкою вище.' : ready ? 'Цього запиту немає серед завантажених даних вашого облікового запису.' : 'Зачекайте, будь ласка.'));
  const status = _o(request);
  const received = ['replied', 'unrated', 'refused', 'partial'].includes(status);
  const portalURL = szPortalURL(request.url);
  const title = received ? 'Відповідь є. Перевірте, чи все зрозуміло.'
    : status === 'bounced' ? 'Запит не доставлено.' : 'Стежимо за відповіддю.';
  const nextStep = status === 'bounced' ? 'Перевірте адресу одержувача перед повторним надсиланням.'
    : received ? 'Порівняйте відповідь із питаннями вашого запиту. За потреби підготуйте уточнення у боті.'
      : 'Перевірте актуальний стан запиту у джерелі. Розрахункова дата не враховує продовження строку.';
  return sz('article', {className: 'sz-detail'},
    sz(Vr, {to: '/requests', className: 'sz-back-link'}, sz(SzIcon, {name: 'back'}), 'Деталі запиту'),
    sz('header', {className: 'sz-detail-heading'},
      sz('p', {className: 'sz-eyebrow'}, request.channel === 'dostup' ? 'Запит через портал' : 'Запит електронною поштою'),
      sz('h1', {}, request.subject), sz('p', {className: 'sz-detail-recipient'}, request.recipientName), sz(_s, {status})),
    sz('section', {className: 'sz-next-action sz-detail-hero'},
      sz('p', {className: 'sz-eyebrow'}, sz(SzIcon, {name: received ? 'check' : 'clock'}), 'Стан запиту'),
      sz('h2', {}, title),
      sz('div', {className: 'sz-next-step'}, sz('span', {className: 'sz-request-icon'}, sz(SzIcon, {name: 'arrow'})),
        sz('div', {}, sz('p', {className: 'sz-next-date-label'}, 'Можливий наступний крок'), sz('p', {}, nextStep)))),
    received ? sz('section', {className: 'sz-response-section'},
      sz('h2', {}, 'Фрагмент відповіді'),
      request.responseExcerpt ? sz('div', {className: 'sz-response-excerpt'}, request.responseExcerpt)
        : sz('p', {className: 'sz-empty-copy'}, 'Текст відповіді не передано до застосунку. Перегляньте його у джерелі.'),
      sz('p', {className: 'sz-small-muted'}, 'Вміст із журналу запитів. Це не AI-аналіз і не правова оцінка.')) : null,
    portalURL ? sz('a', {href: portalURL, target: '_blank', rel: 'noopener noreferrer', className: 'sz-source-link'},
      sz('span', {className: 'sz-request-icon'}, sz(SzIcon, {name: 'document'})),
      sz('span', {}, 'Запит і відповідь на порталі', sz('small', {}, 'Відкрити першоджерело')), sz(SzIcon, {name: 'arrow'})) : null,
    sz('section', {className: 'sz-timeline-section'}, sz('h2', {}, 'Шлях запиту'),
      sz('ol', {className: 'sz-timeline'},
        received ? sz('li', {className: 'is-received'}, sz('strong', {}, 'Відповідь отримано'),
          sz('span', {}, request.repliedAt ? co(request.repliedAt) : 'Дату відповіді не вказано'), sz('small', {}, po[status].label)) : null,
        status === 'bounced' ? sz('li', {}, sz('strong', {}, 'Не доставлено'), sz('small', {}, 'За даними журналу запитів')) : null,
        !received && status !== 'bounced' && request.deadline ? sz('li', {}, sz('strong', {}, 'Розрахунковий строк'),
          sz('span', {}, co(request.deadline)), sz('small', {}, '5 робочих днів від надсилання; можливе продовження.')) : null,
        sz('li', {}, sz('strong', {}, 'Запит надіслано'), sz('span', {}, co(request.createdAt) || 'Дату не вказано')))),
    sz('a', {href: 'https://t.me/Infozaputbot', target: '_blank', rel: 'noopener noreferrer', className: 'sz-primary-action'},
      sz(SzIcon, {name: 'arrow'}), 'Продовжити у боті'));
}
var SmartDetailRoute = Kr('/requests/$requestId')({component: SmartRequestDetail});

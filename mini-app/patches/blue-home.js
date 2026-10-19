// Readable home presentation using the existing user-scoped store and portal API.
function SzRequestLink({request, className, children}) {
  return sz(Vr, {to: '/requests/$requestId', params: {requestId: request.id}, className}, children);
}
function SzNewRequest() {
  // README's existing production bot; opening it does not send or create a request.
  return sz('a', {href: 'https://t.me/Infozaputbot', target: '_blank', rel: 'noopener noreferrer', onClick: szOpenBotChat,
    className: 'sz-primary-action'}, sz(SzIcon, {name: 'plus'}), 'Новий запит у боті');
}
function Es() {
  const requests = go(state => state.requests);
  const summary = Ss(requests);
  const nearest = Cs(requests);
  const latest = ws(requests);
  const nearestDeadline = nearest && szDeadlineInfo(nearest);
  const {ready, error} = _.useContext(SmartSyncContext);
  const [portal, setPortal] = _.useState(null);
  const [portalError, setPortalError] = _.useState(false);
  _.useEffect(() => {
    let alive = true;
    xo('/api/portal-stats').then(result => {
      if (!alive) return;
      if (result.ok && result.data) setPortal(result.data); else setPortalError(true);
    }).catch(() => { if (alive) setPortalError(true); });
    return () => { alive = false; };
  }, []);
  const next = nearest && _o(nearest) === 'overdue' ? nearest : latest || nearest || requests[0];
  const isReceipt = next && ['replied', 'unrated', 'refused', 'partial'].includes(_o(next));
  const isBounced = next && _o(next) === 'bounced';
  const nextDate = next ? (isReceipt ? next.repliedAt : isBounced ? next.createdAt : next.deadline) : null;
  const remaining = next && !isReceipt && !isBounced ? lo(next.deadline) : null;
  const active = [...requests].sort((a, b) => {
    const priorities = {overdue: 0, pending: 1, bounced: 2};
    return (priorities[_o(a)] ?? 3) - (priorities[_o(b)] ?? 3) || new Date(b.createdAt) - new Date(a.createdAt);
  }).slice(0, 3);
  return sz('div', {className: 'sz-home'},
    sz('header', {className: 'sz-home-heading'},
      sz('p', {className: 'sz-eyebrow'}, 'Ваш особистий простір'),
      sz('h1', {}, 'Ваші запити.', sz('span', {}, 'Під контролем.'))),
    sz('section', {className: 'sz-summary', 'aria-label': 'Ваші запити у цифрах'},
      sz('div', {}, sz('strong', {}, summary.total), sz('span', {}, 'Надіслано')),
      sz('div', {}, sz('strong', {}, summary.answered), sz('span', {}, 'Відповідей')),
      sz('div', {}, sz('strong', {}, summary.pending), sz('span', {}, 'Очікують'))),
    next ? sz(SzRequestLink, {request: next, className: 'sz-next-action'},
      sz('div', {className: 'sz-next-inner'},
        sz('div', {className: 'sz-next-top'}, sz('p', {className: 'sz-eyebrow'}, 'Наступний крок'),
          sz('span', {className: 'sz-outline-chip'}, isReceipt ? 'Є відповідь' : isBounced ? 'Не доставлено' : 'Перевірка стану')),
        sz('h2', {}, isReceipt ? 'Перевірити відповідь' : isBounced ? 'Перевірити доставку' : 'Перевірити стан запиту'),
        sz('p', {className: 'sz-next-subject'}, next.subject),
        sz('div', {className: 'sz-next-date-row'},
          sz('div', {}, sz('p', {className: 'sz-next-date'}, nextDate ? co(nextDate) : 'Дату не вказано'),
            sz('p', {className: 'sz-next-date-label'}, isReceipt ? 'Отримано відповідь' : isBounced ? 'Запит надіслано' : 'Розрахунковий строк')),
          sz('span', {className: 'sz-round-arrow'}, sz(SzIcon, {name: 'arrow'}))),
        !isReceipt ? sz('p', {className: 'sz-next-note'},
          isBounced ? 'Перевірте адресу одержувача перед повторним надсиланням.' : remaining === null ? 'Перевірте дату й стан у джерелі.' : remaining < 0 ? 'Розрахунковий строк минув. Перевірте актуальний стан.' : remaining === 0 ? 'Розрахунковий строк — сьогодні.' : `До розрахункового строку: ${remaining} ${U(remaining)}.`) : null))
      : sz('section', {className: 'sz-next-action sz-empty-hero'},
        sz('p', {className: 'sz-eyebrow'}, error ? 'Дані недоступні' : 'Почнімо з першого кроку'),
        sz('h2', {}, error ? 'Не вдалося завантажити запити.' : ready ? 'Ваша інформація починається із запиту.' : 'Завантажуємо ваші запити…'),
        sz('p', {className: 'sz-next-note'}, error ? 'Спробуйте оновити дані кнопкою вище.' : 'Створюйте запити у боті й стежте за відповідями тут.')),
    next && !isReceipt && !isBounced ? sz('p', {className: 'sz-calculation-note'},
      'Орієнтир: 5 робочих днів від надсилання. Не враховує продовження строку та особливості розгляду.') : null,
    nearest && nearestDeadline ? sz(SzRequestLink, {request: nearest, className: 'sz-nearest-deadline'},
      sz('div', {className: 'sz-nearest-heading'}, sz('h2', {}, 'Найближчий строк'),
        sz('strong', {'data-tone': nearestDeadline.tone}, nearestDeadline.label)),
      sz('p', {className: 'sz-nearest-subject'}, nearest.subject),
      sz('p', {className: 'sz-small-muted'}, `${nearestDeadline.date} · ${nearest.recipientName}`),
      sz('p', {className: 'sz-small-muted'}, 'Розрахунковий орієнтир: 5 робочих днів від надсилання; можливе продовження.')) : null,
    sz('section', {className: 'sz-work-section'},
      sz('div', {className: 'sz-section-heading'}, sz('h2', {}, 'Ваші справи'),
        sz(Vr, {to: '/requests', className: 'sz-text-link'}, 'Усі запити', sz(SzIcon, {name: 'chevron'}))),
      active.length ? sz('div', {className: 'sz-request-list'}, active.map(req => sz(xs, {key: req.id, req})))
        : sz('p', {className: 'sz-empty-copy', role: 'status'}, error ? 'Список не завантажено.' : ready ? 'Тут з’являться ваші надіслані запити.' : 'Завантаження…')),
    latest ? sz(SzRequestLink, {request: latest, className: 'sz-latest-event'},
      sz('span', {className: 'sz-event-dot', 'aria-hidden': true}),
      sz('div', {}, sz('p', {}, 'Остання відповідь'), sz('p', {}, `${co(latest.repliedAt)} · ${latest.subject}`)),
      sz(SzIcon, {name: 'chevron'})) : null,
    sz(SzNewRequest),
    sz('section', {className: 'sz-breakdown'},
      sz('div', {className: 'sz-section-heading'}, sz('h2', {}, 'Стан запитів'),
        sz('span', {className: 'sz-small-muted'}, 'Ваші дані')),
      sz('dl', {className: 'sz-status-grid'},
        [['Відповіді без відмов', summary.replied + summary.partial, 'ok'], ['Очікують', summary.pending, 'wait'],
          ['Прострочено', summary.overdue, 'bad'], ['Відмови', summary.refused, 'mute'],
          ['Не доставлено', summary.bounced, 'warn']].map(([label, value, tone]) =>
          sz('div', {key: label, 'data-tone': tone}, sz('dt', {}, label), sz('dd', {}, value))))),
    sz('section', {className: 'sz-portal'},
      sz('div', {className: 'sz-section-heading'}, sz('h2', {}, 'Статистика порталу'),
        sz(Vr, {to: '/rating', className: 'sz-text-link'}, 'Рейтинг', sz(SzIcon, {name: 'chevron'}))),
      sz('p', {className: 'sz-small-muted'}, 'Загальнонаціональні дані «Доступу до правди»'),
      portal ? sz('div', {className: 'sz-portal-stats'},
        [['Всього запитів', portal.requests], ['Успішних', portal.successful], ['Органів', portal.organs]].map(([label, value]) =>
          sz('div', {key: label}, sz('strong', {}, value ?? '—'), sz('span', {}, label))))
        : sz('p', {className: 'sz-empty-copy', role: 'status'}, portalError ? 'Статистика зараз недоступна.' : 'Завантаження…'),
      portal?.fetchedAt ? sz('p', {className: 'sz-small-muted sz-updated'}, `Оновлено ${Ds(portal.fetchedAt)}`) : null,
      sz(Vr, {to: '/analytics', className: 'sz-text-link sz-analytics-link'}, 'Аналітика журналу застосунку', sz(SzIcon, {name: 'arrow'}))));
}

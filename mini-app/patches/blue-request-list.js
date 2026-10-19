// Keep all six existing filters; distinguish an empty category from an empty account.
function szFilterRequests(requests, filter) {
  return filter === 'all' ? requests : requests.filter(request =>
    filter === 'replied' ? ['replied', 'unrated', 'refused', 'partial'].includes(_o(request)) : _o(request) === filter);
}
function Rs() {
  const requests = go(state => state.requests);
  const [filter, setFilter] = _.useState('all');
  const shown = _.useMemo(() => szFilterRequests(requests, filter), [requests, filter]);
  const {ready, error} = _.useContext(SmartSyncContext);
  const filteredEmpty = requests.length > 0;
  return sz('div', {},
    sz(Oo, {title: 'Запити', hint: 'Фільтруйте за статусом. Відкрийте запит, щоб переглянути відповідь і події. Строки розрахункові.'}),
    sz('div', {className: '-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 pb-1 [scrollbar-width:none]'},
      Ls.map(item => sz('button', {key: item.id, type: 'button', onClick: () => setFilter(item.id), 'aria-pressed': filter === item.id,
        className: H('h-9 shrink-0 rounded-full px-3.5 text-sm font-medium transition-colors duration-150',
          filter === item.id ? 'bg-primary text-primary-fg' : 'bg-raised text-muted shadow-[var(--shadow-border)]')}, item.label))),
    shown.length ? sz('div', {className: 'flex flex-col gap-2'}, shown.map(req => sz(xs, {key: req.id, req})))
      : sz(Fs, {icon: sz(Si, {className: 'size-8'}),
        title: filteredEmpty ? 'У цій категорії запитів немає' : error ? 'Не вдалося завантажити запити' : ready ? 'Запитів немає' : 'Завантаження…',
        hint: filteredEmpty ? 'Оберіть іншу категорію або поверніться до всіх запитів.' : error ? 'Спробуйте оновити дані кнопкою вище.' : ready ? 'Створіть запит у Telegram-боті Smart Zapyt. Надіслані запити з’являться тут.' : undefined,
        action: filteredEmpty ? sz('button', {type: 'button', className: 'sz-reset-filter', onClick: () => setFilter('all')}, 'Показати всі запити') : undefined}));
}

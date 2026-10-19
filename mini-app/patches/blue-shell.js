// Presentation-only integration. Existing authenticated sync functions stay intact.
function sz(type, props, ...children) {
  return F.jsx(type, {...(props || {}), ...(children.length ? {children: children.length === 1 ? children[0] : children} : {})});
}
function SzIcon({name = 'document', className = ''}) {
  const paths = {
    document: 'M7 3h7l4 4v14H7z M14 3v5h5 M10 12h5 M10 16h5',
    arrow: 'M6 18 18 6 M6 6h12v12',
    chevron: 'm9 5 7 7-7 7',
    back: 'm14 5-7 7 7 7',
    plus: 'M12 5v14 M5 12h14',
    bolt: 'm13 2-8 12h6l-1 8 9-13h-7z',
    check: 'm5 12 4 4L19 6',
    clock: 'M12 8v5l3 2 M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0'
  };
  return sz('svg', {viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.6,
    strokeLinecap: 'round', strokeLinejoin: 'round', className: `sz-icon ${className}`, 'aria-hidden': true},
    sz('path', {d: paths[name] || paths.document}));
}
const SmartSyncContext = _.createContext({ready: false, error: false, loading: true, retry: () => {}});
async function szSyncRequests() {
  // Co() catches transport errors. A valid response always replaces this array,
  // including successful empty results; an error leaves the previous data intact.
  const previous = go.getState().requests;
  await Co();
  return go.getState().requests !== previous;
}
function SzBrand() {
  return sz(Vr, {to: '/', className: 'sz-brand', 'aria-label': 'Smart Zapyt — огляд'},
    sz('span', {className: 'sz-brand-mark'}, sz(SzIcon, {name: 'bolt'})),
    sz('span', {}, 'smart zapyt'));
}
function Do({children}) {
  const pathname = pi({select: state => state.location.pathname});
  const profile = go(state => state.profile);
  const [sync, setSync] = _.useState({ready: false, error: false, loading: true});
  const mounted = _.useRef(false);
  const refresh = async () => {
    if (mounted.current) setSync(previous => ({...previous, loading: true}));
    const success = await szSyncRequests();
    if (mounted.current) setSync({ready: true, error: !success, loading: false});
  };
  _.useEffect(() => {
    mounted.current = true;
    refresh(); wo();
    const app = typeof window !== 'undefined' && window.Telegram?.WebApp;
    if (app && app.initDataUnsafe?.user) {
      const user = app.initDataUnsafe.user;
      go.getState().setProfile({firstName: user.first_name || '', lastName: user.last_name || '', middleName: ''});
    }
    const onVisible = () => { if (document.visibilityState === 'visible') { refresh(); wo(); } };
    document.addEventListener('visibilitychange', onVisible);
    return () => { mounted.current = false; document.removeEventListener('visibilitychange', onVisible); };
  }, []);
  const nav = () => To.map(item => sz(Vr, {
    key: item.to, to: item.to,
    className: `sz-nav-link${Eo(pathname, item.to, item.exact) ? ' is-active' : ''}`,
    'aria-current': Eo(pathname, item.to, item.exact) ? 'page' : undefined
  }, sz(item.icon, {className: 'sz-icon'}), sz('span', {}, item.label)));
  return sz('div', {className: 'sz-app min-h-dvh bg-bg text-fg'},
    sz('aside', {className: 'sz-sidebar'}, sz(SzBrand),
      sz('p', {className: 'sz-sidebar-caption'}, 'Ваш простір публічної інформації'),
      sz('nav', {'aria-label': 'Головна навігація'}, nav()),
      sz('p', {className: 'sz-sidebar-note'}, 'Запити, відповіді та статистика в одному місці.')),
    sz('div', {className: 'sz-content'},
      sz('header', {className: 'sz-app-header'}, sz(SzBrand),
        sz('span', {className: 'sz-avatar', title: uo(profile) || 'Smart Zapyt', 'aria-label': uo(profile) || 'Smart Zapyt'},
          [profile.firstName?.[0], profile.lastName?.[0]].filter(Boolean).join('').toUpperCase() || 'SZ')),
      sz('main', {className: 'sz-main'},
        sync.error ? sz('div', {className: 'sz-sync-error', role: 'alert'},
          sz('p', {}, 'Не вдалося оновити запити. Попередні дані, якщо є, залишено на екрані.'),
          sz('button', {type: 'button', onClick: refresh, disabled: sync.loading}, sync.loading ? 'Оновлення…' : 'Спробувати ще раз')) : null,
        sz(SmartSyncContext.Provider, {value: {...sync, retry: refresh}}, children))),
    sz('nav', {className: 'sz-bottom-nav', 'aria-label': 'Головна навігація'}, nav()));
}

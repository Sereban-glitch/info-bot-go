/**
 * app.js — Основная логика интерфейса Agro-Reader (Berthoud)
 */

let R = [];
let F = [];
let shown = 0;
const PAGE = 30;

let fB = '';
let fP = '';
let fS = '';
let bBrand = '';
let AICache = null;

let favs = {};
try {
  favs = JSON.parse(localStorage.getItem('agro_fav') || '{}');
} catch (e) {
  favs = {};
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"]/g, c => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;'
  }[c]));
}

// === SCREEN SWITCHING ===
function showScreen(s) {
  document.querySelectorAll('.nav-item').forEach(x => x.classList.toggle('active', x.dataset.s === s));
  document.querySelectorAll('.screen').forEach(x => x.classList.toggle('active', x.id === 'screen-' + s));
  if (s === 'fav') renderFav();
  if (s === 'battle') renderBattle();
}

document.querySelectorAll('.nav-item').forEach(b => {
  b.onclick = () => showScreen(b.dataset.s);
});

// === CHIP RENDERING ===
function chipRow(elId, vals, cur, setter) {
  const box = document.getElementById(elId);
  if (!box) return;
  box.innerHTML = '';
  vals.forEach(v => {
    const d = document.createElement('div');
    const isAct = (v[0] === cur);
    d.className = 'chip' + (isAct ? ' active' : '');
    d.textContent = v[0] + (v[1] ? ' · ' + v[1] : '');
    d.title = v[0];
    d.onclick = () => setter(v[0] === cur ? '' : v[0]);
    box.appendChild(d);
  });
}

function drawChips() {
  chipRow('fBrands', [...new Set(R.map(r => r.brand).filter(Boolean))].sort().map(v => [v]), fB, v => {
    fB = v; drawChips(); apply();
  });
  chipRow('fProbs', [...new Set(R.map(r => r.problem_label || r.problem_type).filter(Boolean))].sort().map(v => [v]), fP, v => {
    fP = v; drawChips(); apply();
  });
  chipRow('fSrcs', [...new Set(R.map(r => r.source).filter(Boolean))].sort().map(v => [v]), fS, v => {
    fS = v; drawChips(); apply();
  });
}

// === CARD HTML BUILDER ===
const GR = { A: 0, B: 1, C: 2, D: 3 };

function cardHTML(r) {
  const url = r.comment_url || r.source_url || '';
  const fav = favs[r.id] ? ' active' : '';
  return `
    <div class="review-header">
      <div class="review-brand">
        ${esc(r.brand || '')}
        ${r.evidence_grade ? `<span class="badge badge-grade">Grade ${esc(r.evidence_grade)}</span>` : ''}
        ${r.is_critical ? `<span class="badge badge-critical">Критично</span>` : ''}
        <div class="review-model">${esc(r.model || '')} · ${esc(r.problem_label || r.problem_type || '')}</div>
      </div>
      <button class="review-fav${fav}" data-fav="${esc(r.id)}">★</button>
    </div>
    <div class="review-quote">${esc(r.quote || '')}</div>
    ${(r.quote_translated && r.quote_translated !== r.quote) ? `<div class="review-quote-translated">${esc(r.quote_translated)}</div>` : ''}
    ${r.counterargument ? `<div class="review-counter">🛡 ${esc(r.counterargument)}</div>` : ''}
    ${url ? `<div class="review-src">🔗 <a href="${esc(url)}" target="_blank" rel="noopener">${esc(r.source || 'джерело')}</a></div>` : ''}
  `;
}

// === FAVORITES HANDLER ===
document.addEventListener('click', e => {
  const b = e.target.closest('[data-fav]');
  if (b) {
    const id = b.dataset.fav;
    if (favs[id]) delete favs[id];
    else favs[id] = 1;
    try {
      localStorage.setItem('agro_fav', JSON.stringify(favs));
    } catch (_) {}
    b.classList.toggle('active');
    renderFav();
  }
});

// === FILTER & SORT LOGIC ===
function apply() {
  const q = (document.getElementById('q')?.value || '').toLowerCase();
  const sort = document.getElementById('sort')?.value || 'relevance';

  F = R.filter(r => {
    if (fB && r.brand !== fB) return false;
    if (fP && (r.problem_label || r.problem_type) !== fP) return false;
    if (fS && r.source !== fS) return false;
    if (sort !== 'all' && r.sales_quality === 'weak_comment' && !q) return false;
    if (q && !((r.quote || '') + ' ' + (r.quote_translated || '') + ' ' + (r.counterargument || '')).toLowerCase().includes(q)) return false;
    return true;
  });

  window.R = R;
  window.F = F;

  const byDate = (a, b) => String(b.published_at || '').localeCompare(String(a.published_at || ''));

  if (sort === 'critical') {
    F.sort((a, b) => (b.is_critical - a.is_critical) || byDate(a, b));
  } else if (sort === 'date') {
    F.sort(byDate);
  } else if (sort === 'params') {
    F.sort((a, b) => (Object.keys(b.tech_params || {}).length - Object.keys(a.tech_params || {}).length) || byDate(a, b));
  } else {
    // relevance default: critical first, then grade A->B->C->D, then date
    F.sort((a, b) => ((b.is_critical - a.is_critical) || ((GR[a.evidence_grade] ?? 9) - (GR[b.evidence_grade] ?? 9)) || byDate(a, b)));
  }

  shown = 0;
  const listEl = document.getElementById('list');
  if (listEl) listEl.innerHTML = '';
  more();

  const cntEl = document.getElementById('cnt');
  if (cntEl) cntEl.innerHTML = `Знайдено: <strong>${F.length}</strong>`;
}

function more() {
  const box = document.getElementById('list');
  if (!box) return;
  F.slice(shown, shown + PAGE).forEach(r => {
    const div = document.createElement('div');
    div.className = 'review-card' + (r.is_critical ? ' critical' : '');
    div.innerHTML = cardHTML(r);
    box.appendChild(div);
  });
  shown += PAGE;
  const moreBtn = document.getElementById('more');
  if (moreBtn) moreBtn.style.display = (shown >= F.length) ? 'none' : 'block';
}

// === BATTLE SCREEN & TALK TRACKS ===
const TT = {
  boom: { t: 'Проблема: Штанга', say: b => `Клієнт розглядає ${b}? Оператори повідомляють про проблеми зі штангою — чіпляє землю, ламається.`, act: '🎯 BERTHOUD Raptor з AXIALE — активна стабілізація штанги.' },
  isobus: { t: 'Проблема: ISOBUS', say: b => `У ${b} скарги на ISOBUS — секції не відкриваються, потрібні сторонні монітори.`, act: '🎯 BERTHOUD Raptor з EC Tronic — повна інтеграція ISOBUS.' },
  weight: { t: 'Проблема: Вага', say: b => `${b} занадто важкий — ущільнення ґрунту, не працює на м\'яких полях.`, act: '🎯 BERTHOUD Raptor — легша конструкція, низький тиск на ґрунт.' },
  service: { t: 'Проблема: Обслуговування', say: b => `У ${b} складне ТО — потрібен дилер для кожної операції.`, act: '🎯 BERTHOUD Raptor — легкий доступ до вузлів, ТО без дилера.' },
  service_dealer: { t: 'Проблема: Сервіс / дилер', say: b => `У ${b} проблеми з сервісом і дилерами.`, act: '🎯 Покажіть сервісні переваги BERTHOUD.' },
  electronics: { t: 'Проблема: Електроніка', say: b => `У ${b} помилки датчиків, нерозшифровані коди.`, act: '🎯 BERTHOUD — детальна діагностика українською.' },
  price: { t: 'Проблема: Ціна', say: b => `${b} дорогий — запчастини і сервіс.`, act: '🎯 TCO BERTHOUD Raptor на 5 років — запчастини дешевше.' },
  engine: { t: 'Проблема: Двигун', say: b => `У ${b} питання витрати палива та надійності двигуна.`, act: '🎯 BERTHOUD — оптимізована витрата, надійний двигун.' },
  cab: { t: 'Проблема: Кабіна', say: b => `У ${b} шумна кабіна, погана видимість.`, act: '🎯 Кабіна BERTHOUD <70 дБ, панорамний огляд.' },
  nozzle: { t: 'Проблема: Форсунки', say: b => `У ${b} обмежена сумісність форсунок.`, act: '🎯 BERTHOUD — сумісність з усіма типами (Lechler, TeeJet, Albuz).' },
  rate_control: { t: 'Проблема: Норма внесення', say: b => `У ${b} нестабільна норма внесення.`, act: '🎯 BERTHOUD тримає норму через контроль потоку та секцій.' },
  pressure_stability: { t: 'Проблема: Тиск', say: b => `У ${b} падіння/коливання тиску.`, act: '🎯 Стабільність насосної групи BERTHOUD.' },
  pump_flow: { t: 'Проблема: Насос', say: b => `У ${b} скарги на насос і потік.`, act: '🎯 Сервісний доступ до насосної групи BERTHOUD.' },
  flow_meter: { t: 'Проблема: Витратомір', say: b => `У ${b} сигнали по витратоміру.`, act: '🎯 Контроль внесення і діагностика BERTHOUD.' },
  tank_filling: { t: 'Проблема: Заправка', say: b => `У ${b} питання заправки/промивки.`, act: '🎯 BERTHOUD скорочує втрати часу на заправці.' },
  boom_stability: { t: 'Проблема: Стійкість штанги', say: b => `У ${b} ризики стабільності штанги.`, act: '🎯 AXIALE — головний доказ стабільності BERTHOUD.' },
  hydraulics: { t: 'Проблема: Гідравліка', say: b => `У ${b} сигнали по гідравліці.`, act: '🎯 Надійність гідровузлів BERTHOUD.' },
  transmission: { t: 'Проблема: Трансмісія', say: b => `У ${b} питання трансмісії.`, act: '🎯 Порівняйте надійність трансмісії BERTHOUD.' }
};

function renderTalkTracks(brand, list) {
  const groups = {};
  list.forEach(r => {
    const pt = r.problem_type || 'default';
    if (pt === 'default') return;
    (groups[pt] = groups[pt] || []).push(r);
  });
  const top = Object.entries(groups).sort((a, b) => b[1].length - a[1].length).slice(0, 3);
  document.getElementById('ttCnt').textContent = '· ' + top.length;
  const box = document.getElementById('tlist');
  box.innerHTML = '';
  top.forEach(([pt, items]) => {
    const t = TT[pt] || {
      t: 'Проблема: ' + pt,
      say: b => `У ${b} сигнали: ${pt} (${items.length} відгуків).`,
      act: '🎯 Використайте як контраргумент на користь BERTHOUD.'
    };
    const div = document.createElement('div');
    div.className = 'problem-card';
    div.innerHTML = `
      <div class="problem-header">
        <div class="problem-name">${esc(t.t)}</div>
        <div class="problem-count">${items.length}</div>
      </div>
      <div class="problem-desc">${esc(t.say(brand))}</div>
      <div class="problem-desc" style="color:var(--green);margin-top:4px">${esc(t.act)}</div>
    `;
    box.appendChild(div);
  });
  if (!top.length) box.innerHTML = '<div class="empty">Немає даних</div>';
}

async function renderAICard(brand) {
  const box = document.getElementById('alist');
  try {
    if (!AICache) AICache = await (await fetch('ai_battlecards.json')).json();
    const c = (AICache.brands || {})[brand];
    document.getElementById('aiCnt').textContent = c ? '· ' + brand : '';
    if (!c) {
      box.innerHTML = '<div class="empty">Немає ИИ-картки для ' + esc(brand) + '</div>';
      return;
    }
    box.innerHTML = `
      <div class="review-card">
        <div class="review-quote">${esc(c.summary || '')}</div>
        ${(c.customer_may_say || []).map(s => `<div class="problem-desc">💬 ${esc(s)}</div>`).join('')}
        ${(c.competitor_strengths || []).map(s => `<div class="problem-desc" style="color:var(--orange)">⚠ ${esc(s)}</div>`).join('')}
      </div>
    `;
  } catch (e) {
    box.innerHTML = '<div class="empty">Не вдалося завантажити</div>';
  }
}

function renderBattle() {
  const list = R.filter(r => r.battle_ready);
  document.getElementById('bCnt').textContent = '· ' + list.length;
  const brands = [...new Set(list.map(r => r.brand).filter(Boolean))].sort();
  if (!bBrand || !brands.includes(bBrand)) bBrand = brands[0] || '';
  const box = document.getElementById('fBB');
  box.innerHTML = '';
  brands.forEach(b => {
    const d = document.createElement('div');
    d.className = 'chip' + (b === bBrand ? ' active' : '');
    d.textContent = b;
    d.onclick = () => { bBrand = b; renderBattle(); };
    box.appendChild(d);
  });
  renderTalkTracks(bBrand, list.filter(r => r.brand === bBrand));
  renderAICard(bBrand);
  const bl = document.getElementById('blist');
  bl.innerHTML = '';
  list.filter(r => !bBrand || r.brand === bBrand).slice(0, 100).forEach(r => {
    const div = document.createElement('div');
    div.className = 'review-card critical';
    div.innerHTML = cardHTML(r);
    bl.appendChild(div);
  });
  if (!list.length) bl.innerHTML = '<div class="empty">Поки порожньо</div>';
}

function renderFav() {
  const ids = Object.keys(favs);
  document.getElementById('fCnt').textContent = '· ' + ids.length;
  const box = document.getElementById('flist');
  box.innerHTML = '';
  const map = {};
  R.forEach(r => { map[r.id] = r; });
  ids.forEach(id => {
    if (!map[id]) return;
    const div = document.createElement('div');
    div.className = 'review-card';
    div.innerHTML = cardHTML(map[id]);
    box.appendChild(div);
  });
  if (!ids.length) box.innerHTML = '<div class="empty">Натисніть ★ на картці відгуку, щоб зберегти його сюди</div>';
}

// === INITIALIZATION ===
fetch('reviews.json')
  .then(r => {
    if (!r.ok) throw new Error(r.status);
    return r.json();
  })
  .then(d => {
    R = d.reviews || [];
    window.R = R;

    const bc = {};
    R.forEach(r => { bc[r.brand] = (bc[r.brand] || 0) + 1; });
    const brands = Object.entries(bc).sort((a, b) => b[1] - a[1]);

    document.getElementById('hTotal').textContent = R.length;
    document.getElementById('hSub').textContent = 'бойових: ' + R.filter(r => r.battle_ready).length;
    document.getElementById('sCrit').textContent = R.filter(r => r.is_critical).length;
    document.getElementById('sBattle').textContent = R.filter(r => r.battle_ready).length;
    document.getElementById('sBrands').textContent = brands.length;
    document.getElementById('sSrc').textContent = new Set(R.map(r => r.source)).size;
    document.getElementById('dbdate').textContent = (d.metadata && d.metadata.last_updated || '').slice(0, 10);

    const bt = document.getElementById('brandTiles');
    if (bt) {
      bt.innerHTML = '';
      brands.slice(0, 9).forEach(([b, n]) => {
        const crit = R.filter(r => r.brand === b && r.is_critical).length;
        const t = document.createElement('div');
        t.className = 'brand-tile' + (fB === b ? ' on' : '');
        t.innerHTML = `
          <div class="brand-tile-name">${esc(b)}</div>
          <div class="brand-tile-count">${n}</div>
          ${crit ? `<div class="brand-tile-crit">⚠ ${crit}</div>` : ''}
        `;
        t.onclick = () => {
          fB = (fB === b) ? '' : b;
          showScreen('reviews');
          drawChips();
          apply();
        };
        bt.appendChild(t);
      });
    }

    const pc = {};
    R.forEach(r => {
      const k = r.problem_label || r.problem_type || 'Інше';
      pc[k] = (pc[k] || 0) + 1;
    });
    const pl = document.getElementById('probList');
    if (pl) {
      pl.innerHTML = '';
      Object.entries(pc).sort((a, b) => b[1] - a[1]).slice(0, 8).forEach(([p, n]) => {
        const c = document.createElement('div');
        c.className = 'problem-card' + (fP === p ? ' on' : '');
        c.innerHTML = `
          <div class="problem-header">
            <div class="problem-name">${esc(p)}</div>
            <div class="problem-count">${n}</div>
          </div>
        `;
        c.onclick = () => {
          fP = (fP === p) ? '' : p;
          showScreen('reviews');
          drawChips();
          apply();
        };
        pl.appendChild(c);
      });
    }

    drawChips();
    apply();
    renderBattle();
  })
  .catch(e => {
    const cntEl = document.getElementById('cnt');
    if (cntEl) cntEl.textContent = '· помилка завантаження';
    toast('Не вдалося завантажити базу: ' + e);
  });

// === EVENT LISTENERS ===
document.getElementById('q')?.addEventListener('input', apply);
document.getElementById('sort')?.addEventListener('input', apply);
document.getElementById('more')?.addEventListener('click', more);

document.getElementById('expXlsx')?.addEventListener('click', exportToExcel);
document.getElementById('expCsv')?.addEventListener('click', exportToCsv);
document.getElementById('expJson')?.addEventListener('click', exportToJson);

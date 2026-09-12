/**
 * export.js — Модуль экспорта данных (XLSX, CSV, JSON) для Agro-Reader
 * Использует SheetJS (xlsx.full.min.js) и нативные плагины Capacitor.
 */

function toast(msg) {
  let t = document.getElementById('toast');
  if (!t) {
    t = document.createElement('div');
    t.id = 'toast';
    t.className = 'toast';
    document.body.appendChild(t);
  }
  t.textContent = msg;
  t.style.display = 'block';
  clearTimeout(t._h);
  t._h = setTimeout(() => { t.style.display = 'none'; }, 3500);
}

function guardExport() {
  if (!window.R || !window.R.length) {
    toast('Дані ще завантажуються, зачекайте секунду…');
    return false;
  }
  if (!window.F || !window.F.length) {
    toast('Список порожній — змініть фільтри або пошук');
    return false;
  }
  toast(`Вивантажую ${window.F.length} відгуків…`);
  return true;
}

async function saveAndShareFile(filename, data, mimeType, isBase64 = false) {
  // 1. Попытка нативного сохранения через Capacitor (Android APK)
  try {
    const FS = window.Capacitor?.Plugins?.Filesystem;
    const SH = window.Capacitor?.Plugins?.Share;
    if (FS && SH) {
      const writeOpts = {
        path: filename,
        data: data,
        directory: 'CACHE',
        recursive: true
      };
      if (!isBase64) {
        writeOpts.encoding = 'utf8';
      }
      const ret = await FS.writeFile(writeOpts);
      await SH.share({
        title: filename,
        url: ret.uri
      });
      return;
    }
  } catch (err) {
    console.warn('[export] Native save error, falling back to browser download:', err);
  }

  // 2. Запасной путь для веб-браузера
  try {
    let blob;
    if (isBase64) {
      const byteChars = atob(data);
      const byteNumbers = new Array(byteChars.length);
      for (let i = 0; i < byteChars.length; i++) {
        byteNumbers[i] = byteChars.charCodeAt(i);
      }
      blob = new Blob([new Uint8Array(byteNumbers)], { type: mimeType });
    } else {
      blob = new Blob([data], { type: mimeType });
    }
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    setTimeout(() => {
      URL.revokeObjectURL(a.href);
      a.remove();
    }, 2500);
    toast(`Файл ${filename} завантажується`);
  } catch (err) {
    toast(`Помилка збереження файлу: ${err}`);
  }
}

// === EXPORT XLSX (Настоящий бинарный Excel с вкладками) ===
function exportToExcel() {
  if (!guardExport()) return;
  const F = window.F;

  if (typeof XLSX === 'undefined') {
    toast('Помилка: бібліотека XLSX не завантажена');
    return;
  }

  const wb = XLSX.utils.book_new();

  // Вкладка 1: Всі відгуки (показывается первой при открытии)
  const allData = [
    ['№', 'Дата', 'Бренд', 'Модель', 'Проблема', 'Критичний', 'Грейд', 'Джерело', 'Автор', 'Цитата', 'Переклад (УКР)', 'Контраргумент BERTHOUD', 'Посилання на джерело']
  ];
  F.forEach((r, i) => {
    allData.push([
      i + 1,
      (r.published_at || r.collected_at || '').slice(0, 10),
      r.brand || '',
      r.model || '',
      r.problem_label || r.problem_type || '',
      r.is_critical ? 'Так' : '—',
      r.evidence_grade || 'D',
      r.source || '',
      r.comment_author || '',
      r.quote || '',
      r.quote_translated || '',
      r.counterargument || '',
      r.comment_url || r.source_url || ''
    ]);
  });
  const wsAll = XLSX.utils.aoa_to_sheet(allData);
  wsAll['!cols'] = [
    { wch: 5 }, { wch: 12 }, { wch: 15 }, { wch: 15 }, { wch: 20 },
    { wch: 10 }, { wch: 8 }, { wch: 15 }, { wch: 20 }, { wch: 50 },
    { wch: 50 }, { wch: 45 }, { wch: 35 }
  ];
  XLSX.utils.book_append_sheet(wb, wsAll, 'Всі відгуки');

  // Вкладка 2: Критичні
  const crit = F.filter(r => r.is_critical).sort((a, b) => String(b.published_at || '').localeCompare(String(a.published_at || '')));
  const critData = [
    ['№', 'Дата', 'Бренд', 'Модель', 'Проблема', 'Грейд', 'Автор', 'Цитата', 'Переклад (УКР)', 'Контраргумент BERTHOUD', 'Посилання на джерело']
  ];
  crit.forEach((r, i) => {
    critData.push([
      i + 1,
      (r.published_at || r.collected_at || '').slice(0, 10),
      r.brand || '',
      r.model || '',
      r.problem_label || r.problem_type || '',
      r.evidence_grade || 'D',
      r.comment_author || '',
      r.quote || '',
      r.quote_translated || '',
      r.counterargument || '',
      r.comment_url || r.source_url || ''
    ]);
  });
  const wsCrit = XLSX.utils.aoa_to_sheet(critData);
  wsCrit['!cols'] = [
    { wch: 5 }, { wch: 12 }, { wch: 15 }, { wch: 15 }, { wch: 20 },
    { wch: 8 }, { wch: 20 }, { wch: 50 }, { wch: 50 }, { wch: 45 }, { wch: 35 }
  ];
  XLSX.utils.book_append_sheet(wb, wsCrit, 'Критичні');

  // Вкладка 3: Сводка KPI
  const bc = {};
  F.forEach(r => { bc[r.brand || '—'] = (bc[r.brand || '—'] || 0) + 1; });
  const brands = Object.entries(bc).sort((a, b) => b[1] - a[1]);
  const pg = {};
  F.forEach(r => {
    const p = r.problem_label || r.problem_type || 'default';
    pg[p] = (pg[p] || 0) + 1;
  });
  const pgroups = Object.entries(pg).sort((a, b) => b[1] - a[1]);

  const sumData = [
    ['Показник', 'Значення'],
    ['Всього відгуків у вибірці', F.length],
    ['Критичних проблем', crit.length],
    ['Бойових (Battle-ready)', F.filter(r => r.battle_ready).length],
    ['Перекладено (УКР)', F.filter(r => r.quote_translated).length],
    ['Кількість брендів', brands.length],
    [],
    ['ТОП БРЕНДІВ', 'КІЛЬКІСТЬ ВІДГУКІВ'],
    ...brands.slice(0, 15).map(([b, c]) => [b, c]),
    [],
    ['ТОП ПРОБЛЕМ', 'КІЛЬКІСТЬ'],
    ...pgroups.slice(0, 15).map(([p, c]) => [p, c])
  ];
  const wsSum = XLSX.utils.aoa_to_sheet(sumData);
  wsSum['!cols'] = [{ wch: 30 }, { wch: 20 }];
  XLSX.utils.book_append_sheet(wb, wsSum, 'Сводка KPI');

  const b64 = XLSX.write(wb, { bookType: 'xlsx', type: 'base64' });
  saveAndShareFile(`reviews_radar_${F.length}.xlsx`, b64, 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', true);
}

// === EXPORT CSV ===
function exportToCsv() {
  if (!guardExport()) return;
  const F = window.F;
  const esc = (v) => {
    v = String(v ?? '');
    return /[",\n\r]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v;
  };
  const rows = [
    ['№', 'Дата', 'Бренд', 'Модель', 'Проблема', 'Критичний', 'Грейд', 'Джерело', 'Автор', 'Цитата', 'Переклад (УКР)', 'Контраргумент', 'Посилання']
  ];
  F.forEach((r, i) => {
    rows.push([
      i + 1,
      (r.published_at || r.collected_at || '').slice(0, 10),
      r.brand,
      r.model,
      r.problem_label || r.problem_type,
      r.is_critical ? 'Так' : '—',
      r.evidence_grade,
      r.source,
      r.comment_author,
      r.quote,
      r.quote_translated,
      r.counterargument,
      r.comment_url || r.source_url
    ].map(esc));
  });
  const csvContent = '\ufeff' + rows.map(r => r.join(';')).join('\r\n');
  saveAndShareFile(`reviews_${F.length}.csv`, csvContent, 'text/csv;charset=utf-8', false);
}

// === EXPORT JSON ===
function exportToJson() {
  if (!guardExport()) return;
  const F = window.F;
  const jsonContent = JSON.stringify(F.map(r => ({
    brand: r.brand,
    model: r.model,
    problem: r.problem_label || r.problem_type,
    grade: r.evidence_grade,
    is_critical: r.is_critical,
    quote: r.quote,
    quote_translated: r.quote_translated,
    counterargument: r.counterargument,
    url: r.comment_url || r.source_url
  })), null, 2);
  saveAndShareFile(`reviews_${F.length}.json`, jsonContent, 'application/json', false);
}

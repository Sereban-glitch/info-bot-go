// Pure presentation helper. Never infer CSV row boundaries or column headings.
// Render every returned string as text (never HTML); raw is the exact input.
function szResponsePreview(value) {
  const raw = typeof value === 'string' ? value : '';
  const damaged = raw.includes('\uFFFD');
  const clipped = /(?:…|\.{3}|\uFFFD)\s*$/.test(raw);
  const result = {kind: 'text', raw, rows: [], fields: [], delimiter: null,
    incomplete: clipped, partialRaw: '', notice: '', damaged};
  const damageNotice = damaged ? ' У збереженому фрагменті є пошкоджені символи. Перевірте оригінал у джерелі.' : '';
  const finish = notice => ({...result, notice: (notice + damageNotice).trim()});
  if (!raw.trim()) return finish('');

  // Actual line breaks outside quoted fields delimit records. Quoted line
  // breaks and escaped quotes remain part of the same field.
  function read(delimiter) {
    const records = [];
    let cells = [], tokens = [], field = '', state = 'start', malformed = false;
    let start = 0, tokenStart = 0;
    const cell = end => { cells.push(field); tokens.push(raw.slice(tokenStart, end)); field = ''; state = 'start'; };
    const row = (end, next, unclosed = false) => {
      cell(end);
      const source = raw.slice(start, end);
      if (source.trim()) records.push({cells, tokens, raw: source, malformed, unclosed});
      cells = []; tokens = []; malformed = false; start = next; tokenStart = next;
    };
    for (let i = 0; i < raw.length; i++) {
      const char = raw[i];
      if (state === 'quoted') {
        if (char === '"') {
          if (raw[i + 1] === '"') { field += '"'; i++; }
          else state = 'closed';
        } else field += char;
      } else if (char === delimiter) {
        cell(i); tokenStart = i + 1;
      } else if (char === '\n' || char === '\r') {
        const next = char === '\r' && raw[i + 1] === '\n' ? i + 2 : i + 1;
        row(i, next); i = next - 1;
      } else if (char === '"') {
        if (state !== 'start' || field) malformed = true;
        state = 'quoted';
      } else if (state === 'closed') {
        if (char !== ' ' && char !== '\t') { malformed = true; state = 'plain'; field += char; }
      } else {
        state = 'plain'; field += char;
      }
    }
    if (start < raw.length) row(raw.length, raw.length, state === 'quoted');
    return records;
  }

  const candidates = [';', ',', '\t'].map(delimiter => ({delimiter, records: read(delimiter)}));
  for (const {delimiter, records} of candidates) {
    if (records.length < 2) continue;
    const width = records[0].cells.length;
    if (width < 2) continue;
    const last = records.at(-1);
    const partial = clipped || last.unclosed || last.malformed || last.cells.length !== width;
    const complete = partial ? records.slice(0, -1) : records;
    // Two consistent complete records are the minimum evidence for a table.
    // A header-looking line followed by arbitrary prose is not enough.
    if (complete.length < 2 || complete.some(record => record.unclosed || record.malformed || record.cells.length !== width)) continue;
    result.kind = 'table'; result.delimiter = delimiter;
    result.rows = complete.map(record => record.cells);
    result.incomplete = partial; result.partialRaw = partial ? last.raw : '';
    return finish(partial
      ? 'Табличний фрагмент відповіді. Неповний або нерозпізнаний кінець наведено окремо; повний вміст перевірте у джерелі.'
      : 'Табличний фрагмент відповіді. Повний вміст перевірте у джерелі.');
  }

  // Flat semicolon data may contain several original rows. Expose only the
  // observed field sequence; even a plausible header is never reconstructed.
  const flat = candidates[0].records;
  if (flat.length === 1 && flat[0].cells.length >= 3) {
    const record = flat[0];
    result.kind = 'fields'; result.delimiter = ';'; result.incomplete = true;
    result.fields = record.malformed || record.unclosed ? record.tokens.map(token => token.trim()) : record.cells;
    return finish('Неповний структурований фрагмент. Межі рядків не збережено; поля наведено послідовно, без відновлення таблиці.');
  }
  const suspicious = candidates.some(({records}) => records.some(record => record.cells.length > 1 && (record.unclosed || record.malformed)));
  result.incomplete = result.incomplete || suspicious;
  return finish(result.incomplete ? 'Фрагмент може бути обірваним. Повний текст перевірте у джерелі.' : '');
}

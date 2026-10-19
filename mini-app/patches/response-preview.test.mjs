import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const context = vm.createContext({});
vm.runInContext(fs.readFileSync(new URL('response-preview.js', import.meta.url), 'utf8'), context);
const preview = value => JSON.parse(JSON.stringify(context.szResponsePreview(value)));

test('ordinary text, HTML and source placeholders stay literal and exact', () => {
  for (const raw of ['', 'Звичайний текст\n[ email address ]', '<img src=x onerror=alert(1)>', 'One, two, three']) {
    const result = preview(raw);
    assert.equal(result.kind, 'text'); assert.equal(result.raw, raw);
    assert.deepEqual(result.rows, []); assert.deepEqual(result.fields, []);
  }
  assert.equal(preview(null).raw, ''); assert.equal(preview({text: 'x'}).raw, '');
});

test('explicit semicolon rows become cells without inventing a header', () => {
  const raw = 'Орган;Адреса\r\nСуд;[ email address ]\r\nІнший суд;м. Київ\r\n';
  const result = preview(raw);
  assert.equal(result.kind, 'table'); assert.equal(result.delimiter, ';');
  assert.deepEqual(result.rows, [['Орган', 'Адреса'], ['Суд', '[ email address ]'], ['Інший суд', 'м. Київ']]);
  assert.equal(result.raw, raw); assert.equal(result.incomplete, false);
  assert.equal(Object.hasOwn(result, 'headers'), false);
});

test('quoted delimiters, escaped quotes, CRLF and quoted newlines are preserved', () => {
  const result = preview('Назва,Опис\r\n"Суд, Київ","Рядок 1\r\nРядок ""2"""\r\n');
  assert.equal(result.kind, 'table'); assert.equal(result.delimiter, ',');
  assert.deepEqual(result.rows, [['Назва', 'Опис'], ['Суд, Київ', 'Рядок 1\r\nРядок "2"']]);
});

test('tabular rows with tabs and trailing empty cells keep their field counts', () => {
  const result = preview('Назва\tАдреса\tПримітка\nСуд\t[ email address ]\t');
  assert.equal(result.kind, 'table');
  assert.deepEqual(result.rows, [['Назва', 'Адреса', 'Примітка'], ['Суд', '[ email address ]', '']]);
});

for (const tail of ['Другий;"Обірваний…', 'Другий;Обрізано…', 'Другий', 'Другий;Зайве;Поле', 'Другий;Гр�', 'Другий;"Некоректно"x']) {
  test(`partial last record is never padded or turned into a complete row: ${tail}`, () => {
    const raw = 'Назва;Адреса\nСуд;[ email address ]\n' + tail;
    const result = preview(raw);
    assert.equal(result.kind, 'table'); assert.equal(result.incomplete, true);
    assert.deepEqual(result.rows, [['Назва', 'Адреса'], ['Суд', '[ email address ]']]);
    assert.equal(result.partialRaw, tail); assert.equal(result.raw, raw);
  });
}

test('flattened semicolon excerpt is a field sequence, never guessed rows', () => {
  const raw = 'Назва;Адреса;Суд;[ email address ];Інший суд;Гр�…';
  const result = preview(raw);
  assert.equal(result.kind, 'fields'); assert.equal(result.incomplete, true); assert.equal(result.damaged, true);
  assert.deepEqual(result.rows, []);
  assert.deepEqual(result.fields, ['Назва', 'Адреса', 'Суд', '[ email address ]', 'Інший суд', 'Гр�…']);
  assert.match(result.notice, /Межі рядків не збережено/); assert.match(result.notice, /пошкоджені символи/);
  assert.equal(result.raw, raw);
});

test('flattened quoted row joins remain literal rather than reconstructed', () => {
  const raw = '"Назва";"Адреса" "Суд";"[ email address ]" "Другий";"Гр�…';
  const result = preview(raw);
  assert.equal(result.kind, 'fields'); assert.deepEqual(result.rows, []);
  assert.equal(result.fields[1], '"Адреса" "Суд"');
  assert.equal(result.fields[2], '"[ email address ]" "Другий"');
  assert.equal(result.raw, raw);
});

test('quoted newlines do not manufacture row boundaries', () => {
  const result = preview('"Київ\nСуд";"[ email address ]";"Опис"');
  assert.equal(result.kind, 'fields'); assert.deepEqual(result.rows, []);
  assert.equal(result.fields[0], 'Київ\nСуд');
});

test('inconsistent or ambiguous multiline data stays raw', () => {
  for (const raw of ['Hello, world\nThanks', 'A;B\nx;y;z\na;b', 'A;B\n"truncated…', 'A;B\nx;"bad"x\nc;d']) {
    const result = preview(raw);
    assert.equal(result.kind, 'text'); assert.deepEqual(result.rows, []); assert.equal(result.raw, raw);
  }
});

test('field values that look like HTML are data, never interpreted markup', () => {
  const result = preview('A;B\n<img src=x onerror=alert(1)>;[ email address ]');
  assert.equal(result.kind, 'table');
  assert.equal(result.rows[1][0], '<img src=x onerror=alert(1)>');
});


test('ordinary prose with quotes does not imply a broken CSV record', () => {
  const result = preview('Суд надав відповідь «так». Він пише: "Готово".');
  assert.equal(result.kind, 'text'); assert.equal(result.incomplete, false);
  assert.equal(result.notice, '');
});

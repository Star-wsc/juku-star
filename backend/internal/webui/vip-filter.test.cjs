const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');

function environment(saved) {
  const window = new EventTarget(), buttons = [new EventTarget(), new EventTarget()];
  for (const button of buttons) {button.attributes = {}; button.setAttribute = (key, value) => {button.attributes[key] = value;};}
  const values = new Map(saved ? [['juku.vip.show', saved]] : []);
  const localStorage = {getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value)};
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, 'vip-filter.js'), 'utf8'), {window, document: {querySelectorAll: () => buttons}, localStorage, Event, CustomEvent});
  return {window, buttons, values, vip: window.JukuVIP};
}

test('VIP filtering immediately toggles known entries, preserves unknowns, and explains missing classification', () => {
  const {window, vip, buttons, values} = environment();
  const paid = {id: 'huangdou:paid', vip: true}, free = {id: 'huangdou:free', vip: false}, unknown = {id: 'huangdou:unknown'};
  vip.init(true);
  assert.equal(vip.visible(paid), false);
  assert.equal(buttons[0].textContent, '隐藏 VIP');
  assert.equal(vip.visible(free), true);
  assert.equal(vip.visible(unknown), true);
  assert.match(vip.summary([paid, free, unknown]), /已隐藏 1 部 VIP.*1 部 VIP 状态待识别/);
  let requested = 0;
  window.addEventListener('jukuvipfilterchange', event => {if (event.detail.interactive) requested++;});
  buttons[0].dispatchEvent(new Event('click'));
  assert.equal(vip.visible(paid), true);
  assert.equal(buttons[1].textContent, '显示 VIP');
  assert.equal(values.get('juku.vip.show'), 'true');
  buttons[1].dispatchEvent(new Event('click'));
  assert.equal(vip.visible(paid), false);
  assert.equal(buttons[0].textContent, '隐藏 VIP');
  assert.equal(requested, 2);
});

test('a stored preference restores visibility while storage synchronization does not request a new batch', () => {
  const {window, vip, buttons} = environment('true');
  vip.init(true);
  assert.equal(vip.shown, true);
  let interactive;
  window.addEventListener('jukuvipfilterchange', event => {interactive = event.detail.interactive;});
  const event = new Event('storage');
  event.key = 'juku.vip.show'; event.newValue = 'false';
  window.dispatchEvent(event);
  assert.equal(vip.shown, false);
  assert.equal(interactive, false);
  assert.equal(buttons[0].attributes['aria-pressed'], 'false');
});

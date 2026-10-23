const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const source = fs.readFileSync(path.join(__dirname, 'drama-refresh.js'), 'utf8');
const modulePromise = import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));
const id = 'hongguo:7000000000000000001';
const row = (title, checkedAt) => ({id, title, sortMetadata: {checkedAt}});

test('title and cover refresh share one request and respect the server cooldown', async () => {
  const {createDramaRefresh} = await modulePromise;
  let timestamp = 1000, calls = 0, finish;
  const applied = [];
  const refresh = createDramaRefresh({now: () => timestamp, apply: drama => applied.push(drama), post: async (url, body) => {
    calls++;
    assert.equal(url, '/api/ui/dramas/refresh');
    assert.equal(body.dramaId, id);
    return new Promise(resolve => {finish = resolve;});
  }});
  const first = refresh.refresh(id), second = refresh.refresh(id);
  assert.equal(first, second);
  await Promise.resolve();
  assert.equal(calls, 1);
  finish({dramaId: id, drama: row('已更新', '2026-09-16T02:00:00Z'), retryAfter: 20});
  await first;
  await refresh.refresh(id);
  assert.equal(calls, 1);
  assert.equal(applied.length, 1);
  timestamp += 20001;
  const retry = refresh.refresh(id);
  await Promise.resolve();
  assert.equal(calls, 2);
  finish({dramaId: id, drama: row('再次更新', '2026-09-16T03:00:00Z'), retryAfter: 300});
  await retry;
});

test('a stale list response cannot erase clicked-drama metadata; a newer update wins', async () => {
  const {createDramaRefresh} = await modulePromise;
  const updated = row('已补齐的标题', '2026-09-16T03:00:00Z');
  const refresh = createDramaRefresh({now: () => 1000, apply() {}, post: async () => ({dramaId: id, drama: updated})});
  await refresh.refresh(id);
  assert.equal(refresh.reconcile(row('旧标题', '2026-09-16T02:00:00Z')).title, updated.title);
  const newer = row('服务器的新标题', '2026-09-16T04:00:00Z');
  assert.equal(refresh.reconcile(newer).title, newer.title);
});

test('failed or mismatched metadata never mutates the drama and can be retried', async () => {
  const {createDramaRefresh} = await modulePromise;
  for (const broken of [() => {throw new Error('offline');}, async () => ({dramaId: 'other', drama: {id: 'other'}})]) {
    let timestamp = 0, failures = 0, calls = 0;
    const refresh = createDramaRefresh({now: () => timestamp, post: (...args) => {calls++; return broken(...args);}, apply() {assert.fail('invalid data applied');}, failure() {failures++;}});
    await refresh.refresh(id);
    assert.equal(failures, 1);
    await refresh.refresh(id);
    assert.equal(calls, 1);
    timestamp = 30001;
    await refresh.refresh(id);
    assert.equal(calls, 2);
    assert.equal(failures, 2);
  }
});

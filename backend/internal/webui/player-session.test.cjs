const assert = require('node:assert/strict');
const test = require('node:test');
const vm = require('node:vm');
const fs = require('node:fs');

function fixture(request) {
  let now = 0, number = 0;
  const timers = new Map(), states = [], expired = [], errors = [];
  const context = {module: {exports: {}}, AbortController, Promise, Error, setTimeout: (fn, delay) => {const id = ++number; timers.set(id, {fn, at: now + delay}); return id;}, clearTimeout: id => timers.delete(id)};
  vm.runInNewContext(fs.readFileSync(require.resolve('./player-session.js'), 'utf8'), context);
  const session = context.module.exports({request, onState: state => states.push(state), onExpired: error => expired.push(error), onError: error => errors.push(error)});
  const flush = async () => {for (let index = 0; index < 12; index++) await Promise.resolve();};
  const tick = async amount => {
    const end = now + amount;
    await flush();
    while (true) {
      const next = Array.from(timers).filter(([, value]) => value.at <= end).sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      now = next[1].at; timers.delete(next[0]); next[1].fn(); await flush();
    }
    now = end; await flush();
  };
  return {session, states, expired, errors, tick, flush};
}

test('heartbeats renew every 20 seconds and concurrent callers share one request', async () => {
  const requests = [];
  let resolve;
  const f = fixture((id, signal) => {requests.push({id, signal}); return new Promise(done => {resolve = done;});});
  f.session.start('watch-one');
  await f.tick(20000);
  assert.equal(requests.length, 1);
  const first = f.session.ping(), second = f.session.ping();
  assert.equal(first, second);
  resolve({run: 1}); await f.flush();
  assert.equal(f.states.length, 1);
  await f.tick(19999); assert.equal(requests.length, 1);
  await f.tick(1); assert.equal(requests.length, 2);
  f.session.stop();
});

test('a hung heartbeat times out and retries without blocking future keepalives', async () => {
  const requests = [];
  const f = fixture((id, signal) => {requests.push({id, signal}); return requests.length === 1 ? new Promise(() => {}) : Promise.resolve({run: 7});});
  f.session.start('watch'); f.session.ping();
  await f.tick(8000);
  assert.equal(requests[0].signal.aborted, true);
  await f.tick(5000);
  assert.equal(requests.length, 2);
  assert.equal(f.states[0].run, 7);
  assert.equal(f.expired.length, 0);
  f.session.stop();
});

test('foreground checks cancel a stuck request and ignore its late expiry response', async () => {
  let failOld;
  const requests = [];
  const f = fixture((id, signal) => {requests.push(signal); return requests.length === 1 ? new Promise((_, reject) => {failOld = reject;}) : Promise.resolve({run: 3});});
  f.session.start('watch'); f.session.ping(); await f.flush();
  await f.session.ping(true);
  assert.equal(requests[0].aborted, true);
  failOld(Object.assign(new Error('expired'), {status: 410})); await f.flush();
  assert.equal(f.expired.length, 0);
  assert.equal(f.states[0].run, 3);
  f.session.stop();
});

test('expired sessions request one recovery; authentication failures do not trigger recovery loops', async () => {
  for (const status of [410, 401, 403]) {
    let requests = 0;
    const f = fixture(() => {requests++; return Promise.reject(Object.assign(new Error('unavailable'), {status}));});
    f.session.start('watch'); await f.session.ping(); await f.tick(120000);
    assert.equal(requests, 1);
    assert.equal(f.expired.length, status === 410 ? 1 : 0);
    assert.equal(f.errors.length, status === 410 ? 0 : 1);
    f.session.stop();
  }
});

test('closing or changing a drama invalidates late heartbeats from its old session', async () => {
  let finish;
  const f = fixture(() => new Promise(resolve => {finish = resolve;}));
  f.session.start('old'); f.session.ping(); await f.flush();
  f.session.start('new'); finish({run: 1}); await f.flush();
  assert.equal(f.states.length, 0);
  f.session.stop(); await f.tick(120000);
  assert.equal(f.states.length, 0);
});

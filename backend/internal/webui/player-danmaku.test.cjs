const test = require('node:test');
const assert = require('node:assert/strict');
const {plan, frames, lifetime, createClock} = require('./player-danmaku.js');

test('source timestamps, including zero, survive filtering and crowded lanes', () => {
  const items = Array.from({length: 20}, (_, i) => ({id: String(i), text: '文字 <b>原样</b>', timeMs: i * 50}));
  const schedule = plan([null, {}, ...items, items[0], {id: 'bad', text: 'bad', timeMs: -1}], 700, 3, 20);
  assert.equal(schedule.length, 3);
  assert.equal(schedule[0].start, 0);
  for (const item of schedule) assert.equal(item.start * 1000, items.find(x => x.id === item.id).timeMs);
  assert.equal(schedule[0].text, '文字 <b>原样</b>');
  assert.equal(frames(schedule, -1, 700).length, 0);
  assert.equal(frames([schedule[0]], lifetime, 700).length, 0);
  assert.deepEqual(plan(items, 0, 3, 20), []);
});

test('mixed text lengths cannot collide at any point in a lane', () => {
  const items = Array.from({length: 160}, (_, i) => ({id: String(i), text: (i % 2 ? 'W' : '字').repeat(1 + i % 60), timeMs: i * 150}));
  for (const width of [280, 760, 1600]) {
    const schedule = plan(items, width, 4, 20, text => text.length * 22);
    assert.ok(schedule.length > 4);
    for (let time = 0; time < 33; time += 1 / 60) {
      const frame = frames(schedule, time, width);
      for (let lane = 0; lane < 4; lane++) {
        const row = frame.filter(item => item.lane === lane).sort((a, b) => a.x - b.x);
        for (let i = 1; i < row.length; i++) assert.ok(row[i].x - row[i - 1].x - row[i - 1].size >= 23.9, 'overlapping text');
      }
    }
  }
});

test('a coarse 4 Hz video clock still produces continuous 60 Hz movement', () => {
  const clock = createClock();
  clock.sync(2, 0, 1, true, true);
  let last = 2;
  for (let frame = 1; frame <= 120; frame++) {
    const wall = frame * 1000 / 60;
    clock.sync(2 + Math.floor(wall / 250) / 4, wall, 1, true);
    const position = clock.position(wall);
    assert.ok(Math.abs(position - last - 1 / 60) < 1e-8, 'clock must not step with media samples');
    last = position;
  }
});

test('pause, buffering, rate changes and seek use the media timeline', () => {
  const clock = createClock();
  clock.sync(10, 0, 1, true, true);
  assert.equal(clock.position(500), 10.5);
  clock.sync(10.5, 500, 1, false, true);
  assert.equal(clock.position(8000), 10.5);
  clock.sync(10.5, 8000, 2, true, true);
  assert.equal(clock.position(8500), 11.5);
  clock.sync(60, 8500, 2, true, true);
  assert.equal(clock.position(9000), 61);
  clock.sync(5, 9000, 1, false, true);
  assert.equal(clock.position(10000), 5);
  clock.sync(5, 10000, 1, true, true);
  clock.sync(8, 11000, 1, true);
  assert.equal(clock.position(11000), 8, 'recover a real discontinuity');
});

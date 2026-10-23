const test = require('node:test');
const assert = require('node:assert/strict');
const {sortDramas, metricValue, dateValue, summary} = require('./library-sort.js');

test('numeric popularity, Chinese units, missing values and zero retain their meaning', () => {
  assert.equal(metricValue('1.2亿热度'), 120000000);
  assert.equal(metricValue('2.5w次播放'), 25000);
  assert.equal(metricValue('1,234'), 1234);
  assert.equal(metricValue('3.5万+'), 35000);
  assert.equal(metricValue('0'), 0);
  for (const value of ['', null, undefined, '未知', '-1', '9.5分', '约一万']) assert.equal(metricValue(value), null);
});

test('dates sort both ways while missing or impossible dates always stay last', () => {
  const dramas = [
    {id: 'unknown', title: 'A'}, {id: 'new', title: '新', onlineDate: '2026-09-13'},
    {id: 'old', title: '旧', onlineDate: '2025-11-01'}, {id: 'bad', title: 'B', onlineDate: '2026-02-31'}
  ];
  const original = dramas.slice();
  assert.deepEqual(sortDramas(dramas, 'newest').map(x => x.id), ['new', 'old', 'unknown', 'bad']);
  assert.deepEqual(sortDramas(dramas, 'oldest').map(x => x.id), ['old', 'new', 'unknown', 'bad']);
  assert.deepEqual(dramas, original);
  assert.equal(dateValue('2026-02-29'), null);
  assert.notEqual(dateValue('2024-02-29'), null);
  assert.equal(summary(dramas, 'newest'), '上线日期未知 2 部，已置后');
});

test('heat and play counts are independent sorts and default preserves source order', () => {
  const dramas = [
    {id: 'unknown', title: 'A'}, {id: 'heat', title: 'B', heat: '2亿热度', views: '0'},
    {id: 'views', title: 'C', heat: '8万热度', views: '10万次播放'}, {id: 'zero', title: 'D', heat: '0'}
  ];
  assert.deepEqual(sortDramas(dramas, 'heat').map(x => x.id), ['heat', 'views', 'zero', 'unknown']);
  assert.deepEqual(sortDramas(dramas, 'views').map(x => x.id), ['views', 'heat', 'unknown', 'zero']);
  assert.deepEqual(sortDramas(dramas, 'default'), dramas);
  assert.equal(summary([{views: '10万次播放'}], 'heat'), '当前结果缺少热度，无法按此排序');
});

test('equal values use a predictable title order and retain equal-title input order', () => {
  const dramas = [
    {id: 'ten', title: '剧10', heat: '100'}, {id: 'two', title: '剧2', heat: '100'},
    {id: 'also-two', title: '剧2', heat: '100'}
  ];
  assert.deepEqual(sortDramas(dramas, 'heat').map(x => x.id), ['two', 'also-two', 'ten']);
});

test('missing search metrics preserve input order and explain why time sorting cannot apply', () => {
  const results = [{id: 'first', title: 'Z'}, {id: 'second', title: 'A'}];
  for (const mode of ['newest', 'oldest', 'heat', 'views']) {
    assert.deepEqual(sortDramas(results, mode), results);
  }
  assert.equal(summary(results, 'newest'), '当前结果缺少上线日期，无法按此排序');
  assert.deepEqual(sortDramas(results, 'title').map(x => x.id), ['second', 'first']);
  const withHeat = [...results, {id: 'known', title: 'B', heat: '20019'}];
  assert.deepEqual(sortDramas(withHeat, 'heat').map(x => x.id), ['known', 'first', 'second']);
});

test('search heat keeps precision instead of sorting by rounded display values', () => {
  const results = [
    {id: 'lower', title: 'A', heat: '20001'},
    {id: 'higher', title: 'Z', heat: '20019'},
    {id: 'zero', title: '零', heat: '0'},
    {id: 'missing', title: '未知'}
  ];
  assert.deepEqual(sortDramas(results, 'heat').map(x => x.id), ['higher', 'lower', 'zero', 'missing']);
});

test('聚宝仙盆 seasons sort numerically without changing titles or mixing separate series', () => {
  const base='聚宝仙盆之杂灵根才是真BOSS';
  const seasons=['第八季','第二季','第九季','第七季','第三季','第十季','第十一季','第四季','第五季','第六季',''];
  const rows=seasons.map((suffix,index)=>({id:String(index),title:base+suffix,heat:'100'}));
  const expected=['','第二季','第三季','第四季','第五季','第六季','第七季','第八季','第九季','第十季','第十一季'].map(suffix=>base+suffix);
  for (const mode of ['title','heat']) assert.deepEqual(sortDramas(rows,mode).map(row=>row.title),expected);
  assert.deepEqual(rows.map(row=>row.title),seasons.map(suffix=>base+suffix));
  const separate=[...rows,{title:'聚宝仙盆仙界篇第七季'},{title:'聚宝仙盆'}];
  assert.deepEqual(sortDramas(separate,'title').map(row=>row.title),['聚宝仙盆','聚宝仙盆仙界篇第七季',...expected]);
});

test('Chinese ordinals, Arabic numerals and full-width digits share natural order', () => {
  const {titleKey}=require('./library-sort.js');
  const titles=['剧第一百零二季','剧第２季','剧第十一季','剧第十季','剧第一百季','剧第二十一季','剧第3季'];
  assert.deepEqual(sortDramas(titles.map(title=>({title})),'title').map(row=>row.title),['剧第２季','剧第3季','剧第十季','剧第十一季','剧第二十一季','剧第一百季','剧第一百零二季']);
  for(const title of ['三生三世','十一的故事','聚宝仙盆']) assert.equal(titleKey({title}),title);
});

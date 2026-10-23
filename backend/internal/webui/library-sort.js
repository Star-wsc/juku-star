(function (root) {
  'use strict';
  const modes = ['default', 'newest', 'oldest', 'heat', 'views', 'title'];
  const collator = new Intl.Collator('zh-Hans-CN', {numeric: true, sensitivity: 'base'});

  function chineseNumber(text) {
    const digits = {零:0,〇:0,一:1,二:2,两:2,兩:2,三:3,四:4,五:5,六:6,七:7,八:8,九:9};
    const units = {十:10,百:100,千:1000,万:10000,萬:10000};
    if ([...text].every(character => character in digits)) return Number([...text].map(character => digits[character]).join(''));
    let total=0, section=0, digit=0;
    for (const character of text) {
      if (character in digits) digit=digits[character];
      else if (units[character]===10000) {total+=(section+digit||1)*10000;section=0;digit=0;}
      else {section+=(digit||1)*units[character];digit=0;}
    }
    return total+section+digit;
  }

  function titleKey(drama) {


    return String(drama.title || drama.name || '').normalize('NFKC').trim()
      .replace(/第\s*([零〇一二两兩三四五六七八九十百千万萬]+)\s*(?=[季部集章卷篇册冊辑輯])/g, (_, number) => '第'+chineseNumber(number));
  }

  function metricValue(value) {
    if (typeof value === 'number') return Number.isFinite(value) && value >= 0 ? value : null;
    const text = String(value ?? '').replace(/[,，\s]/g, '');
    const match = /^(\d+(?:\.\d+)?)(亿|万|千|w|k|m|b)?\+?(?:次播放|次观看|人看过|热度|播放|观看|次)?\+?$/i.exec(text);
    if (!match) return null;
    const unit = (match[2] || '').toLowerCase();
    const number = Number(match[1]) * ({亿: 1e8, 万: 1e4, 千: 1e3, w: 1e4, k: 1e3, m: 1e6, b: 1e9}[unit] || 1);
    return Number.isFinite(number) ? number : null;
  }

  function dateValue(value) {
    const match = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(String(value || '').trim());
    if (!match) return null;
    const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]);
    const date = new Date(Date.UTC(year, month - 1, day));
    if (year < 2000 || year > 2100 || date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null;
    return date.getTime();
  }

  function sortValue(drama, mode) {
    if (mode === 'newest' || mode === 'oldest') return dateValue(drama.onlineDate);
    if (mode === 'heat') return metricValue(drama.heat);
    if (mode === 'views') return metricValue(drama.views);
    return null;
  }

  function sortDramas(list, mode) {
    if (!modes.includes(mode) || mode === 'default') return list.slice();

    return list.map((drama, index) => ({drama, index, title: titleKey(drama), value: sortValue(drama, mode)})).sort((left, right) => {
      if (mode !== 'title') {
        if (left.value === null && right.value === null) return left.index - right.index;
        if (left.value === null && right.value !== null) return 1;
        if (left.value !== null && right.value === null) return -1;
        if (left.value !== right.value) return mode === 'oldest' ? left.value - right.value : right.value - left.value;
      }
      return collator.compare(left.title, right.title) || left.index - right.index;
    }).map(item => item.drama);
  }

  function summary(list, mode) {
    if (!['newest', 'oldest', 'heat', 'views'].includes(mode)) return '';
    const label = {newest: '上线日期', oldest: '上线日期', heat: '热度', views: '播放量'}[mode];
    const known = list.filter(drama => sortValue(drama, mode) !== null).length;
    if (!list.length) return '';
    if (!known) return '当前结果缺少' + label + '，无法按此排序';
    return known < list.length ? label + '未知 ' + (list.length - known) + ' 部，已置后' : '';
  }

  const librarySort = {modes, metricValue, dateValue, sortDramas, summary, titleKey};
  if (typeof module !== 'undefined' && module.exports) module.exports = librarySort;
  if (root) root.JukuLibrarySort = librarySort;
})(typeof window !== 'undefined' ? window : null);

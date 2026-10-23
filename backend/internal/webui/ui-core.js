import { viewerHeaders, checkViewerResponse } from './viewer.js';

export const $ = id => document.getElementById(id);
let messageTimer;

function element(tag, className, text){ const node=document.createElement(tag); if(className)node.className=className; if(text!==undefined)node.textContent=String(text); return node; }

function empty(node){ while(node.firstChild)node.removeChild(node.firstChild); }

function button(text, action, disabled, className){ const node=element('button',className||'secondary',text); node.type='button';node.disabled=Boolean(disabled); node.addEventListener('click',action); return node; }

function setMessage(text, isError = false, action = null) {
  clearTimeout(messageTimer);
  const target = $('messageText');
  target.replaceChildren();
  target.className = isError ? 'error toast' : 'toast';
  if (!text) return;
  target.appendChild(element('span', '', text));
  if (action) target.appendChild(button(action.label || '撤销', async () => {
    try {await action.run();} catch (error) {setMessage(error.message, true);}
  }));
  messageTimer = setTimeout(() => target.replaceChildren(), action ? 10000 : 6500);
}

async function api(path, options){ const init=options||{}; init.headers=Object.assign({'Accept':'application/json'},viewerHeaders(),init.headers||{}); if(init.body)init.headers['Content-Type']='application/json'; const response=await fetch(path,init); let data; try{data=await response.json();}catch(_){throw new Error('服务器未返回有效 JSON，请确认已启动新版程序');} checkViewerResponse(data); if(!response.ok){const error=new Error(data.error||'HTTP '+response.status);error.status=response.status;throw error;} return data; }

function post(path, body){ return api(path,{method:'POST',body:JSON.stringify(body)}); }

function valueText(value){ if(value===undefined||value===null)return ''; if(Array.isArray(value))return value.length?String(value.length):''; if(typeof value==='object'){ for(const key of ['url','src','path','cover','image','pic','name','title','count','total']){const text=valueText(value[key]);if(text)return text;}return '';} const text=String(value).trim(); return text==='0'?'':text; }

function firstNonEmpty(...values){ for(const value of values){const text=valueText(value);if(text)return text;}return ''; }

function dramaTitle(drama){ return drama.title||drama.name||'短剧'; }

function watchLabel(id,fallback){return window.JukuHistory.get(id)?'继续观看':fallback;}

function historySuffix(id){const text=window.JukuHistory.progressText(id);return text?' · '+text:'';}

function normalizeSource(value){
    let source=String(value||'').trim().toLowerCase();
    if(/^https?:\/\//.test(source)){try{source=new URL(source).hostname;}catch(_){return '';}}
    source=source.replace(/^www\./,'').split(':')[0];
    const aliases={huangguo:'huangguo',huangguoai:'huangguo','huangguo-video':'huangguo','huangguoai.com':'huangguo','huangguo.video':'huangguo',cloudfront:'huangguo',api:'huangguo',huangdou:'huangdou','tideember.cc':'huangdou','xqjurgek.top':'huangdou',hongguo:'hongguo','hongguoduanju.com':'hongguo'};
    return Object.prototype.hasOwnProperty.call(aliases,source)?aliases[source]:'';
  }

function sourceKey(dr){ for(const hint of [dr.source,dr.id,dr.channelName,dr.channel_name,dr.site,dr.host]){const source=normalizeSource(hint);if(source)return source;} if(!dr.source&&['黄果原创','成人短剧','成人漫剧','AI魔改'].includes(dr.channelName))return 'huangguo'; return String(dr.source||'other').trim().toLowerCase()||'other'; }

function sourceLabel(value){ const names={huangguo:'黄果',huangdou:'黄豆',hongguo:'红果',other:'其他'};return names[normalizeSource(value)||value]||value; }

function categoryName(dr){ const category=firstNonEmpty(dr.categoryName,dr.category_name,dr.typeName,dr.type_name,dr.sortName,dr.sort_name,dr.category,dr.categoryNameSnake);if(category)return category;return ['黄果原创','成人短剧','成人漫剧','AI魔改'].includes(dr.channelName)?dr.channelName:'未分类'; }

function episodeCount(dr){ return firstNonEmpty(dr.totalEpisode,dr.total_episode,dr.chapterCount,dr.chapter_count,dr.episodeCount,dr.episode_count,dr.episodes,dr.total); }

function coverURL(dr){ return firstNonEmpty(dr.cover,dr.coverUrl,dr.cover_url,dr.image,dr.imageUrl,dr.image_url,dr.img,dr.pic,dr.picture,dr.poster,dr.thumb,dr.thumbnail); }

function tagsText(dr){ return Array.isArray(dr.tags)?dr.tags.map(valueText).filter(Boolean):[]; }

function dramaSearchText(dr){ return [dr.title,dr.name,dr.id,dr.desc,dr.intro,dr.remark,sourceLabel(sourceKey(dr)),categoryName(dr),tagsText(dr).join(' ')].join(' ').toLowerCase(); }

function rebuildOptions(select,values,allLabel,labelFor,reset){ const previous=reset?'':select.value;empty(select);const all=element('option','',allLabel);all.value='';select.appendChild(all);values.forEach(value=>{const option=element('option','',labelFor(value));option.value=value;select.appendChild(option);});select.value=values.includes(previous)?previous:''; }

function number(value){const result=Number(value);return Number.isFinite(result)?result:0;}

function formatBytes(value){let count=number(value);if(count<=0)return '0 B';const units=['B','KB','MB','GB','TB'];let index=0;while(count>=1024&&index<units.length-1){count/=1024;index++;}return (index?count.toFixed(count>=100?0:1):Math.round(count))+' '+units[index];}

function formatTime(value){const seconds=Math.max(0,Math.floor(number(value)));if(!seconds)return '—';const minutes=Math.floor(seconds/60);return (minutes?minutes+'分':'')+(seconds%60)+'秒';}

function statusText(status){return {queued:'排队中',parsing:'解析中',running:'下载中',success:'成功',failed:'失败',paused:'已暂停',canceled:'已取消'}[status]||status;}

function releaseText(status){return {finished:'完结',ongoing:'未完结',unknown:'状态未知'}[status]||'状态未知';}

function phaseText(task){if(task.removeRequested)return '正在停止并清理';if(task.pauseRequested)return '暂停中';if(task.cancelRequested)return '取消中';return {preparing:'准备 FFmpeg',starting:'准备下载',resolving:'解析播放地址',retrying:'等待重试'}[task.phase]||statusText(task.status);}

function progressText(task){ if(task.status==='success')return '100%';if(task.status==='running'&&!number(task.mediaTotalSeconds)&&!number(task.totalBytes))return phaseText(task)+' · 进度未知';return Math.max(0,Math.min(100,number(task.progress)))+'%'; }

function progressBar(percent){const bar=element('div','progress');const fill=element('span');fill.style.width=Math.max(0,Math.min(100,number(percent)))+'%';bar.appendChild(fill);return bar;}

function episodeLabel(task){const sequence=number(task.episode||task.index);return sequence>0?'第'+String(sequence).padStart(3,'0')+'集':task.title||'章节';}

function groupStats(group){const stats={success:0,failed:0,running:0,parsing:0,queued:0,paused:0,canceled:0,percent:0,bytes:0,speed:0};group.tasks.forEach(task=>{if(task.status in stats)stats[task.status]++;stats.percent+=task.status==='success'?100:number(task.progress);stats.bytes+=number(task.downloadedBytes);if(task.status==='running')stats.speed+=number(task.speedBytesPerSecond);});stats.percent=group.tasks.length?Math.floor(stats.percent/group.tasks.length):0;return stats;}

export { element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats };

const iconShapes = {
  user: [['circle', {cx: 12, cy: 8, r: 4}], ['path', {d: 'M4 21v-2a6 6 0 0 1 6-6h4a6 6 0 0 1 6 6v2'}]],
  brand: [['rect', {x: 3.5, y: 3.5, width: 17, height: 17, rx: 3}], ['path', {d: 'm10 8 6 4-6 4Z', fill: 'currentColor', stroke: 'none'}]],
  library: [['rect', {x: 3, y: 3, width: 7, height: 7, rx: 1.5}], ['rect', {x: 14, y: 3, width: 7, height: 7, rx: 1.5}], ['rect', {x: 3, y: 14, width: 7, height: 7, rx: 1.5}], ['rect', {x: 14, y: 14, width: 7, height: 7, rx: 1.5}]],
  bookmark: [['path', {d: 'M6 21V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v16l-6-4-6 4Z'}]],
  download: [['path', {d: 'M12 3v12m-5-5 5 5 5-5M4 17v3a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-3'}]],
  search: [['circle', {cx: 10.5, cy: 10.5, r: 6.5}], ['path', {d: 'm16 16 4.5 4.5'}]],
  refresh: [['path', {d: 'M20 4v5h-5M4 20v-5h5M5.6 7A8 8 0 0 1 19 6l1 3M4 15l1 3a8 8 0 0 0 13.4-1'}]],
  settings: [['path', {d: 'M4 7h8m4 0h4M4 17h4m4 0h8'}], ['circle', {cx: 14, cy: 7, r: 2}], ['circle', {cx: 10, cy: 17, r: 2}]],
  more: [['circle', {cx: 5, cy: 12, r: 1, fill: 'currentColor', stroke: 'none'}], ['circle', {cx: 12, cy: 12, r: 1, fill: 'currentColor', stroke: 'none'}], ['circle', {cx: 19, cy: 12, r: 1, fill: 'currentColor', stroke: 'none'}]],
  close: [['path', {d: 'm6 6 12 12M6 18 18 6'}]],
  chevron: [['path', {d: 'm7 10 5 5 5-5'}]],
  right: [['path', {d: 'm9 6 6 6-6 6'}]],
  play: [['path', {d: 'M8 5.5v13l10-6.5L8 5.5Z'}]],
  filter: [['path', {d: 'M4 7h16M7 12h10M10 17h4'}]],
  chart: [['rect', {x: 3, y: 13, width: 4, height: 7, rx: 1}], ['rect', {x: 10, y: 4, width: 4, height: 16, rx: 1}], ['rect', {x: 17, y: 9, width: 4, height: 11, rx: 1}]],
  select: [['rect', {x: 4, y: 4, width: 16, height: 16, rx: 3}], ['path', {d: 'm8 12 3 3 5-6'}]],
  sidebar: [['rect', {x: 3, y: 4, width: 18, height: 16, rx: 2}], ['path', {d: 'M15 4v16'}]],
  folder: [['path', {d: 'M3 8V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8h18'}]],
  previous: [['path', {d: 'M6 5v14m12-14L8 12l10 7Z'}]],
  next: [['path', {d: 'M18 5v14M6 5l10 7-10 7Z'}]],
  pause: [['path', {d: 'M8 5v14M16 5v14', 'stroke-width': 4}]],
  rotate: [['rect', {x: 7, y: 6, width: 10, height: 12, rx: 2, transform: 'rotate(-25 12 12)'}], ['path', {d: 'M3 10V5h5m13 9v5h-5M3 5a11 11 0 0 1 17 2M21 19A11 11 0 0 1 4 17'}]],
  fullscreen: [['path', {d: 'M9 3H3v6m12-6h6v6M3 15v6h6m12-6v6h-6'}]],
  clock: [['circle', {cx: 12, cy: 12, r: 9}], ['path', {d: 'M12 7v5l3 2'}]],
  activity: [['path', {d: 'M3 12h4l3-8 4 16 3-8h4'}]],
  help: [['circle', {cx: 12, cy: 12, r: 9}], ['path', {d: 'M9.5 9a2.5 2.5 0 1 1 4.3 1.75c-.9.7-1.8 1.2-1.8 2.75'}], ['circle', {cx: 12, cy: 17, r: .75, fill: 'currentColor', stroke: 'none'}]],
  check: [['path', {d: 'm5 12 4 4L19 6'}]]
};

const iconTemplates = new Map();

export function icon(name) {
  if (iconTemplates.has(name)) return iconTemplates.get(name).cloneNode(true);
  const namespace = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(namespace, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  for (const [tag, attributes] of iconShapes[name] || iconShapes.play) {
    const shape = document.createElementNS(namespace, tag);
    for (const [key, value] of Object.entries(attributes)) shape.setAttribute(key, value);
    svg.appendChild(shape);
  }
  iconTemplates.set(name, svg);
  return svg.cloneNode(true);
}

export function populateIcons(root = document) {
  root.querySelectorAll('[data-icon]').forEach(node => node.replaceWith(icon(node.dataset.icon)));
}

export function readPreference(key, fallback) {
  try {const value = localStorage.getItem('duanju.' + key); return value === null ? fallback : JSON.parse(value);} catch (_) {return fallback;}
}

export function savePreference(key, value) {
  try {localStorage.setItem('duanju.' + key, JSON.stringify(value));} catch (_) {}
}

export function reconcileChildren(root, nodes) {
  const keep = new Set(nodes);
  for (const child of Array.from(root.childNodes)) if (!keep.has(child)) child.remove();
  let cursor = root.firstChild;
  for (const node of nodes) {
    if (cursor === node) cursor = cursor.nextSibling;
    else root.insertBefore(node, cursor);
  }
}

export function withFocus(root, render) {
  const active = document.activeElement;
  const key = root.contains(active) && active.dataset.focusKey;
  render();
  if (key && !active.isConnected) root.querySelector('[data-focus-key="' + CSS.escape(key) + '"]')?.focus({preventScroll: true});
}

export function initial(title) {
  return Array.from(String(title || '剧').replace(/[《》「」\s]/g, '')).slice(0, 2).join('');
}

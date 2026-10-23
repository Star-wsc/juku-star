(() => {
  const lifetime = 8;
  const windowMS = 30000;


  function plan(items, width, rows, fontSize, measure) {
    if (width <= 0 || rows < 1) return [];
    const lanes = Array(rows).fill(null), result = [], seen = new Set();
    const valid = items.filter(item => item && typeof item.id === 'string' && item.id && typeof item.text === 'string' && item.text.trim() && Number.isFinite(item.timeMs) && item.timeMs >= 0);
    for (const item of valid.slice().sort((a, b) => a.timeMs - b.timeMs)) {
      if (seen.has(item.id)) continue;
      seen.add(item.id);
      const text = Array.from(item.text).slice(0, 100).join('');
      const size = Math.ceil(measure ? measure(text) : Array.from(text).length * fontSize * 1.1) + 8;
      const start = item.timeMs / 1000;
      const lane = lanes.findIndex(previous => {
        if (!previous || start - previous.start >= lifetime) return true;
        const gap = start - previous.start;
        return (width + previous.size) * gap / lifetime - previous.size >= 24 &&
          width - (width + size) * (lifetime - gap) / lifetime >= 24;
      });
      if (lane < 0) continue;
      const entry = {id: item.id, text, start, lane, size};
      lanes[lane] = entry;
      result.push(entry);
    }
    return result;
  }

  function frames(schedule, position, width) {
    return schedule.filter(item => position >= item.start && position < item.start + lifetime)
      .map(item => ({...item, x: width - (width + item.size) * (position - item.start) / lifetime}));
  }



  function createClock() {
    let media = 0, wall = 0, rate = 1, moving = false;
    const position = now => media + (moving ? Math.max(0, now - wall) * rate / 1000 : 0);
    return {
      position,
      sync(value, now, speed, running, force = false) {
        if (!Number.isFinite(value)) return;
        if (force || running !== moving || speed !== rate || Math.abs(value - position(now)) > .35) {
          media = value;
          wall = now;
          rate = speed;
          moving = running;
        }
      }
    };
  }

  if (typeof module !== 'undefined' && module.exports) module.exports = {plan, frames, lifetime, createClock};
  if (typeof document === 'undefined') return;
  const node = id => document.getElementById(id);
  const video = node('onlineVideo'), stage = node('playbackStage'), layer = node('playbackDanmaku');
  const control = node('danmakuControl'), enabled = node('danmakuEnabled'), status = node('danmakuStatus');
  let session = '', episode = 0, supported = false, ready = false, generation = 0;
  let pages = new Map(), failures = new Map(), schedule = [], elements = new Map();
  let controller = null, pendingStart = -1, animation = 0, width = 0, fontSize = 18, rows = 3;
  let waiting = false;
  const clock = createClock();
  const measure = document.createElement('canvas').getContext('2d');
  try {enabled.checked = localStorage.getItem('juku-danmaku-enabled') !== 'false';} catch (_) {}

  function clearLayer() {
    for (const item of elements.values()) item.motion?.cancel();
    layer.replaceChildren();
    elements.clear();
  }

  function moving() {
    return supported && enabled.checked && ready && !video.paused && !video.ended && !video.seeking && !waiting && !document.hidden;
  }

  function cancelRequest() {
    if (controller) controller.abort();
    controller = null;
    pendingStart = -1;
  }

  function suspend() {
    ready = false;
    waiting = false;
    cancelAnimationFrame(animation);
    animation = 0;
    cancelRequest();
    clearLayer();
  }

  function setEpisode(id, index, canLoad) {
    suspend();
    if (session !== id || episode !== index) {
      generation++;
      pages = new Map();
      failures = new Map();
      schedule = [];
    }
    session = id;
    episode = index;
    supported = Boolean(canLoad);
    control.hidden = !supported;
    status.hidden = !supported;
    status.textContent = supported && enabled.checked ? '等待播放' : '';
  }

  function close() {
    setEpisode('', 0, false);
    if (document.fullscreenElement === stage) document.exitFullscreen().catch(() => {});
    else if (document.webkitFullscreenElement === stage && document.webkitExitFullscreen) document.webkitExitFullscreen();
  }

  function rebuild() {
    width = layer.clientWidth;
    fontSize = width < 600 ? 16 : 20;
    rows = Math.max(1, Math.min(4, Math.floor(layer.clientHeight * .55 / (fontSize * 1.5))));
    layer.style.fontSize = fontSize + 'px';
    if (measure) measure.font = '600 ' + fontSize + 'px sans-serif';
    schedule = plan([...pages.values()].flatMap(page => page.items), width, rows, fontSize, measure ? text => measure.measureText(text).width : undefined);
  }

  function draw(now = performance.now(), sync = false) {
    if (!supported || !enabled.checked || !ready || video.seeking || document.hidden) return;
    const position = clock.position(now), running = moving();
    const live = frames(schedule, position, width), active = new Set(live.map(item => item.id));
    for (const [id, item] of elements) {
      if (!active.has(id)) {item.motion?.cancel(); item.node.remove(); elements.delete(id);}
    }
    for (const item of live) {
      let element = elements.get(item.id);
      const y = item.lane * fontSize * 1.5;


      if (element && (element.width !== width || element.size !== item.size || element.y !== y)) {
        element.motion?.cancel(); element.node.remove(); elements.delete(item.id); element = null;
      }
      if (!element) {
        const span = document.createElement('span');
        span.textContent = item.text;
        span.className = 'danmaku-item';
        layer.appendChild(span);
        const motion = span.animate ? span.animate([
          {transform: 'translate3d(' + width + 'px,' + y + 'px,0)'},
          {transform: 'translate3d(' + -item.size + 'px,' + y + 'px,0)'}
        ], {duration: lifetime * 1000, easing: 'linear', fill: 'both'}) : null;
        if (motion) {motion.pause(); motion.currentTime = (position - item.start) * 1000;}
        element = {node: span, motion, width, size: item.size, y};
        elements.set(item.id, element);
      }
      if (element.motion) {
        const motion = element.motion, elapsed = (position - item.start) * 1000;
        if (motion.playbackRate !== video.playbackRate) motion.playbackRate = video.playbackRate;
        if ((sync && Math.abs(Number(motion.currentTime) - elapsed) > 200) || !running) motion.currentTime = elapsed;
        if (running && motion.playState !== 'running') motion.play();
        else if (!running && motion.playState !== 'paused') motion.pause();
      } else {
        element.node.style.transform = 'translate3d(' + item.x.toFixed(2) + 'px,' + y + 'px,0)';
      }
    }
  }

  function refreshStatus() {
    if (!supported || !enabled.checked) {status.textContent = ''; return;}
    const count = [...pages.values()].reduce((sum, page) => sum + page.items.length, 0);
    status.textContent = count ? count + ' 条已载入' : pages.size ? '这段暂无弹幕' : '等待播放';
  }

  async function loadWindow(start, duration) {
    const token = generation, currentSession = session, currentEpisode = episode;
    const requestController = new AbortController();
    controller = requestController;
    pendingStart = start;
    if (!pages.size) status.textContent = '加载中…';
    const timeout = setTimeout(() => requestController.abort(), 12000);
    try {
      const query = new URLSearchParams({session: currentSession, episode: String(currentEpisode), start: String(start), duration: String(duration)});
      const response = await fetch('/api/ui/playback/danmaku?' + query, {signal: requestController.signal, credentials: 'same-origin', headers: {'Accept': 'application/json', ...window.JukuViewer?.headers()}});
      if (!response.ok) throw new Error('弹幕暂不可用');
      const page = await response.json();
      if (token !== generation || controller !== requestController || requestController.signal.aborted) return;
      if (page.startMs !== start || !Array.isArray(page.items) || !Number.isFinite(page.nextMs) || page.nextMs <= start) throw new Error('弹幕数据无效');
      page.items = page.items.filter(item => item && typeof item.id === 'string' && typeof item.text === 'string' && Number.isFinite(item.timeMs) && item.timeMs >= 0 && item.timeMs < duration).slice(0, 90);
      pages.set(start, page);
      failures.delete(start);
      const current = Math.floor(video.currentTime * 1000 / windowMS) * windowMS;
      const farthest = [...pages.keys()].sort((a, b) => Math.abs(a - current) - Math.abs(b - current));
      for (const key of farthest.slice(6)) pages.delete(key);
      rebuild();
      refreshStatus();
      draw();
    } catch (error) {
      if (token === generation && controller === requestController) {
        failures.set(start, Date.now() + 15000);
        status.textContent = '弹幕暂不可用';
      }
    } finally {
      clearTimeout(timeout);
      if (controller === requestController) {controller = null; pendingStart = -1;}
    }
  }

  function ensureWindow() {
    if (!supported || !enabled.checked || !ready || video.seeking || document.hidden || !session || !Number.isFinite(video.duration) || video.duration <= 0) return;
    const position = Math.max(0, Math.floor(video.currentTime * 1000));
    const duration = Math.min(86400000, Math.ceil(video.duration * 1000));
    if (position >= duration) return;
    const current = Math.floor(position / windowMS) * windowMS;
    let wanted = current;
    if (pages.has(current)) {
      const next = pages.get(current).nextMs;
      if (position < next - 8000 || next >= duration || pages.has(next)) return;
      wanted = next;
    }
    if ((failures.get(wanted) || 0) > Date.now()) return;
    if (controller) {
      if (pendingStart === wanted || pages.has(current)) return;
      cancelRequest();
    }
    loadWindow(wanted, duration);
  }

  function tick(now = performance.now()) {
    animation = 0;
    draw(now);
    if (moving()) animation = requestAnimationFrame(tick);
  }

  function update(force = false) {
    const now = performance.now();
    clock.sync(video.currentTime, now, video.playbackRate, moving(), force);
    ensureWindow();
    draw(now, true);
    if (moving()) {
      if (!animation) animation = requestAnimationFrame(tick);
    } else {
      cancelAnimationFrame(animation);
      animation = 0;
    }
  }

  video.addEventListener('loadedmetadata', () => {ready = true; waiting = false; rebuild(); update(true);});
  video.addEventListener('playing', () => {ready = true; waiting = false; update(true);});
  video.addEventListener('waiting', () => {waiting = true; update(true);});
  video.addEventListener('timeupdate', () => update());
  video.addEventListener('seeked', () => {waiting = false; update(true);});
  for (const event of ['pause', 'ratechange']) video.addEventListener(event, () => update(true));
  video.addEventListener('seeking', () => {cancelRequest(); clearLayer(); update(true);});
  video.addEventListener('ended', suspend);
  enabled.addEventListener('change', () => {
    try {localStorage.setItem('juku-danmaku-enabled', String(enabled.checked));} catch (_) {}
    cancelRequest();
    failures.clear();
    if (!enabled.checked) {cancelAnimationFrame(animation); animation = 0; clearLayer();}
    refreshStatus();
    update(true);
  });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      cancelAnimationFrame(animation); animation = 0;
      for (const item of elements.values()) item.motion?.pause();
    } else update(true);
  });
  if (window.ResizeObserver) new ResizeObserver(() => {rebuild(); draw(performance.now(), true);}).observe(layer);
  else window.addEventListener('resize', () => {rebuild(); draw(performance.now(), true);});

  const fullscreen = node('playerFullscreenBtn'), exit = node('exitPlayerFullscreenBtn');
  const supportsFullscreen = Boolean(stage.requestFullscreen || stage.webkitRequestFullscreen);
  fullscreen.hidden = !supportsFullscreen;
  if (supportsFullscreen) video.setAttribute('controlslist', 'nofullscreen');
  fullscreen.addEventListener('click', async () => {
    try {
      if (stage.requestFullscreen) await stage.requestFullscreen();
      else stage.webkitRequestFullscreen();
    } catch (_) {status.textContent = '浏览器未允许全屏';}
  });
  exit.addEventListener('click', () => {
    if (document.exitFullscreen) document.exitFullscreen().catch(() => {});
    else if (document.webkitExitFullscreen) document.webkitExitFullscreen();
  });
  // 网页全屏：铺满视口，不调用 Fullscreen API
  const webFullBtn = node('webFullscreenBtn');
  const panelEl = node('playerPanel');
  const setWebFull = (on) => {
    panelEl.classList.toggle('web-fullscreen', on);
    webFullBtn.setAttribute('aria-pressed', on ? 'true' : 'false');
    webFullBtn.textContent = on ? '退出网页全屏' : '网页全屏';
  };
  webFullBtn.addEventListener('click', () => {
    setWebFull(!panelEl.classList.contains('web-fullscreen'));
  });
  // 系统全屏与网页全屏互不强绑；关闭播放器时退出网页全屏
  window.JukuExitWebFullscreen = () => setWebFull(false);
  window.JukuPlaybackDanmaku = {setEpisode, suspend, close};
})();

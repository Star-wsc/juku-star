(() => {
  const node = id => document.getElementById(id);
  const panel = node('playerPanel');
  const video = node('onlineVideo');
  const episodeList = node('playbackEpisodes');
  const statusText = node('playbackStatus');
  const errorText = node('playbackError');
  const qualityControl = node('playbackQualityControl');
  const qualitySelect = node('playbackQuality');
  let quality = 0;
  let transport = 'mse';
  let allowRemux = false, streamRemux = false;
  try {quality = Number(localStorage.getItem('juku.playback.quality')) || 0;} catch (_) {}
  if (!Number.isInteger(quality) || quality < 0 || quality > 4320) quality = 0;
  let dramaID = '';
  let dramaName = '';
  let collectionTaskID = '';
  let collectionMode = false;
  let preparedIndex = 0;
  let sessionID = '';
  let sessionAvailable = false;
  let mimeType = '';
  let episodes = [];
  let episodeButtons = [];
  let episodePage = 0, prefetchStatusTimer = null, prefetchPreparing = false;
  const completedEpisodes = new Set();
  let currentIndex = 0;
  let openingVersion = 0;
  let streamVersion = 0;
  let openingController = null;
  let streamController = null;
  let objectURL = '';
  let loading = false;
  let recoveryPromise = null, recoveryTarget = null, recoveryBlocked = false;
  let recoveryAttempts = [];
  let seekTimer = null;
  let lastPosition = 0;
  let playbackRun = 0;
  let streamComplete = false;
  let prefetchAttempted = 0;
  let prefetchVersion = 0;
  let historySource = '', releaseStatus = '';
  let historySequence = 0;
  let historyPlayed = false;
  let historyCompleted = false;
  let historySentAt = 0;
  let historySignature = '';
  let playbackDuration = 0;
  let historyMessage = '';
  const historyStatus = node('playbackHistoryStatus');
  const prefetchToggle = node('prefetchNextEpisode');
  const prefetchStatus = node('prefetchStatus');
  try {prefetchToggle.checked = localStorage.getItem('juku.playback.prefetchNext') !== 'false';} catch (_) {}
  const mobile = window.JukuPlayerMobile({panel, video, state: () => ({title: dramaName, episode: episodes[currentIndex - 1]?.episode, total: episodes.length, duration: playbackDuration, loading, releaseStatus, vip: episodes[currentIndex - 1]?.vip}), showEpisodes: showEpisodePanel, showPreferences: section => setPlayerPreferences(true, section)});
  const controls = window.JukuPlayerControls({panel, video, mobile: () => mobile.active(), rotation: () => mobile.rotation(), canSwipe: () => panel.open && episodes.length > 0 && !panel.classList.contains('episodes-open') && node('playerPreferences').hidden, swipe: direction => {
    const next = currentIndex + direction;
    if (next < 1) return '已经是第一集';
    if (next > episodes.length) return '已经是最后一集';
    playEpisode(next);
    return '第 ' + episodes[next - 1].episode + ' 集';
  }, ready: () => !loading && Boolean(currentIndex), fullscreen: () => {
    if (document.fullscreenElement === node('playbackStage') || document.webkitFullscreenElement === node('playbackStage')) node('exitPlayerFullscreenBtn').click();
    else if (!node('playerFullscreenBtn').hidden) node('playerFullscreenBtn').click();
  }});

  function clear(element) {
    while (element.firstChild) element.removeChild(element.firstChild);
  }

  const capabilities = window.JukuPlaybackCapabilities(window, video, navigator);
  const supportsNativePlayback = capabilities.native;
  const supportsMediaSource = capabilities.mse;
  let desiredPlayback = true;
  const keepalive = window.JukuPlaybackSession({
    request: (session, signal) => requestJSON('/api/ui/playback/control', {session, action: 'heartbeat'}, signal),
    onState: state => {if (state.run === playbackRun) renderPrefetchStatus(state.prefetch);},
    onExpired: () => {sessionAvailable = false; recoverPlayback();},
    onError: error => {sessionAvailable = false; recoveryBlocked = true; showError(error);}
  });

  function fallbackPlayback(index, offset, shouldPlay, keepResumeMessage = true) {
    if (transport === 'mse' && allowRemux && streamRemux) {
      allowRemux = false;
      playEpisode(index, offset, shouldPlay, keepResumeMessage);
      return true;
    }
    if (transport !== 'mse' || !supportsNativePlayback()) return false;
    transport = 'hls';
    playEpisode(index, offset, shouldPlay, keepResumeMessage);
    return true;
  }

  function updateQualities(options, current) {
    clear(qualitySelect);
    const automatic = document.createElement('option');
    automatic.value = '0';
    automatic.textContent = current > 0 ? '自动（' + current + 'p）' : '自动';
    qualitySelect.appendChild(automatic);
    const values = new Set();
    for (const entry of Array.isArray(options) ? options : []) {
      const value = Number(entry.value);
      if (!Number.isInteger(value) || value <= 0 || value > 4320 || values.has(value)) continue;
      values.add(value);
      const option = document.createElement('option');
      option.value = String(value);
      option.textContent = value + 'p';
      qualitySelect.appendChild(option);
    }
    qualitySelect.value = values.has(quality) ? String(quality) : '0';
    qualityControl.hidden = values.size < 2;
    qualitySelect.disabled = loading;
    mobile.update();
  }

  async function requestJSON(path, body, signal) {
    const controller = new AbortController();
    const abort = () => controller.abort();
    if (signal?.aborted) abort();
    else signal?.addEventListener('abort', abort, {once: true});
    let timedOut = false;
    const timeout = setTimeout(() => {timedOut = true; controller.abort();}, 100000);
    try {
      const response = await fetch(path, {
        method: body === undefined ? 'GET' : 'POST',
        headers: {'Accept': 'application/json', 'Content-Type': 'application/json', ...window.JukuViewer?.headers()},
        credentials: 'same-origin',
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal
      });
      const result = await response.json();
      window.JukuViewer?.checkResponse(result);
      if (!response.ok) {
        const error = new Error(result.error || 'HTTP ' + response.status);
        error.status = response.status;
        throw error;
      }
      return result;
    } catch (error) {
      if (timedOut) throw new Error('播放请求超时，请检查连接后重试');
      throw error;
    } finally {
      clearTimeout(timeout);
      signal?.removeEventListener('abort', abort);
    }
  }

  function historySnapshot(force = false, repeat = false) {
    if (!historyPlayed || !sessionID || !playbackRun || !episodes[currentIndex - 1] || !window.JukuHistory) return null;
    const position = !loading && Number.isFinite(video.currentTime) ? video.currentTime : lastPosition;
    const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : playbackDuration;
    if (!Number.isFinite(position) || position < 0) return null;
    const signature = playbackRun + ':' + position.toFixed(3) + ':' + historyCompleted;
    if (!repeat && signature === historySignature || !force && Date.now() - historySentAt < 10000) return null;
    historySignature = signature;
    historySentAt = Date.now();
    const episode = episodes[currentIndex - 1];
    const progress = {run: playbackRun, sequence: ++historySequence, episode: currentIndex, position, duration: duration || 0, completed: historyCompleted};
    const entry = {dramaId: dramaID, title: dramaName, source: historySource, chapterId: episode.chapterId || '', episode: episode.episode, index: episode.number || currentIndex, total: episode.total || episodes.length, position, duration: duration || 0, completed: historyCompleted, mode: collectionMode ? 'collection' : 'online', taskId: episode.taskId || '', watchedAt: new Date().toISOString()};
    return {progress, entry};
  }

  function saveHistory(force = false) {
    const snapshot = historySnapshot(force);
    if (!snapshot) return;
    const session = sessionID;
    window.JukuHistory.save(session, snapshot.progress, snapshot.entry).then(() => {
      if (session === sessionID && snapshot.progress.sequence === historySequence) historyStatus.textContent = historyMessage;
    }).catch(error => {
      if (session !== sessionID || snapshot.progress.sequence !== historySequence) return;
      historySignature = '';
      if (error.status === 410) {sessionAvailable = false; recoverPlayback(); return;}
      historyStatus.textContent = '观看进度暂未保存：' + error.message;
    });
  }

  function releaseSession(id, progress, unloading = false) {
    if (!id) return;
    const body = JSON.stringify({session: id, action: 'close', ...(progress ? {progress} : {})});
    if (unloading && navigator.sendBeacon && navigator.sendBeacon('/api/ui/playback/control', new Blob([body], {type: 'application/json'}))) return;
    fetch('/api/ui/playback/control', {method: 'POST', headers: {'Content-Type': 'application/json', ...window.JukuViewer?.headers()}, credentials: 'same-origin', body, keepalive: true}).then(response => response.json()).then(result => {
      window.JukuViewer?.checkResponse(result);
      if (progress && result.historyError) window.JukuHistory?.reportError(result.historyError);
    }).catch(error => {if (progress && !unloading) window.JukuHistory?.reportError(error.message);});
  }

  function stopStream() {
    controls.reset();
    saveHistory(true);
    historyPlayed = false;
    historyCompleted = false;
    historySignature = '';
    playbackDuration = 0;
    window.JukuPlaybackDanmaku?.suspend();
    streamVersion++;
    playbackRun = 0;
    streamComplete = false;
    prefetchAttempted = 0;
    prefetchPreparing = false;
    clearTimeout(prefetchStatusTimer);
    prefetchVersion++;
    prefetchStatus.hidden = true;
    prefetchStatus.textContent = '';
    loading = true;
    qualityControl.hidden = true;
    qualitySelect.disabled = true;
    clearTimeout(seekTimer);
    if (streamController) streamController.abort();
    streamController = null;
    video.pause();
    video.removeAttribute('src');
    video.load();
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = '';
  }

  function dispose(unloading = false) {
    const snapshot = historySnapshot(true, true);
    if (snapshot) window.JukuHistory.remember(snapshot.entry);
    historyPlayed = false;
    openingVersion++;
    if (openingController) openingController.abort();
    openingController = null;
    keepalive.stop();
    recoveryPromise = null;
    recoveryTarget = null;
    stopStream();
    releaseSession(sessionID, snapshot?.progress, unloading);
    sessionID = '';
    sessionAvailable = false;
    preparedIndex = 0;
    window.JukuPlaybackDanmaku?.close();
  }

  function updateEpisodeControls() {
    node('previousEpisodeBtn').disabled = currentIndex <= 1;
    node('nextEpisodeBtn').disabled = currentIndex < 1 || currentIndex >= episodes.length;
    node('retryPlaybackBtn').disabled = !dramaID && !collectionTaskID;
    node('retryPlaybackBtn').hidden = !errorText.textContent;
    for (const button of episodeButtons) {
      const index = Number(button.dataset.episodeIndex);
      button.setAttribute('aria-current', String(index === currentIndex));
      button.classList.toggle('watched', completedEpisodes.has(index));
    }
    node('playbackEpisodeCount').textContent = episodes.length ? '第 ' + (episodes[currentIndex - 1]?.episode || currentIndex || '—') + ' 集 / 共 ' + episodes.length + ' 集' : '正在获取分集';
    node('jumpEpisode').max = episodes.length || 1;
    node('jumpEpisode').disabled = !episodes.length;
    node('jumpEpisodeBtn').disabled = !episodes.length;
    node('currentEpisodeBtn').disabled = !currentIndex;
    node('episodeRange').disabled = !episodes.length;
    mobile.update();
  }

  function showError(error) {
    controls.reset();
    window.JukuPlaybackDanmaku?.suspend();
    loading = false;
    qualitySelect.disabled = false;
    errorText.textContent = (error.message || String(error)) + '；可点击“重试播放”。';
    statusText.textContent = '播放未完成';
    if (error.status === 410) sessionAvailable = false;
    updateEpisodeControls();
  }

  function updateDependency(state, text) {
    if (!panel.open || !openingController || sessionID || errorText.textContent) return;
    if (state.status === 'downloading' || state.status === 'verifying') statusText.textContent = text;
    else if (state.status === 'ready') statusText.textContent = collectionMode ? '正在读取合集分集…' : '正在获取分集…';
  }

  function renderEpisodes(page = episodePage) {
    episodePage = Math.max(0, Math.min(page, Math.ceil(episodes.length / 30) - 1));
    clear(episodeList);
    const range = node('episodeRange');
    clear(range);
    for (let start = 0; start < episodes.length; start += 30) {
      const option = document.createElement('option');
      option.value = String(start / 30);
      option.textContent = (start + 1) + '–' + Math.min(episodes.length, start + 30) + ' 集';
      range.appendChild(option);
    }
    range.value = String(episodePage);
    episodeButtons = episodes.slice(episodePage * 30, episodePage * 30 + 30).map(episode => {
      const button = document.createElement('button');
      button.className = 'secondary';
      button.textContent = episode.episode + (episode.vip ? ' VIP' : '');
      button.classList.toggle('vip-episode', Boolean(episode.vip));
      button.title = (episode.title || '第 ' + episode.episode + ' 集') + (episode.vip ? ' · VIP，仅试看' : '');
      button.setAttribute('aria-label', '播放第 ' + episode.episode + ' 集' + (episode.vip ? '，VIP，仅试看' : ''));
      button.dataset.episodeIndex = episode.index;
      button.addEventListener('click', () => {playEpisode(episode.index); closeEpisodePanel();});
      episodeList.appendChild(button);
      return button;
    });
    updateEpisodeControls();
  }

  function heartbeat(fresh = false) {return keepalive.ping(fresh);}

  function recoveryPosition() {
    const position = !loading && !video.error && Number.isFinite(video.currentTime) ? video.currentTime : lastPosition;
    const duration = playbackDuration || video.duration;
    return Math.max(0, Number.isFinite(duration) && duration > 0 ? Math.min(position, Math.max(0, duration - .25)) : position);
  }

  function recoverPlayback(index = currentIndex, offset = recoveryPosition(), shouldPlay = desiredPlayback) {
    const episode = episodes[index - 1];
    if (!panel.open || !episode || recoveryBlocked) return Promise.resolve(false);
    const target = {index, offset, shouldPlay, chapter: episode.chapterId, episode: episode.episode, task: episode.taskId};
    if (recoveryPromise) {if (arguments.length) recoveryTarget = target; return recoveryPromise;}
    recoveryTarget = target;
    sessionAvailable = false;
    if (document.hidden || navigator.onLine === false) return Promise.resolve(false);
    recoveryAttempts = recoveryAttempts.filter(time => Date.now() - time < 60000);
    if (recoveryAttempts.length >= 2) {
      showError(new Error('播放连接暂未恢复，请检查网络后重试'));
      return Promise.resolve(false);
    }
    recoveryAttempts.push(Date.now());
    const snapshot = historySnapshot(true, true);
    if (snapshot) window.JukuHistory?.remember(snapshot.entry);
    const completed = new Set(Array.from(completedEpisodes, number => episodes[number - 1]?.chapterId));
    const previousSession = sessionID;
    const version = ++openingVersion;
    keepalive.stop();
    openingController?.abort();
    const controller = new AbortController();
    openingController = controller;
    historyPlayed = false;
    stopStream();
    sessionID = '';
    releaseSession(previousSession);
    errorText.textContent = '';
    statusText.textContent = '正在恢复播放连接…';
    updateEpisodeControls();
    const promise = (async () => {
      try {
        const selection = collectionMode ? {taskId: target.task || collectionTaskID} : {dramaId: dramaID};
        const result = await requestJSON('/api/ui/playback/open', {...selection, resume: false}, controller.signal);
        if (version !== openingVersion || !panel.open) {releaseSession(result.session); return false;}
        sessionID = result.session;
        sessionAvailable = true;
        mimeType = result.mimeType || mimeType;
        dramaID = result.dramaId || dramaID;
        dramaName = result.title || dramaName;
        historySource = result.source || historySource;
        releaseStatus = result.releaseStatus || releaseStatus;
        episodes = result.episodes || [];
        if (!episodes.length) throw new Error('站点没有返回可播放的分集');
        const latest = recoveryTarget || target;
        let selected = latest.chapter && episodes.find(item => item.chapterId === latest.chapter);
        if (!selected) selected = episodes.find(item => item.episode === latest.episode);
        if (!selected) throw new Error('原分集已不在当前选集中，请重新选集');
        completedEpisodes.clear();
        for (const item of episodes) if (item.chapterId && completed.has(item.chapterId)) completedEpisodes.add(item.index);
        preparedIndex = 0;
        historySequence = 0;
        historySentAt = 0;
        historyMessage = '已自动恢复到刚才的观看位置';
        historyStatus.textContent = historyMessage;
        node('playerTitle').textContent = dramaName;
        renderEpisodes(Math.floor((selected.index - 1) / 30));
        keepalive.start(sessionID);
        recoveryTarget = null;
        playEpisode(selected.index, Math.max(0, latest.offset), latest.shouldPlay, true);
        return true;
      } catch (error) {
        if (version === openingVersion && error.name !== 'AbortError') {
          if (error.status === 401 || error.status === 403) recoveryBlocked = true;
          sessionAvailable = false;
          showError(error);
        }
        return false;
      } finally {
        if (recoveryPromise === promise) recoveryPromise = null;
      }
    })();
    recoveryPromise = promise;
    return promise;
  }

  async function wakePlayback() {
    if (!panel.open || document.hidden || navigator.onLine === false || recoveryBlocked || recoveryPromise) return;
    if (!sessionAvailable) {
      const target = recoveryTarget;
      if (target) recoverPlayback(target.index, !loading && target.index === currentIndex ? recoveryPosition() : target.offset, !loading && target.index === currentIndex ? desiredPlayback : target.shouldPlay);
      else recoverPlayback();
      return;
    }
    const version = streamVersion;
    const state = await heartbeat(true);
    if (state && version === streamVersion && panel.open && video.error) recoverPlayback();
  }

  async function open(id, title, initialIndex = 0, offset = 0, taskID = '', resume = true, fromHistory = false) {
    dispose();
    recoveryAttempts = [];
    recoveryBlocked = false;
    dramaID = id;
    dramaName = title;
    collectionTaskID = taskID;
    collectionMode = Boolean(taskID);
    episodes = [];
    episodeButtons = [];
    episodePage = 0;
    completedEpisodes.clear();
    currentIndex = 0;
    lastPosition = offset;
    historySource = '';
    releaseStatus = '';
    historySequence = 0;
    historyMessage = '';
    historySentAt = 0;
    historyStatus.textContent = '';
    node('playerTitle').textContent = title;
    statusText.textContent = collectionMode ? '正在读取合集分集…' : '正在获取分集…';
    updatePlaybackHint('');
    errorText.textContent = '';
    clear(episodeList);
    updateEpisodeControls();
    if (!panel.open) {panel.showModal(); video.focus({preventScroll: true});}
    if (!supportsMediaSource('video/mp4; codecs="avc1.42C01F, mp4a.40.2"') && !supportsNativePlayback()) {
      showError(new Error('当前浏览器不支持 H.264 在线播放，请更新浏览器后重试'));
      return;
    }
    const version = openingVersion;
    openingController = new AbortController();
    try {
      const shouldResume = resume && initialIndex === 0;
      const result = await requestJSON('/api/ui/playback/open', {...(taskID ? {taskId: taskID} : {dramaId: id}), resume: shouldResume, fromHistory}, openingController.signal);
      if (version !== openingVersion || !panel.open) {
        releaseSession(result.session);
        return;
      }
      sessionID = result.session;
      sessionAvailable = true;
      mimeType = result.mimeType;
      dramaID = result.dramaId || id;
      dramaName = result.title || title;
      historySource = result.source || '';
      releaseStatus = result.releaseStatus || '';
      collectionMode = result.mode === 'collection';
      episodes = result.episodes || [];
      const remembered = window.JukuHistory.get(dramaID);
      if (remembered?.completed) {
        const finished = episodes.find(episode => remembered.chapterId ? episode.chapterId === remembered.chapterId : episode.episode === remembered.episode);
        if (finished) completedEpisodes.add(finished.index);
      }
      transport = capabilities.choose(mimeType);
      allowRemux = transport === 'mse' && capabilities.remux();
      if (!episodes.length) throw new Error('站点没有返回可播放的分集');
      if (!transport) throw new Error('当前浏览器没有可用的在线播放通道，请使用支持 HLS 或 H.264 的浏览器');
      node('playerTitle').textContent = result.title || title;
      renderEpisodes();
      keepalive.start(sessionID);
      historyMessage = shouldResume ? result.resumeMessage || '' : '';
      historyStatus.textContent = historyMessage;
      playEpisode(Math.min(Math.max(initialIndex || result.initialIndex || 1, 1), episodes.length), shouldResume ? Number(result.initialPosition) || 0 : offset, !shouldResume || !result.resumePaused, true);
    } catch (error) {
      if (version === openingVersion && error.name !== 'AbortError') showError(error);
    }
  }

  function openCollection(taskID, title, resume = false) {
    return open('', title, 0, 0, taskID, resume);
  }

  function openHistory(id, title) {
    return open(id, title, 0, 0, '', true, true);
  }

  function reopen(index, offset) {
    if (collectionMode) return open('', dramaName, 0, offset, episodes[index - 1]?.taskId || collectionTaskID, false);
    return open(dramaID, dramaName, index, offset, '', false);
  }

  function updatePlaybackHint(source) {
    if (!collectionMode) {
      node('playbackHint').textContent = '直接观看，不加入下载任务。开启预缓存后，临近播完时提前准备下一集；关闭窗口即停止取流并释放缓存。';
    } else {
      const prefix = source === 'local' ? '本集播放本地已完成文件。' : source === 'online' ? '本集在线缓冲，同时使用原下载任务保存视频。' : '已完成分集优先播放本地，播到未完成分集时自动下载该集。';
      node('playbackHint').textContent = prefix + '只补下载播到的分集；关闭播放器不取消下载，可在下载合集中暂停或取消。';
    }
  }

  function abortError() {
    return new DOMException('播放已停止', 'AbortError');
  }

  function waitForEvent(target, eventName, signal, action) {
    return new Promise((resolve, reject) => {
      function cleanup() {
        target.removeEventListener(eventName, done);
        target.removeEventListener('error', failed);
        signal.removeEventListener('abort', aborted);
      }
      function done() {cleanup(); resolve();}
      function failed() {cleanup(); const error = new Error('浏览器无法解码此视频流'); error.playbackDecode = true; reject(error);}
      function aborted() {cleanup(); reject(abortError());}
      if (signal.aborted) {aborted(); return;}
      target.addEventListener(eventName, done, {once: true});
      target.addEventListener('error', failed, {once: true});
      signal.addEventListener('abort', aborted, {once: true});
      try {if (action) action();} catch (error) {cleanup(); reject(error);}
    });
  }

  function bufferedAhead() {
    for (let index = 0; index < video.buffered.length; index++) {
      if (video.currentTime >= video.buffered.start(index) && video.currentTime <= video.buffered.end(index)) return video.buffered.end(index) - video.currentTime;
    }
    return 0;
  }

  function schedulePrefetchStatus() {
    clearTimeout(prefetchStatusTimer);
    if (!prefetchPreparing || !prefetchToggle.checked || !panel.open || !sessionID) return;
    const session = sessionID, run = playbackRun;
    prefetchStatusTimer = setTimeout(async () => {
      if (session !== sessionID || run !== playbackRun) return;
      await heartbeat();
      if (session === sessionID && run === playbackRun) schedulePrefetchStatus();
    }, document.hidden ? 10000 : 2000);
  }

  function renderPrefetchStatus(view) {
    if (!prefetchToggle.checked || !view || view.episode !== currentIndex + 1) return;
    prefetchStatus.hidden = false;
    prefetchStatus.textContent = view.state === 'ready' ? '下一集已缓存' : view.state === 'partial' ? '下一集开头已缓存' : view.state === 'failed' ? '下一集将正常缓冲' : '正在缓存下一集…';
    prefetchPreparing = !['ready', 'partial', 'failed'].includes(view.state);
    schedulePrefetchStatus();
  }

  function maybePrefetchNext() {
    if (transport === 'hls' && playbackDuration > 0 && video.currentTime > 0 && bufferedAhead() >= playbackDuration - video.currentTime - 0.5) streamComplete = true;
    if (!prefetchToggle.checked || !streamComplete || loading || video.paused || video.ended || video.seeking || !panel.open || !sessionAvailable || !playbackRun || !currentIndex || currentIndex >= episodes.length || prefetchAttempted === currentIndex) return;
    const remaining = video.duration - video.currentTime;
    if (!Number.isFinite(remaining) || remaining <= 0 || remaining / Math.max(video.playbackRate, 0.25) > 30 || bufferedAhead() < remaining - 0.5) return;
    const session = sessionID;
    const run = playbackRun;
    const version = ++prefetchVersion;
    prefetchAttempted = currentIndex;
    renderPrefetchStatus({episode: currentIndex + 1, state: 'preparing'});
    requestJSON('/api/ui/playback/prefetch', {session, episode: currentIndex + 1, run, version, remux: allowRemux && transport === 'mse'}, openingController?.signal).then(view => {
      if (session === sessionID && run === playbackRun && version === prefetchVersion) renderPrefetchStatus(view);
    }).catch(error => {
      if (session === sessionID && run === playbackRun && version === prefetchVersion && error.name !== 'AbortError') renderPrefetchStatus({episode: currentIndex + 1, state: 'failed'});
    });
  }

  prefetchToggle.addEventListener('change', () => {
    try {localStorage.setItem('juku.playback.prefetchNext', String(prefetchToggle.checked));} catch (_) {}
    prefetchAttempted = 0;
    const version = ++prefetchVersion;
    prefetchStatus.hidden = true;
    prefetchPreparing = false;
    clearTimeout(prefetchStatusTimer);
    if (prefetchToggle.checked) {
      maybePrefetchNext();
    } else if (sessionID && playbackRun) {
      requestJSON('/api/ui/playback/prefetch', {session: sessionID, run: playbackRun, version, cancel: true}, openingController?.signal).catch(() => {});
    }
  });

  async function trimBuffer(buffer, signal) {
    const cutoff = video.currentTime - 20;
    if (cutoff > 0 && buffer.buffered.length && buffer.buffered.start(0) < cutoff) {
      await waitForEvent(buffer, 'updateend', signal, () => buffer.remove(0, cutoff));
    }
  }

  async function playNative(index, offset, shouldPlay, version, currentSession, signal) {
    const result = await requestJSON('/api/ui/playback/hls/open', {session: currentSession, episode: index, start: offset, quality, version}, signal);
    if (signal.aborted || version !== streamVersion) throw abortError();
    playbackRun = Number(result.run);
    playbackDuration = Number(result.duration) || 0;
    updatePlaybackHint(result.source);
    updateQualities(result.qualities, Number(result.quality));
    statusText.textContent = result.prefetched ? '正在读取预缓存…' : '正在缓冲…';
    await waitForEvent(video, 'loadedmetadata', signal, () => {video.src = result.url; video.load();});
    if (signal.aborted || version !== streamVersion) throw abortError();
    if (offset > 0) video.currentTime = Math.min(offset, Math.max(0, playbackDuration - 0.05));
    video.playbackRate = Number(node('playbackRate').value) || 1;
    loading = false;
    qualitySelect.disabled = false;
    mobile.update();
    statusText.textContent = shouldPlay ? '正在播放' : '已暂停';
    if (shouldPlay) video.play().catch(error => {
      if (version !== streamVersion || signal.aborted) return;
      if (error.name === 'NotAllowedError') statusText.textContent = '已就绪，点击视频中的播放按钮';
      else if (error.name !== 'AbortError') showError(error);
    });
  }

  async function playEpisode(index, offset = 0, shouldPlay = true, keepResumeMessage = false) {
    if (!episodes[index - 1]) return;
    if (!sessionAvailable) {recoverPlayback(index, offset, shouldPlay); return;}
    stopStream();
    if (!keepResumeMessage) {
      historyMessage = '';
      historyStatus.textContent = '';
    }
    currentIndex = index;
    streamRemux = false;
    desiredPlayback = shouldPlay;
    if (Math.floor((index - 1) / 30) !== episodePage) renderEpisodes(Math.floor((index - 1) / 30));
    window.JukuPlaybackDanmaku?.setEpisode(sessionID, index, episodes[index - 1].danmaku);
    lastPosition = offset;
    errorText.textContent = '';
    updateEpisodeControls();
    statusText.textContent = offset > 0 ? '正在跳转并缓冲…' : '正在解析播放地址…';
    const version = streamVersion;
    const currentSession = sessionID;
    const controller = new AbortController();
    streamController = controller;
    const signal = controller.signal;
    let reader;
    try {
      if (collectionMode && preparedIndex !== index) {
        statusText.textContent = '正在检查本地分集并准备下载…';
        const preparation = await requestJSON('/api/ui/playback/prepare', {session: currentSession, episode: index}, signal);
        if (signal.aborted) throw abortError();
        preparedIndex = index;
        updatePlaybackHint(preparation.source);
        window.dispatchEvent(new Event('downloadsChanged'));
      }
      if (transport === 'hls') {
        await playNative(index, offset, shouldPlay, version, currentSession, signal);
        return;
      }
      const source = new window.MediaSource();
      objectURL = URL.createObjectURL(source);
      await waitForEvent(source, 'sourceopen', signal, () => {video.src = objectURL;});
      const response = await fetch('/api/ui/playback/stream?' + new URLSearchParams({session: currentSession, episode: String(index), start: String(offset), quality: String(quality), version: String(version), remux: allowRemux ? '1' : '0'}), {signal, cache: 'no-store', credentials: 'same-origin', headers: window.JukuViewer?.headers()});
      if (!response.ok) {
        const result = await response.json();
        window.JukuViewer?.checkResponse(result);
        const error = new Error(result.error || 'HTTP ' + response.status);
        error.status = response.status;
        throw error;
      }
      if (signal.aborted) throw abortError();
      let options = [];
      try {options = JSON.parse(response.headers.get('X-Playback-Qualities') || '[]');} catch (_) {}
      updateQualities(options, Number(response.headers.get('X-Playback-Quality')));
      streamRemux = response.headers.get('X-Playback-Mode') === 'remux';
      const streamMIME = response.headers.get('X-Playback-MIME') || mimeType;
      if (!supportsMediaSource(streamMIME)) {
        if (fallbackPlayback(index, offset, shouldPlay, keepResumeMessage)) return;
        throw new Error('浏览器不支持当前清晰度的编码，可切换较低清晰度后重试');
      }
      updatePlaybackHint(response.headers.get('X-Playback-Source'));
      const duration = Number(response.headers.get('X-Playback-Duration'));
      playbackDuration = Number.isFinite(duration) && duration > 0 ? duration : 0;
      const run = Number(response.headers.get('X-Playback-Run'));
      playbackRun = run;
      const buffer = source.addSourceBuffer(streamMIME);
      buffer.timestampOffset = offset;
      if (duration > 0 && Number.isFinite(duration)) source.duration = duration;
      statusText.textContent = response.headers.get('X-Playback-Prefetched') === '1' ? '正在读取预缓存…' : '正在缓冲…';
      reader = response.body.getReader();
      let initialized = false;
      while (!signal.aborted) {
        while (!signal.aborted && bufferedAhead() > (video.paused && initialized ? 5 : 30)) {
          await new Promise(resolve => setTimeout(resolve, 200));
        }
        if (signal.aborted) throw abortError();
        const chunk = await reader.read();
        if (chunk.done) break;
        await trimBuffer(buffer, signal);
        await waitForEvent(buffer, 'updateend', signal, () => buffer.appendBuffer(chunk.value));
        if (!initialized && buffer.buffered.length) {
          initialized = true;
          video.currentTime = Math.min(buffer.buffered.end(0) - 0.001, Math.max(offset, buffer.buffered.start(0) + 0.03));
          video.playbackRate = Number(node('playbackRate').value) || 1;
          loading = false;
          qualitySelect.disabled = false;
          mobile.update();
          statusText.textContent = shouldPlay ? '正在播放' : '已暂停';
          if (shouldPlay) video.play().catch(error => {
            if (version !== streamVersion || signal.aborted) return;
            if (error.name === 'NotAllowedError') statusText.textContent = '已就绪，点击视频中的播放按钮';
            else if (error.name !== 'AbortError') showError(error);
          });
        }
      }
      if (signal.aborted) throw abortError();
      const state = await requestJSON('/api/ui/playback/status?session=' + encodeURIComponent(currentSession), undefined, signal);
      if (signal.aborted || version !== streamVersion) throw abortError();
      if (state.run !== run || state.state !== 'ended') throw new Error(state.error || '视频连接中断，请重试');
      if (!initialized) throw new Error('未收到可播放的视频画面');
      if (source.readyState === 'open') source.endOfStream();
      streamComplete = true;
      maybePrefetchNext();
    } catch (error) {
      if (reader) await reader.cancel().catch(() => {});
      if (version === streamVersion && !signal.aborted) {
        if (error.status === 410) {recoverPlayback(index, Math.max(offset, lastPosition), desiredPlayback); return;}
        const compatibleFailure = error.playbackDecode || error.name === 'NotSupportedError';
        if (compatibleFailure && fallbackPlayback(index, Math.max(offset, lastPosition), desiredPlayback, keepResumeMessage)) return;
        showError(error);
      }
    } finally {
      if (reader) reader.releaseLock();
    }
  }

  video.addEventListener('seeking', () => {
    if (transport === 'hls') return;
    if (loading || !currentIndex || !Number.isFinite(video.currentTime)) return;
    const target = video.currentTime;
    clearTimeout(seekTimer);
    for (let index = 0; index < video.buffered.length; index++) {
      if (target >= video.buffered.start(index) && target < video.buffered.end(index)) return;
    }
    seekTimer = setTimeout(() => playEpisode(currentIndex, target, !video.paused), 180);
  });
  video.addEventListener('timeupdate', () => {if (!loading && Number.isFinite(video.currentTime)) {lastPosition = video.currentTime; if (!video.paused && !video.seeking) saveHistory();} maybePrefetchNext();});
  video.addEventListener('playing', () => {desiredPlayback = true; if (!loading && !errorText.textContent) {historyPlayed = true; historyCompleted = false; statusText.textContent = '正在播放'; saveHistory(true);} maybePrefetchNext();});
  video.addEventListener('waiting', () => {if (!loading && !errorText.textContent) statusText.textContent = '正在缓冲…';});
  video.addEventListener('pause', () => {if (!loading && !video.error) desiredPlayback = false; if (!loading) saveHistory(true); if (!loading && !video.ended && !errorText.textContent) statusText.textContent = '已暂停';});
  video.addEventListener('error', async () => {
    if (!video.error || !video.hasAttribute('src') || !panel.open) return;
    const version = streamVersion, code = video.error.code;
    if (transport === 'hls' || code === 2) {
      await heartbeat(true);
      if (version !== streamVersion || !panel.open || recoveryPromise || !sessionAvailable) return;
    }
    if ([3, 4].includes(code) && currentIndex && fallbackPlayback(currentIndex, lastPosition, desiredPlayback)) return;
    if (streamController) streamController.abort();
    showError(new Error('浏览器播放失败，请检查网络或重试（错误 ' + code + '）'));
  });
  video.addEventListener('ended', () => {
    if (loading || !panel.open || errorText.textContent) return;
    historyCompleted = true;
    completedEpisodes.add(currentIndex);
    updateEpisodeControls();
    saveHistory(true);
    if (node('autoNextEpisode').checked && currentIndex < episodes.length) playEpisode(currentIndex + 1);
    else statusText.textContent = '本集播放完毕';
  });
  node('previousEpisodeBtn').addEventListener('click', () => playEpisode(currentIndex - 1));
  node('nextEpisodeBtn').addEventListener('click', () => playEpisode(currentIndex + 1));
  node('retryPlaybackBtn').addEventListener('click', () => {
    preparedIndex = 0;
    recoveryAttempts = [];
    recoveryBlocked = false;
    if (episodes.length && sessionAvailable) playEpisode(currentIndex || 1, lastPosition);
    else if (episodes.length) recoverPlayback(currentIndex || 1, lastPosition, true);
    else reopen(currentIndex || 1, lastPosition);
  });
  node('playbackRate').addEventListener('change', () => {video.playbackRate = Number(node('playbackRate').value) || 1;});
  qualitySelect.addEventListener('change', () => {
    quality = Number(qualitySelect.value) || 0;
    try {localStorage.setItem('juku.playback.quality', String(quality));} catch (_) {}
    if (currentIndex) playEpisode(currentIndex, !loading && Number.isFinite(video.currentTime) ? video.currentTime : lastPosition, !video.paused);
  });
  node('closePlayerBtn').addEventListener('click', () => panel.close());
  panel.addEventListener('close', () => {
    if (window.JukuExitWebFullscreen) window.JukuExitWebFullscreen();
  });
  panel.addEventListener('close', () => {if (!panel.open) dispose();});
  document.addEventListener('visibilitychange', () => {if (document.hidden) saveHistory(true); else wakePlayback();});
  window.addEventListener('pagehide', () => {lastPosition = recoveryPosition(); dispose(true);});
  for (const type of ['pageshow', 'online']) window.addEventListener(type, wakePlayback);
  video.addEventListener('play', () => {if (!loading) wakePlayback();});

  function showEpisodePanel() {
    renderEpisodes(Math.floor(Math.max(0, currentIndex - 1) / 30));
    if (mobile.active()) {
      node('playerPreferences').hidden = true;
      panel.classList.remove('preferences-open');
      node('playerPreferencesBtn').setAttribute('aria-expanded', 'false');
      panel.classList.add('episodes-open');
      node('toggleEpisodesBtn').setAttribute('aria-expanded', 'true');
      window.JukuDialogs.layer('episodes', true);
      node('closeEpisodesBtn').focus();
    } else node('episodeRange').focus();
  }

  function closeEpisodePanel() {
    if (!panel.classList.contains('episodes-open')) return;
    panel.classList.remove('episodes-open');
    node('toggleEpisodesBtn').setAttribute('aria-expanded', 'false');
    window.JukuDialogs.layer('episodes', false);
    (mobile.active() ? node('mobileEpisodesBtn') : node('toggleEpisodesBtn')).focus({preventScroll: true});
  }

  node('toggleEpisodesBtn').addEventListener('click', showEpisodePanel);
  node('closeEpisodesBtn').addEventListener('click', closeEpisodePanel);
  node('episodeRange').addEventListener('change', () => renderEpisodes(Number(node('episodeRange').value) || 0));
  node('currentEpisodeBtn').addEventListener('click', () => {
    renderEpisodes(Math.floor(Math.max(0, currentIndex - 1) / 30));
    episodeButtons.find(button => Number(button.dataset.episodeIndex) === currentIndex)?.focus();
  });
  node('episodeJumpForm').addEventListener('submit', event => {
    event.preventDefault();
    const index = Number(node('jumpEpisode').value);
    if (!Number.isInteger(index) || index < 1 || index > episodes.length) {
      node('jumpEpisode').setCustomValidity('请输入 1 至 ' + episodes.length + ' 之间的集号');
      node('jumpEpisode').reportValidity();
      return;
    }
    node('jumpEpisode').setCustomValidity('');
    playEpisode(index);
    closeEpisodePanel();
  });
  node('jumpEpisode').addEventListener('input', () => node('jumpEpisode').setCustomValidity(''));
  function setPlayerPreferences(visible, section = 'settings') {
    controls.reset();
    node('playerPreferences').hidden = !visible;
    if (visible) panel.dataset.playerSheet = section;
    panel.classList.toggle('preferences-open', visible);
    node('playerPreferencesBtn').setAttribute('aria-expanded', String(visible));
    if (mobile.active()) {
      if (visible) {
        panel.classList.remove('episodes-open');
        node('toggleEpisodesBtn').setAttribute('aria-expanded', 'false');
      }
      window.JukuDialogs.layer('player-settings', visible);
      if (visible) node('closePlayerPreferencesBtn').focus({preventScroll: true});
    } else if (visible) node('playerPreferences').scrollIntoView({block: 'nearest'});
    mobile.update();
  }
  node('playerPreferencesBtn').addEventListener('click', () => setPlayerPreferences(node('playerPreferences').hidden));
  node('closePlayerPreferencesBtn').addEventListener('click', () => {setPlayerPreferences(false); node('mobilePlayerSettingsBtn').focus({preventScroll: true});});
  node('playerSheetBackdrop').addEventListener('click', () => {
    if (panel.classList.contains('episodes-open')) closeEpisodePanel();
    else setPlayerPreferences(false);
  });
  document.addEventListener('dialoglayerchange', event => {
    const preferences = event.detail === 'player-settings' && panel.open;
    node('playerPreferences').hidden = !preferences;
    panel.classList.toggle('preferences-open', preferences);
    node('playerPreferencesBtn').setAttribute('aria-expanded', String(preferences));
    if (event.detail !== 'episodes') {
      const wasOpen = panel.classList.contains('episodes-open');
      panel.classList.remove('episodes-open');
      node('toggleEpisodesBtn').setAttribute('aria-expanded', 'false');
      if (wasOpen && panel.open) node('toggleEpisodesBtn').focus({preventScroll: true});
    } else if (panel.open) {
      panel.classList.add('episodes-open');
      node('toggleEpisodesBtn').setAttribute('aria-expanded', 'true');
    }
  });
  panel.addEventListener('close', () => {
    panel.classList.remove('episodes-open', 'preferences-open');
    node('playerPreferences').hidden = true;
    node('playerPreferencesBtn').setAttribute('aria-expanded', 'false');
    node('toggleEpisodesBtn').setAttribute('aria-expanded', 'false');
  });
  try {
    node('autoNextEpisode').checked = localStorage.getItem('duanju.playback.autoNext') !== 'false';
    const rate = localStorage.getItem('duanju.playback.rate');
    if (Array.from(node('playbackRate').options).some(option => option.value === rate)) node('playbackRate').value = rate;
  } catch (_) {}
  node('autoNextEpisode').addEventListener('change', () => {try {localStorage.setItem('duanju.playback.autoNext', String(node('autoNextEpisode').checked));} catch (_) {}});
  node('playbackRate').addEventListener('change', () => {try {localStorage.setItem('duanju.playback.rate', node('playbackRate').value);} catch (_) {}});
  const pip = node('pictureInPictureBtn');
  pip.hidden = !document.pictureInPictureEnabled || typeof video.requestPictureInPicture !== 'function';
  pip.title = '画中画展示视频，网页弹幕留在当前页面';
  pip.addEventListener('click', async () => {
    try {
      if (document.pictureInPictureElement === video) await document.exitPictureInPicture();
      else await video.requestPictureInPicture();
    } catch (_) {statusText.textContent = '画中画暂不可用，请先开始播放。';}
  });
  document.addEventListener('visibilitychange', schedulePrefetchStatus);

  window.dramaPlayer = {open, openCollection, openHistory, updateDependency};
})();

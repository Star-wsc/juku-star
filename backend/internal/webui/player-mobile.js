(() => {
  window.JukuPlayerMobile = ({panel, video, state, showEpisodes, showPreferences}) => {
    const node = id => document.getElementById(id);
    const mode = window.matchMedia('(max-width:700px), (max-height:500px) and (pointer:coarse)');
    const progress = node('mobilePlaybackProgress');
    const play = node('mobilePlayPauseBtn');
    const transport = node('mobileTransportBtn');
    const rate = node('playbackRate'), quality = node('playbackQuality');
    const orientation = window.JukuPlayerOrientation({panel, video, active: () => mode.matches && panel.open});
    const heading = panel.querySelector('.mobile-player-heading'), caption = panel.querySelector('.mobile-player-caption');
    let scrubbing = false, qualitySignature = '', lastTitle = '', waiting = false, hideTimer = null, previousLandscape = false;
    const time = value => {const seconds = Math.max(0, Math.floor(Number(value) || 0)); return Math.floor(seconds / 60) + ':' + String(seconds % 60).padStart(2, '0');};
    const duration = () => {const value = state().duration || video.duration; return Number.isFinite(value) && value > 0 ? value : 0;};
    function hideControls(hidden) {
      panel.classList.toggle('player-controls-hidden', hidden);
      heading.inert = hidden;
      caption.inert = hidden;
    }
    function reveal() {
      clearTimeout(hideTimer);
      hideControls(false);
      if (!orientation.landscape() || video.paused || waiting || state().loading || scrubbing || panel.classList.contains('episodes-open') || panel.classList.contains('preferences-open') || node('playbackError').textContent) return;
      hideTimer = setTimeout(() => {if (panel.open && orientation.landscape() && !video.paused && !scrubbing) hideControls(true);}, 3000);
    }
    function choices(select, container) {
      container.replaceChildren();
      for (const option of select.options) {
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'secondary';
        button.textContent = option.textContent;
        button.dataset.value = option.value;
        button.setAttribute('aria-pressed', String(option.value === select.value));
        button.addEventListener('click', () => {
          select.value = option.value;
          select.dispatchEvent(new Event('change'));
          update();
          if (panel.dataset.playerSheet !== 'settings') node('closePlayerPreferencesBtn').click();
        });
        container.appendChild(button);
      }
    }
    function updateProgress() {
      if (!mode.matches || !panel.open) return;
      const total = duration();
      progress.max = String(total || 1);
      progress.disabled = !total || state().loading;
      if (!scrubbing) progress.value = String(Math.min(total, Math.max(0, Number(video.currentTime) || 0)));
      const position = scrubbing ? Number(progress.value) : Number(video.currentTime) || 0;
      const text = time(position) + ' / ' + time(total);
      node('mobilePlaybackTime').textContent = text;
      node('mobileCurrentTime').textContent = time(position);
      node('mobileDuration').textContent = time(total);
      progress.setAttribute('aria-valuetext', text);
      progress.style.setProperty('--played', (total ? position / total * 100 : 0) + '%');
    }
    function update() {
      const value = state();
      panel.classList.toggle('mobile-player', mode.matches);
      video.controls = !mode.matches;
      orientation.update();
      if (!mode.matches) {hideControls(false); clearTimeout(hideTimer); return;}
      panel.classList.toggle('playback-loading', Boolean(value.loading));
      panel.classList.toggle('playback-buffering', waiting);
      if (lastTitle !== value.title) {node('mobilePlayerTitle').textContent = node('mobileHeadingTitle').textContent = value.title || '正在加载'; lastTitle = value.title;}
      node('mobileEpisodeLabel').textContent = value.episode ? '第 ' + value.episode + ' 集' : '正在加载';
      node('mobileEpisodeSummary').textContent = [value.releaseStatus === 'finished' ? '已完结' : value.releaseStatus === 'ongoing' ? '连载中' : '', value.total ? '全 ' + value.total + ' 集' : '获取分集中'].filter(Boolean).join(' · ');
      node('mobileEpisodeVIP').hidden = !value.vip;
      node('mobileRateBtn').textContent = node('mobileLandscapeRateBtn').textContent = video.playbackRate === 1 ? '倍速' : (video.playbackRate || 1) + ' 倍';
      const selectedQuality = quality.selectedOptions[0]?.textContent || '自动';
      node('mobileLandscapeQualityBtn').textContent = selectedQuality.match(/\d+p/i)?.[0].toUpperCase() || '自动';
      node('mobileLandscapeQualityBtn').disabled = quality.disabled;
      node('mobileEpisodesBtn').disabled = !value.total;
      node('mobileNextEpisodeBtn').disabled = node('nextEpisodeBtn').disabled;
      panel.dataset.paused = String(video.paused);
      play.hidden = !video.paused || Boolean(node('playbackError').textContent);
      play.disabled = Boolean(value.loading) || !video.hasAttribute('src');
      play.setAttribute('aria-label', video.ended ? '重播本集' : '播放');
      transport.disabled = play.disabled;
      transport.setAttribute('aria-label', video.paused ? video.ended ? '重播本集' : '播放' : '暂停');
      const signature = Array.from(quality.options, item => item.value + ':' + item.textContent).join('|');
      if (signature !== qualitySignature) {qualitySignature = signature; choices(quality, node('mobileQualityChoices'));}
      for (const [select, target] of [[rate, 'mobileRateChoices'], [quality, 'mobileQualityChoices']]) {
        for (const button of node(target).children) {
          button.setAttribute('aria-pressed', String(button.dataset.value === select.value));
          button.disabled = select.disabled;
        }
      }
      node('mobileFullscreenBtn').hidden = node('playerFullscreenBtn').hidden;
      node('mobileFullscreenBtn').textContent = document.fullscreenElement === panel || document.webkitFullscreenElement === panel ? '退出全屏' : '全屏播放';
      node('mobilePreferencesTitle').textContent = panel.dataset.playerSheet === 'rate' ? '倍速' : panel.dataset.playerSheet === 'quality' ? '清晰度' : '更多';
      if (previousLandscape !== orientation.landscape() || video.paused || value.loading || waiting || panel.classList.contains('episodes-open') || panel.classList.contains('preferences-open')) reveal();
      previousLandscape = orientation.landscape();
      updateProgress();
    }
    choices(rate, node('mobileRateChoices'));
    node('mobileBackBtn').addEventListener('click', () => {
      if (orientation.landscape()) {
        orientation.portrait();
        if (document.fullscreenElement === panel) document.exitFullscreen().catch(() => {});
        else if (document.webkitFullscreenElement === panel) document.webkitExitFullscreen?.();
      } else panel.close();
    });
    node('mobileEpisodesBtn').addEventListener('click', showEpisodes);
    node('mobilePlayerSettingsBtn').addEventListener('click', () => showPreferences('settings'));
    for (const id of ['mobileRateBtn', 'mobileLandscapeRateBtn']) node(id).addEventListener('click', () => showPreferences('rate'));
    node('mobileLandscapeQualityBtn').addEventListener('click', () => showPreferences('quality'));
    node('mobileNextEpisodeBtn').addEventListener('click', () => node('nextEpisodeBtn').click());
    node('mobileFullscreenBtn').addEventListener('click', async () => {
      try {
        if (document.fullscreenElement === panel || document.webkitFullscreenElement === panel) await (document.exitFullscreen?.() || document.webkitExitFullscreen?.());
        else if (panel.requestFullscreen) await panel.requestFullscreen();
        else if (panel.webkitRequestFullscreen) await panel.webkitRequestFullscreen();
        else node('playerFullscreenBtn').click();
      } catch (_) {}
      update();
    });
    function togglePlayback() {
      if (!mode.matches || !panel.open || state().loading || !video.hasAttribute('src')) return;
      if (video.paused) video.play().catch(() => {}); else video.pause();
    }
    play.addEventListener('click', togglePlayback);
    transport.addEventListener('click', togglePlayback);
    video.addEventListener('click', event => {
      if (event.defaultPrevented) return;
      if (orientation.landscape()) {
        if (panel.classList.contains('player-controls-hidden') || video.paused) reveal();
        else {clearTimeout(hideTimer); hideControls(true);}
      } else togglePlayback();
    });
    progress.addEventListener('input', () => {scrubbing = true; reveal(); updateProgress();});
    progress.addEventListener('change', () => {
      const target = Math.min(duration(), Math.max(0, Number(progress.value) || 0));
      scrubbing = false;
      if (!state().loading && duration()) video.currentTime = target;
      reveal();
      updateProgress();
    });
    progress.addEventListener('pointercancel', () => {scrubbing = false; updateProgress();});
    progress.addEventListener('blur', () => {scrubbing = false; updateProgress();});
    video.addEventListener('waiting', () => {waiting = true; update();});
    for (const type of ['playing', 'canplay', 'emptied']) video.addEventListener(type, () => {waiting = false; update(); reveal();});
    panel.addEventListener('jukuorientationchange', () => {update(); reveal();});
    panel.addEventListener('keydown', reveal);
    panel.addEventListener('pointerdown', event => {if (event.target.closest('button,input,select')) reveal();});
    document.addEventListener('dialoglayerchange', () => {update(); reveal();});
    document.addEventListener('fullscreenchange', update);
    for (const type of ['timeupdate', 'durationchange']) video.addEventListener(type, updateProgress);
    for (const type of ['loadedmetadata', 'playing', 'pause', 'ended', 'ratechange', 'emptied', 'error']) video.addEventListener(type, update);
    rate.addEventListener('change', update);
    quality.addEventListener('change', update);
    mode.addEventListener('change', update);
    panel.addEventListener('close', () => {
      scrubbing = false;
      clearTimeout(hideTimer);
      hideControls(false);
      if (document.fullscreenElement === panel) document.exitFullscreen().catch(() => {});
      else if (document.webkitFullscreenElement === panel) document.webkitExitFullscreen?.();
    });
    update();
    return {update, active: () => mode.matches, rotation: () => orientation.rotation()};
  };
})();

(() => {
  window.JukuPlayerOrientation = ({panel, video, active}) => {
    const node = id => document.getElementById(id);
    const rotate = node('mobileRotateBtn'), automatic = node('mobileAutoRotate');
    const watch = node('mobileLandscapeBtn');
    const permission = node('mobileOrientationPermissionBtn'), hint = node('mobileOrientationHint');
    let manual = null, sensed = null, sensedTurn = 90, angle = 0, landscape = false;
    let previousViewport = null, sensorKind = null, candidate = '', timer = null, listening = false, granted = false, signature = '';
    let enabled = true;
    try {enabled = localStorage.getItem('juku.playback.autoRotate') !== 'false';} catch (_) {}
    automatic.checked = enabled;

    function sensorAvailable() {return window.isSecureContext && typeof window.DeviceOrientationEvent !== 'undefined';}
    function permissionNeeded() {return sensorAvailable() && typeof window.DeviceOrientationEvent.requestPermission === 'function' && !granted;}
    function clearCandidate() {clearTimeout(timer); timer = null; candidate = '';}
    function stopSensor() {
      window.removeEventListener('deviceorientation', sense);
      listening = false;
      clearCandidate();
    }
    function startSensor() {
      if (!active() || !enabled || !sensorAvailable() || permissionNeeded() || listening) return;
      window.addEventListener('deviceorientation', sense, {passive: true});
      listening = true;
    }
    function sense(event) {
      if (!active() || !enabled || !Number.isFinite(event.beta) || !Number.isFinite(event.gamma)) return;
      const beta = event.beta * Math.PI / 180, gamma = event.gamma * Math.PI / 180;
      const x = Math.sin(gamma) * Math.cos(beta), y = Math.sin(beta);
      if (Math.hypot(x, y) < .65) {clearCandidate(); return;}
      let next;
      if (Math.abs(x) > Math.abs(y) * 1.4) next = true;
      else if (Math.abs(y) > Math.abs(x) * 1.4) next = false;
      else {clearCandidate(); return;}
      const turn = x > 0 ? -90 : 90, key = String(next) + ':' + (next ? turn : 0);
      if (key === sensorKind) {clearCandidate(); return;}
      if (manual !== null && sensorKind === null) {sensorKind = key; clearCandidate(); return;}
      if (key === candidate) return;
      clearCandidate();
      candidate = key;
      timer = setTimeout(() => {
        timer = null;
        candidate = '';
        if (!active() || !enabled) return;
        sensorKind = key;
        sensed = next;
        sensedTurn = turn;
        manual = null;
        update();
      }, 300);
    }
    function update() {
      const on = active(), width = panel.clientWidth || window.innerWidth, height = panel.clientHeight || window.innerHeight;
      const viewportLandscape = width > height;
      const videoWidth = video.videoWidth, videoHeight = video.videoHeight;
      const known = videoWidth > 0 && videoHeight > 0;
      const previousLandscape = landscape;
      landscape = on && (manual ?? (enabled && sensed !== null ? sensed : viewportLandscape));
      const nextAngle = !on || landscape === viewportLandscape ? 0 : landscape ? sensedTurn : -90;
      const viewWidth = nextAngle ? height : width, viewHeight = nextAngle ? width : height;
      const nextSignature = [on, width, height, videoWidth, videoHeight, landscape, nextAngle].join(':');
      if (signature !== nextSignature) {
        signature = nextSignature;
        panel.dataset.videoLayout = known ? videoWidth > videoHeight ? 'landscape' : videoWidth < videoHeight ? 'portrait' : 'square' : 'unknown';
        node('mobileVideoSize').textContent = known ? videoWidth + ' × ' + videoHeight + ' · ' + (videoWidth > videoHeight ? '横屏视频' : videoWidth < videoHeight ? '竖屏视频' : '方形视频') : '读取视频尺寸…';
        panel.classList.toggle('player-rotated', nextAngle !== 0);
        panel.classList.toggle('player-landscape', landscape);
        for (const [key, value] of Object.entries({'frame-width': width, 'frame-height': height, 'view-height': viewHeight, 'view-width': viewWidth})) panel.style.setProperty('--player-' + key, value + 'px');
        panel.style.setProperty('--player-rotation', nextAngle + 'deg');
        if (known) panel.style.setProperty('--landscape-watch-top', Math.max(70, Math.min(viewHeight - 190, (viewHeight + Math.min(viewHeight, viewWidth * videoHeight / videoWidth)) / 2 + 18)) + 'px');
      }
      rotate.disabled = !known;
      watch.hidden = !on || !known || videoWidth <= videoHeight || landscape;
      rotate.setAttribute('aria-pressed', String(landscape));
      rotate.setAttribute('aria-label', landscape ? '竖屏播放' : '横屏播放');
      rotate.title = landscape ? '切换为竖屏播放' : '切换为横屏播放';
      permission.hidden = !on || !enabled || !permissionNeeded();
      node('mobileBackBtn').setAttribute('aria-label', landscape ? '返回竖屏播放' : '返回剧库');
      if (nextAngle !== angle || previousLandscape !== landscape) {
        angle = nextAngle;
        panel.dispatchEvent(new Event('jukuorientationchange'));
      }
      if (on && enabled) startSensor(); else stopSensor();
      if (on && previousViewport === null) previousViewport = viewportLandscape;
    }
    function viewportChanged() {
      if (!active()) return;
      const next = (panel.clientWidth || window.innerWidth) > (panel.clientHeight || window.innerHeight);
      if (previousViewport !== null && next !== previousViewport) {
        if (enabled) manual = null;
        sensed = null;
        sensorKind = null;
        clearCandidate();
      }
      previousViewport = next;
      update();
    }
    async function allowSensor() {
      if (!permissionNeeded()) {startSensor(); return;}
      permission.disabled = true;
      try {
        granted = await window.DeviceOrientationEvent.requestPermission() === 'granted';
        hint.textContent = granted ? '已开启方向感应，也可随时手动旋转。' : '方向感应未开启，仍可手动旋转或跟随屏幕方向。';
        startSensor();
      } catch (_) {hint.textContent = '当前浏览器未开放方向感应，可手动旋转或跟随屏幕方向。';}
      finally {permission.disabled = false; update();}
    }
    function setLandscape(value) {
      if (!active()) return;
      manual = value;
      clearCandidate();
      update();
      panel.dispatchEvent(new Event('jukuorientationchange'));
    }
    rotate.addEventListener('click', () => {if (!rotate.disabled) setLandscape(!landscape);});
    watch.addEventListener('click', () => {
      setLandscape(true);
      try {
        const request = panel.requestFullscreen?.() || panel.webkitRequestFullscreen?.();
        request?.catch?.(() => {});
      } catch (_) {}
    });
    automatic.addEventListener('change', () => {
      enabled = automatic.checked;
      manual = enabled ? null : landscape;
      sensed = null;
      sensorKind = null;
      try {localStorage.setItem('juku.playback.autoRotate', String(enabled));} catch (_) {}
      if (!enabled) stopSensor();
      update();
    });
    permission.addEventListener('click', allowSensor);
    for (const type of ['resize', 'orientationchange']) window.addEventListener(type, viewportChanged);
    window.screen?.orientation?.addEventListener?.('change', viewportChanged);
    window.visualViewport?.addEventListener('resize', viewportChanged);
    for (const type of ['loadedmetadata', 'resize', 'emptied']) video.addEventListener(type, update);
    for (const type of ['fullscreenchange', 'webkitfullscreenchange']) document.addEventListener(type, viewportChanged);
    panel.addEventListener('close', () => {
      manual = null;
      sensed = null;
      sensedTurn = 90;
      sensorKind = null;
      previousViewport = null;
      stopSensor();
      update();
    });
    if (!sensorAvailable()) hint.textContent = '可跟随屏幕方向旋转，也可手动旋转；方向感应需 HTTPS 和浏览器支持。';
    return {update, rotation: () => angle, landscape: () => landscape, portrait: () => setLandscape(false)};
  };
})();

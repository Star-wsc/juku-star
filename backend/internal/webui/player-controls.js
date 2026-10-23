(() => {
  window.JukuPlayerControls = ({panel, video, ready, fullscreen, mobile = () => false, rotation = () => 0, canSwipe = () => false, swipe = () => ''}) => {
    const feedback = document.getElementById('playbackGestureStatus');
    const pressed = new Set();
    let hold = null, gesture = null, feedbackTimer = null, suppressClickUntil = 0;

    const available = () => panel.open && ready() && video.readyState >= 1 && !video.error;
    const interactive = target => target !== video && (target?.isContentEditable || target?.closest?.('input,select,textarea,button,a[href],summary,[contenteditable]:not([contenteditable=false]),[role=slider],[role=combobox],[role=textbox],[role=button],[role=tab],[role=menuitem]'));
    const keyName = event => event.code === 'Space' || event.key === ' ' ? 'Space' : event.key;
    const coordinates = event => rotation() === 90 ? {x: event.clientY, y: -event.clientX} : rotation() === -90 ? {x: -event.clientY, y: event.clientX} : {x: event.clientX, y: event.clientY};

    function hint(text, persistent = false) {
      clearTimeout(feedbackTimer);
      feedback.textContent = text;
      feedback.hidden = !text;
      if (text && !persistent) feedbackTimer = setTimeout(() => {
        if (hold?.active) hint('3 倍速 · 松开恢复', true);
        else feedback.hidden = true;
      }, 1100);
    }

    function endHold(tap = false, restore = true, silent = false) {
      const previous = hold;
      if (!previous) return;
      hold = null;
      clearTimeout(previous.timer);
      if (previous.source === 'pointer') {
        if (previous.active) suppressClickUntil = performance.now() + 600;
        if (!gesture || gesture.id !== previous.id) {try {if (video.hasPointerCapture(previous.id)) video.releasePointerCapture(previous.id);} catch (_) {}}
      }
      if (previous.active) {
        if (restore) video.playbackRate = previous.rate;
        hint(silent ? '' : '恢复 ' + video.playbackRate + ' 倍速');
      } else if (tap && previous.source === 'keyboard') seek(5);
    }

    function reset() {
      const previous = gesture;
      gesture = null;
      if (previous) {try {if (video.hasPointerCapture(previous.id)) video.releasePointerCapture(previous.id);} catch (_) {}}
      endHold(false, true, true);
      hint('');
    }

    function startHold(source, id, point) {
      if (hold) return;
      const current = {source, id, point, active: false, rate: video.playbackRate};
      hold = current;
      current.timer = setTimeout(() => {
        if (hold !== current || !available() || video.paused || video.ended || document.hidden) return;
        current.rate = video.playbackRate;
        current.active = true;
        if (gesture) gesture.held = true;
        video.playbackRate = 3;
        hint('3 倍速 · 松开恢复', true);
      }, 350);
    }

    function seek(seconds) {
      if (!available() || !Number.isFinite(video.duration) || video.duration <= 0) return;
      const target = Math.min(video.duration, Math.max(0, video.currentTime + seconds));
      video.currentTime = target;
      const minutes = Math.floor(target / 60), remainder = String(Math.floor(target % 60)).padStart(2, '0');
      hint((seconds > 0 ? '快进至 ' : '后退至 ') + minutes + ':' + remainder);
    }

    function volume(delta) {
      const target = Math.min(1, Math.max(0, Math.round(((video.muted ? 0 : video.volume) + delta) * 100) / 100));
      video.volume = target;
      video.muted = target === 0;
      hint(Math.abs(video.volume - target) > .01 ? '请使用设备音量键调节' : target === 0 ? '已静音' : '音量 ' + Math.round(target * 100) + '%');
    }

    function consume(event) {
      event.preventDefault();
      event.stopPropagation();
    }

    document.addEventListener('keydown', event => {
      if (!panel.open || event.defaultPrevented || event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey || interactive(event.target)) return;
      const key = keyName(event);
      if (!['Space', 'ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'f', 'F'].includes(key) || !available()) return;
      consume(event);
      const repeated = event.repeat || pressed.has(key);
      pressed.add(key);
      if (key !== 'ArrowRight') endHold();
      if (key === 'Space' && !repeated) {
        if (video.paused) video.play().catch(() => {});
        else video.pause();
      } else if (key === 'ArrowRight' && !repeated) startHold('keyboard', key);
      else if (key === 'ArrowLeft') seek(-5);
      else if (key === 'ArrowUp') volume(.05);
      else if (key === 'ArrowDown') volume(-.05);
      else if (key.toLowerCase() === 'f' && !repeated) fullscreen();
    }, true);

    document.addEventListener('keyup', event => {
      const key = keyName(event);
      if (!pressed.delete(key)) return;
      consume(event);
      if (key === 'ArrowRight' && hold?.source === 'keyboard') endHold(!interactive(event.target));
    }, true);

    function cancelGesture() {
      const previous = gesture;
      gesture = null;
      if (previous) {
        suppressClickUntil = performance.now() + 600;
        try {if (video.hasPointerCapture(previous.id)) video.releasePointerCapture(previous.id);} catch (_) {}
      }
      endHold();
    }

    document.addEventListener('pointerdown', event => {
      if (!event.isPrimary && panel.open) cancelGesture();
    }, true);
    video.addEventListener('pointerdown', event => {
      if (!event.isPrimary) {cancelGesture(); return;}
      if (event.button !== 0 || !panel.open || hold || gesture) return;
      const point = coordinates(event);
      const vertical = mobile() && event.pointerType === 'touch' && canSwipe();
      if (vertical) gesture = {id: event.pointerId, point, started: performance.now(), axis: '', moved: false, held: false};
      const bounds = video.getBoundingClientRect();
      if (available() && !video.paused && !video.ended && (mobile() || event.clientY < bounds.bottom - Math.min(80, bounds.height * .5))) startHold('pointer', event.pointerId, point);
      if (gesture || hold) {try {video.setPointerCapture(event.pointerId);} catch (_) {}}
    }, true);

    document.addEventListener('pointermove', event => {
      const point = coordinates(event);
      if (gesture?.id === event.pointerId) {
        const x = Math.abs(point.x - gesture.point.x), y = Math.abs(point.y - gesture.point.y);
        if (Math.hypot(x, y) > 12) {
          gesture.moved = true;
          if (!gesture.axis) gesture.axis = y > x * 1.3 ? 'vertical' : 'horizontal';
          if (gesture.axis === 'vertical') event.preventDefault();
          endHold();
        }
      } else if (hold?.source === 'pointer' && hold.id === event.pointerId && Math.hypot(point.x - hold.point.x, point.y - hold.point.y) > 12) endHold();
    }, {capture: true, passive: false});

    document.addEventListener('pointerup', event => {
      const current = gesture?.id === event.pointerId ? gesture : null;
      if (current) gesture = null;
      if (hold?.source === 'pointer' && hold.id === event.pointerId) {
        if (hold.active) consume(event);
        endHold();
      }
      if (!current) return;
      try {if (video.hasPointerCapture(current.id)) video.releasePointerCapture(current.id);} catch (_) {}
      if (current.moved || current.held) {suppressClickUntil = performance.now() + 600; consume(event);}
      const point = coordinates(event);
      const x = Math.abs(point.x - current.point.x), y = point.y - current.point.y;
      const threshold = Math.max(56, Math.min(100, video.clientHeight * .1));
      if (!current.held && current.axis === 'vertical' && Math.abs(y) >= threshold && Math.abs(y) > x * 1.5 && performance.now() - current.started < 1500 && canSwipe()) {
        consume(event);
        hint(swipe(y < 0 ? 1 : -1));
      }
    }, true);

    for (const type of ['pointercancel', 'lostpointercapture']) video.addEventListener(type, event => {
      if (gesture?.id === event.pointerId || hold?.source === 'pointer' && hold.id === event.pointerId) cancelGesture();
    });

    video.addEventListener('click', event => {
      if (performance.now() < suppressClickUntil) consume(event);
    }, true);
    video.addEventListener('contextmenu', event => {
      if (hold?.source === 'pointer' || performance.now() < suppressClickUntil) consume(event);
    });
    video.addEventListener('dragstart', event => {if (hold?.source === 'pointer') consume(event);});
    video.addEventListener('ratechange', () => {
      if (hold?.active && video.playbackRate !== 3) endHold(false, false);
    });
    for (const type of ['pause', 'ended', 'emptied', 'error']) video.addEventListener(type, reset);
    document.addEventListener('focusin', event => {if (interactive(event.target)) reset();});
    document.addEventListener('visibilitychange', () => {if (document.hidden) {reset(); pressed.clear();}});
    document.addEventListener('fullscreenchange', reset);
    document.addEventListener('webkitfullscreenchange', reset);
    window.addEventListener('blur', () => {reset(); pressed.clear();});
    window.addEventListener('pagehide', reset);
    panel.addEventListener('close', reset);
    panel.addEventListener('jukuorientationchange', reset);
    return {reset};
  };
})();

(() => {
  function createPlaybackCapabilities(environment, video, agent) {
    const touchApple = /iPad|iPhone|iPod/i.test(agent.userAgent || '') || /Mac/i.test(agent.platform || '') && Number(agent.maxTouchPoints) > 1;
    function native() {
      if (touchApple) return true;
      try {return ['application/vnd.apple.mpegurl', 'application/x-mpegURL'].some(type => ['maybe', 'probably'].includes(video.canPlayType(type)));}
      catch (_) {return false;}
    }
    function mse(type) {
      try {return typeof type === 'string' && Boolean(type) && typeof environment.MediaSource === 'function' && typeof environment.MediaSource.isTypeSupported === 'function' && environment.MediaSource.isTypeSupported(type);}
      catch (_) {return false;}
    }
    function choose(type) {
      if (touchApple && native()) return 'hls';
      if (mse(type)) return 'mse';
      return native() ? 'hls' : '';
    }
    function remux() {
      return !touchApple && ['42E02A', '4D002A', '64002A'].every(profile => mse('video/mp4; codecs="avc1.' + profile + ', mp4a.40.2"'));
    }
    return {native, mse, choose, remux};
  }
  if (typeof module !== 'undefined' && module.exports) module.exports = createPlaybackCapabilities;
  else window.JukuPlaybackCapabilities = createPlaybackCapabilities;
})();

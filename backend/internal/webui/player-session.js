(() => {
  function playbackSession({request, onState, onExpired, onError, interval = 20000, timeout = 8000, retry = 5000}) {
    let session = '', timer = null, pending = null, ended = false;
    function stop() {
      session = '';
      ended = false;
      clearTimeout(timer);
      timer = null;
      const previous = pending;
      pending = null;
      previous?.controller.abort();
    }
    function schedule(delay) {
      clearTimeout(timer);
      if (session && !ended) timer = setTimeout(() => ping(), delay);
    }
    function ping(fresh = false) {
      if (!session || ended) return Promise.resolve(null);
      if (pending && !fresh) return pending.promise;
      if (pending) {
        const previous = pending;
        pending = null;
        previous.controller.abort();
      }
      clearTimeout(timer);
      const call = {session, controller: new AbortController()};
      pending = call;
      call.promise = (async () => {
        let deadline, delay = interval;
        try {
          const expiry = new Promise((_, reject) => {
            deadline = setTimeout(() => {call.controller.abort(); reject(new Error('播放保活请求超时'));}, timeout);
          });
          const state = await Promise.race([Promise.resolve().then(() => request(call.session, call.controller.signal)), expiry]);
          if (pending !== call || session !== call.session) return null;
          onState(state);
          return state;
        } catch (error) {
          if (pending !== call || session !== call.session) return null;
          if (error.status === 410) {
            ended = true;
            onExpired(error);
          } else if (error.status === 401 || error.status === 403) {
            ended = true;
            onError(error);
          } else delay = retry;
          return null;
        } finally {
          clearTimeout(deadline);
          if (pending === call) {pending = null; schedule(delay);}
        }
      })();
      return call.promise;
    }
    function start(id) {stop(); session = id; schedule(interval);}
    return {start, stop, ping};
  }
  if (typeof module === 'object' && module.exports) module.exports = playbackSession;
  else window.JukuPlaybackSession = playbackSession;
})();

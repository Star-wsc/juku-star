export function createDramaRefresh({post, apply, failure, now = Date.now}) {
  const pending = new Map(), retryAt = new Map(), latest = new Map();
  const checkedAt = drama => Date.parse(drama?.sortMetadata?.checkedAt) || 0;

  function reconcile(drama) {
    const recent = latest.get(drama.id);
    if (!recent) return drama;
    if (now() >= recent.expires || checkedAt(drama) > checkedAt(recent.drama)) {
      latest.delete(drama.id);
      return drama;
    }
    return {...drama, ...recent.drama};
  }

  function refresh(id) {
    if (pending.has(id)) return pending.get(id);
    if (now() < (retryAt.get(id) || 0)) return Promise.resolve();
    retryAt.set(id, now() + 300000);
    const work = (async () => {
      await Promise.resolve();
      try {
        const result = await post('/api/ui/dramas/refresh', {dramaId: id});
        if (result.dramaId !== id || result.drama?.id !== id) throw new Error('剧集资料响应不匹配');
        const expires = now() + Math.max(1, Number(result.retryAfter) || 300) * 1000;
        retryAt.set(id, expires);
        if (latest.size >= 256 && !latest.has(id)) latest.delete(latest.keys().next().value);
        latest.set(id, {drama: result.drama, expires});
        apply(result.drama, result.warning || '');
      } catch (error) {
        retryAt.set(id, now() + 30000);
        failure?.(id, error);
      } finally {pending.delete(id);}
    })();
    pending.set(id, work);
    return work;
  }

  return {refresh, reconcile};
}

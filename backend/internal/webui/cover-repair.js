import { coverURL } from './ui-core.js';

export function createCoverRepair({get, post, apply}) {
  const failures = new Map(), touched = new Map(), pending = new Set(), retryAt = new Map(), repaired = new Map();

  async function repair(id) {
    const drama = get(id);
    if (!drama) return;
    const now = Date.now(), observed = coverURL(drama);
    if (observed && failures.get(id) !== observed || pending.has(id) || now < (retryAt.get(id) || 0)) return;
    pending.add(id);
    retryAt.set(id, now + 300000);
    try {
      const result = await post('/api/ui/cover/repair', {dramaId: id, cover: observed});
      retryAt.set(id, Date.now() + Math.max(300, Number(result.retryAfter) || 0) * 1000);
      if (result.dramaId !== id || typeof result.cover !== 'string' || !result.cover.startsWith('/api/ui/image?') || !get(id) || coverURL(get(id)) !== observed) return;
      failures.delete(id);
      if (result.cover !== observed) repaired.set(id, {previous: observed, address: result.cover, expires: retryAt.get(id)});
      apply(id, result.cover, observed);
    } catch {}
    finally {pending.delete(id);}
  }

  return {
    updated(id) {failures.delete(id); retryAt.set(id, Date.now() + 300000); touched.set(id, Date.now());},
    reconcile(drama) {
      const latest = repaired.get(drama.id);
      if (!latest) return drama;
      if (Date.now() >= latest.expires) {repaired.delete(drama.id); return drama;}
      if (coverURL(drama) === latest.previous) {drama.cover = latest.address; drama.coverUrl = latest.address;}
      return drama;
    },
    touch(id) {touched.set(id, Date.now()); void repair(id);},
    failed(id, address) {
      if (!get(id) || coverURL(get(id)) !== address) return;
      failures.set(id, address);
      if (Date.now() - (touched.get(id) || 0) < 60000) void repair(id);
    },
    loaded(id, address) {if (failures.get(id) === address) failures.delete(id);}
  };
}

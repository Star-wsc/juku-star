import { element, reconcileChildren, withFocus } from './ui-core.js';

export function createCardViewport({container, scroller, create, update, key, rendered}) {
  let items = [], emptyNode = null, frame = 0, revision = 0, drawnRevision = -1;
  let columns = 1, rowHeight = 0, measuredWidth = 0, first = -1, last = -1;
  let mounted = new Map();
  const before = element('div', 'cards-spacer'), after = element('div', 'cards-spacer');
  before.setAttribute('aria-hidden', 'true');
  after.setAttribute('aria-hidden', 'true');
  container.setAttribute('role', 'list');

  function schedule() {
    if (!frame) frame = requestAnimationFrame(() => {frame = 0; draw();});
  }

  function draw() {
    const width = container.clientWidth;
    if (!width || !scroller.clientHeight) return;
    const style = getComputedStyle(container);
    const tracks = style.gridTemplateColumns.split(/\s+/).filter(value => /^\d/.test(value));
    const nextColumns = Math.max(1, tracks.length);
    if (width !== measuredWidth || columns !== nextColumns) {
      measuredWidth = width;
      columns = nextColumns;
      rowHeight = 0;
      first = -1;
    }
    const gap = parseFloat(style.rowGap) || 0;
    const step = (rowHeight || (parseFloat(tracks[0]) || width) * 4 / 3 + 60) + gap;
    const top = Math.max(0, scroller.getBoundingClientRect().top - container.getBoundingClientRect().top);
    const totalRows = Math.ceil(items.length / columns);
    const startRow = Math.max(0, Math.min(Math.max(0, totalRows - 1), Math.floor(top / step) - 1));
    const endRow = Math.min(totalRows, Math.ceil((top + scroller.clientHeight) / step) + 1);
    const start = Math.min(items.length, startRow * columns), end = Math.min(items.length, endRow * columns);
    if (first === start && last === end && drawnRevision === revision) return;
    const nextMounted = new Map(), nodes = [], changed = [];
    if (startRow > 0 && items.length) {before.style.height = Math.max(0, startRow * step - gap) + 'px'; nodes.push(before);}
    for (let index = start; index < end; index++) {
      const item = items[index], renderKey = key(item);
      let card = mounted.get(item.id);
      if (!card || card.renderKey !== renderKey) {card = create(item); changed.push(card);}
      else if (drawnRevision !== revision) {update(card, item); changed.push(card);}
      card.setAttribute('role', 'listitem');
      card.setAttribute('aria-posinset', String(index + 1));
      card.setAttribute('aria-setsize', String(items.length));
      nextMounted.set(item.id, card);
      nodes.push(card);
    }
    if (endRow < totalRows) {after.style.height = Math.max(0, (totalRows - endRow) * step - gap) + 'px'; nodes.push(after);}
    if (!items.length && emptyNode) nodes.push(emptyNode);
    const active = document.activeElement?.closest?.('.card');
    if (active && container.contains(active) && !nextMounted.has(active.dataset.dramaId)) scroller.focus({preventScroll: true});
    withFocus(container, () => reconcileChildren(container, nodes));
    mounted = nextMounted;
    first = start;
    last = end;
    drawnRevision = revision;
    container.setAttribute('aria-busy', 'false');
    if (!rowHeight && mounted.size) {
      rowHeight = mounted.values().next().value.getBoundingClientRect().height;
      first = -1;
      schedule();
    }
    if (changed.length) rendered(changed);
  }

  scroller.addEventListener('scroll', schedule, {passive: true});
  window.addEventListener('resize', schedule);
  if (typeof ResizeObserver === 'function') new ResizeObserver(schedule).observe(container);
  return {
    setItems(next, placeholder = null) {items = next; emptyNode = placeholder; revision++; container.setAttribute('aria-busy', 'true'); schedule();},
    refresh() {first = -1; schedule();}
  };
}

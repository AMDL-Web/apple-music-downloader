// Pure helpers for a selection of list rows, held as a Set of job ids. Every
// function returns a new Set (or the same one when nothing changed, so a
// state setter can skip a render) and leaves its input untouched. The jobs a
// selection refers to are always looked up in the current list, so a job the
// feed removes leaves the selection and a job whose status changes is judged
// by its new status.

/** Toggles one id; `on` forces it in (true) or out (false). */
export function toggleId(selected, id, on) {
  const has = selected.has(id);
  const want = on === undefined ? !has : Boolean(on);
  if (want === has) return selected;
  const next = new Set(selected);
  if (want) next.add(id);
  else next.delete(id);
  return next;
}

/** Puts every id in `ids` in (on) or out of the selection. */
export function setIds(selected, ids, on) {
  let changed = false;
  const next = new Set(selected);
  for (const id of ids) {
    if (on && !next.has(id)) {
      next.add(id);
      changed = true;
    } else if (!on && next.has(id)) {
      next.delete(id);
      changed = true;
    }
  }
  return changed ? next : selected;
}

/** Drops ids no longer in `jobs` (deleted, or filtered out of the list). */
export function pruneSelection(selected, jobs) {
  if (selected.size === 0) return selected;
  const present = new Set(jobs.map((j) => j.id));
  let changed = false;
  const next = new Set();
  for (const id of selected) {
    if (present.has(id)) next.add(id);
    else changed = true;
  }
  return changed ? next : selected;
}

/**
 * The ids from `fromId` to `toId` inclusive, in list order (either
 * direction), for a Shift-click. Only `toId` when `fromId` is not in the list.
 */
export function rangeIds(jobs, fromId, toId) {
  const ids = jobs.map((j) => j.id);
  const to = ids.indexOf(toId);
  if (to < 0) return [];
  const from = ids.indexOf(fromId);
  if (from < 0) return [toId];
  return from <= to ? ids.slice(from, to + 1) : ids.slice(to, from + 1);
}

/** The selected jobs, in list order. */
export function selectedJobs(selected, jobs) {
  return selected.size === 0 ? [] : jobs.filter((j) => selected.has(j.id));
}

/** 'none', 'some' or 'all' of the listed jobs selected (an empty list is 'none'). */
export function selectionState(selected, jobs) {
  const n = selectedJobs(selected, jobs).length;
  if (n === 0) return 'none';
  return n === jobs.length ? 'all' : 'some';
}

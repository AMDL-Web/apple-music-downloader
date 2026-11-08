// The run order of the download queue in the console's lists: running jobs
// first (the one that started last at the bottom, so a queued job that
// starts stays where it was drawn), then the queued ones by queue_order
// (the next to start at the top), then the rest as the list has them. And
// the request a reorder sends: POST /downloads/{id}/move with
// {after: <the queued job now above it>} or {to: "first"|"last"}.

const time = (v) => {
  const t = Date.parse(v || '');
  return Number.isNaN(t) ? Infinity : t;
};

const order = (job) => (typeof job.queue_order === 'number' ? job.queue_order : Infinity);

/**
 * Whether a Queue view reads every running and queued job on its own and
 * lets the queue be reordered: All and Active with no search and no type.
 */
export function orderable(filters) {
  return Boolean(filters) && (filters.tab === 'all' || filters.tab === 'active') && !filters.q && !filters.type;
}

/** Sorts a copy of `jobs` by `key`, ties kept in their order. */
function stable(jobs, key) {
  return jobs
    .map((job, i) => ({ job, i, k: key(job) }))
    .sort((a, b) => (a.k === b.k ? a.i - b.i : a.k < b.k ? -1 : 1))
    .map((x) => x.job);
}

/** The queued jobs among `jobs`, in run order. */
export function upNext(jobs) {
  return stable(
    jobs.filter((j) => j && j.status === 'queued'),
    order,
  );
}

/**
 * The rows of a list in run order: `main` (a page of the list, newest
 * first) and `queue` (the queued jobs read on their own, which may reach
 * past the pages loaded). A job in both counts once, in its newer version
 * (by updated_at; the queue's on a tie). Running jobs come first by
 * started_at, then the queued ones by queue_order, then the rest in `main`'s
 * order.
 */
export function arrange(main, queue = []) {
  const latest = new Map();
  for (const job of [...main, ...queue]) {
    const held = latest.get(job.id);
    if (!held || time(job.updated_at) >= time(held.updated_at)) latest.set(job.id, job);
  }
  const seen = new Set();
  const all = [];
  for (const job of [...main, ...queue]) {
    if (seen.has(job.id)) continue;
    seen.add(job.id);
    all.push(latest.get(job.id));
  }
  const running = stable(
    all.filter((j) => j.status === 'running'),
    (j) => time(j.started_at),
  );
  const inMain = new Set(main.map((j) => j.id));
  const rest = all.filter((j) => j.status !== 'running' && j.status !== 'queued' && inMain.has(j.id));
  return [...running, ...upNext(all), ...rest];
}

/**
 * The jobs of `queue` with `id` moved to `index` of the run order (among
 * the others): the moved job gets a queue_order between its new
 * neighbours', so sorting by it holds until the server's own value
 * arrives. Unknown ids leave the queue as it is.
 */
export function placeQueued(queue, id, index) {
  const ordered = upNext(queue);
  const job = ordered.find((j) => j.id === id);
  if (!job) return queue;
  const rest = ordered.filter((j) => j.id !== id);
  const at = Math.max(0, Math.min(index, rest.length));
  const prev = rest[at - 1];
  const next = rest[at];
  let value = order(job);
  if (prev && next) value = (order(prev) + order(next)) / 2;
  else if (prev) value = order(prev) + 1;
  else if (next) value = order(next) - 1;
  const moved = { ...job, queue_order: value };
  return queue.map((j) => (j.id === id ? moved : j));
}

/**
 * The move request that puts `id` where it is in `ids` (the queue in run
 * order after the move): {to: "first"} at the top, else {after: <the id
 * above it>}. Null when `id` is not in `ids`.
 */
export function moveBody(ids, id) {
  const i = ids.indexOf(id);
  if (i < 0) return null;
  return i === 0 ? { to: 'first' } : { after: ids[i - 1] };
}

/**
 * What a key on a focused queued row does to `id` in `ids` (the queue in run
 * order): { index, body } (its place after the move and the move request),
 * or null when the key does not move it. Alt+Up goes before the job above,
 * Alt+Down after the job below, Alt+Home first and Alt+End last; the caller
 * checks Alt.
 */
export function keyMove(ids, id, key) {
  const i = ids.indexOf(id);
  const last = ids.length - 1;
  if (i < 0) return null;
  if (key === 'ArrowUp') return i === 0 ? null : { index: i - 1, body: { before: ids[i - 1] } };
  if (key === 'ArrowDown') return i === last ? null : { index: i + 1, body: { after: ids[i + 1] } };
  if (key === 'Home') return i === 0 ? null : { index: 0, body: { to: 'first' } };
  if (key === 'End') return i === last ? null : { index: last, body: { to: 'last' } };
  return null;
}

/** Where a queued job stands: {first, last} by its place in `ids`, or {} when it is not there. */
export function queuePlace(ids, id) {
  const i = ids.indexOf(id);
  return i < 0 ? {} : { first: i === 0, last: i === ids.length - 1 };
}

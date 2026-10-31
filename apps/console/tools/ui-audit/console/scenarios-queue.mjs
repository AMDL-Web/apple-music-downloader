// The audit's Queue reordering scenarios (All and Active): the queue in run order, a row held
// mid-drag, the row menu's moves, a refused move (409) and a queued job
// that starts while the queue is shown. worldOpts.queued adds a long queue.

const QUEUED = '.job-list > .job-item[data-sortable]';

/** Presses the grip of the `n`th queued row and moves it `dy` px without letting go. */
const hold = (n, dy) => async (ctx) => {
  const row = ctx.page.locator(QUEUED).nth(n);
  await row.hover();
  const grip = await row.locator('.job-grip').boundingBox();
  const x = grip.x + grip.width / 2;
  const y = grip.y + grip.height / 2;
  await ctx.page.mouse.move(x, y);
  await ctx.page.mouse.down();
  for (let i = 1; i <= 8; i++) await ctx.page.mouse.move(x, y + (dy * i) / 8);
  await ctx.wait(300);
};

/** Opens the "…" menu of the `n`th queued row. */
const menu = (n) => (ctx) => ctx.page.locator(QUEUED).nth(n).locator('.job-menu-btn').click();

/** Finishes the first running job with a title, as the feed would tell it. */
const finish = (ctx) => {
  const w = ctx.control.world();
  const job = [...w.jobs.values()].find((j) => j.status === 'running' && j.title);
  if (!job) return;
  Object.assign(job, { status: 'completed', finished_at: new Date().toISOString(), updated_at: new Date().toISOString() });
  w.addEvent({ job_id: job.id, type: 'job_finished', message: 'job finished', payload: { ...job } });
};

export const QUEUE_SCENARIOS = [
  {
    id: 'queue/order-all',
    world: 'active',
    worldOpts: { queued: 8 },
    path: '#/queue?tab=all',
    transitions: [
      { id: 'job-finishes', title: 'a running job finishes: it slides past the queue', run: finish },
      { id: 'job-starts', title: 'the queue head starts: no other row moves', run: (ctx) => ctx.control.tick(1) },
    ],
  },
  { id: 'queue/order-active', world: 'active', worldOpts: { queued: 8 }, path: '#/queue?tab=active' },
  { id: 'queue/order-dragging', world: 'active', worldOpts: { queued: 8 }, path: '#/queue?tab=active', setup: hold(5, -200) },
  { id: 'queue/order-menu', world: 'active', worldOpts: { queued: 8 }, path: '#/queue?tab=active', setup: menu(2) },
  {
    id: 'queue/order-refused',
    world: 'active',
    worldOpts: { queued: 8 },
    path: '#/queue?tab=active',
    faults: { 'POST /api/v1/downloads/{id}/move': { status: 409, code: 'job_not_queued', message: 'job is not queued' } },
    transitions: [
      {
        id: 'move-refused',
        title: 'Move to top refused (409): the row goes back, a toast says why',
        expectContent: true,
        run: async (ctx) => {
          await menu(3)(ctx);
          await ctx.page.locator('[role=menuitem]:has-text("Move to top")').click();
        },
      },
    ],
  },
  {
    id: 'queue/order-start',
    world: 'active',
    worldOpts: { queued: 8 },
    path: '#/queue?tab=active',
    transitions: [{ id: 'job-starts', title: 'the queue head starts: no other row moves', run: (ctx) => ctx.control.tick(1) }],
  },
];

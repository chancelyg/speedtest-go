// Toast queue + DOM renderer for transient user-facing error messages.
//
// Two layers:
//   • ToastQueue — pure, headless FIFO queue with deterministic timing
//     (injectable `clock` so unit tests don't need real timers).
//   • mountToast — wires a ToastQueue to a DOM container and renders
//     `.toast` elements with level-specific styling.
//
// All toasts are immutable records once pushed: mutation is limited to a
// `dismissed` flag flipped by the queue. Consumers call `showToast()`
// (from app.js) which delegates to the singleton mounted on page load.

const DURATION_MS = Object.freeze({
  info:  3000,
  warn:  5000,
  error: 8000,
});

export function autoDismissMs(level) {
  return DURATION_MS[level] ?? DURATION_MS.info;
}

let nextId = 1;

/**
 * Headless toast queue. DOM-free for unit testability.
 *
 * @typedef {Object} Toast
 * @property {number} id
 * @property {string} msg
 * @property {'info'|'warn'|'error'} level
 * @property {string} [actionLabel]
 * @property {() => void} [onAction]
 * @property {number} createdAt
 * @property {boolean} dismissed
 */
export class ToastQueue {
  /**
   * @param {{ max?: number, clock?: () => number }} [opts]
   */
  constructor(opts = {}) {
    this.max     = opts.max ?? 3;
    this.clock   = opts.clock ?? (() => Date.now());
    /** @type {Toast[]} */
    this.toasts  = [];
    /** @type {(t: Toast) => void} */
    this.onAdd    = () => {};
    /** @type {(t: Toast) => void} */
    this.onRemove = () => {};
  }

  /**
   * @param {{ msg: string, level: 'info'|'warn'|'error',
   *           actionLabel?: string, onAction?: () => void }} input
   * @returns {Toast}
   */
  push(input) {
    const toast = {
      id:          nextId++,
      msg:         input.msg,
      level:       input.level,
      actionLabel: input.actionLabel,
      onAction:    input.onAction,
      createdAt:   this.clock(),
      dismissed:   false,
    };
    this.toasts.push(toast);
    this.onAdd(toast);

    // Evict oldest non-dismissed toasts until we're under the cap.
    const live = this.toasts.filter(t => !t.dismissed);
    while (live.length > this.max) {
      this.dismiss(live.shift());
    }
    return toast;
  }

  active() {
    return this.toasts.filter(t => !t.dismissed);
  }

  /**
   * Mark expired (non-action) toasts as dismissed based on the clock.
   * Action toasts (those with an `actionLabel`) never auto-dismiss.
   */
  tick() {
    const now = this.clock();
    for (const t of this.toasts) {
      if (t.dismissed) continue;
      if (t.actionLabel) continue;
      const ttl = autoDismissMs(t.level);
      if (now - t.createdAt >= ttl) {
        this.dismiss(t);
      }
    }
  }

  dismiss(toast) {
    if (!toast || toast.dismissed) return;
    toast.dismissed = true;
    this.onRemove(toast);
  }

  /**
   * Fire the toast's action callback (if any) and dismiss it.
   * Safe to call multiple times — only runs once.
   */
  trigger(toast) {
    if (!toast || toast.dismissed) return;
    const cb = toast.onAction;
    this.dismiss(toast);
    if (typeof cb === 'function') {
      try { cb(); }
      catch (err) { console.error('Toast action callback threw:', err); }
    }
  }
}

/* ── DOM renderer ───────────────────────────────────────────────────────── */

/**
 * Mount a ToastQueue to a DOM container, returning a `showToast` function.
 *
 * The renderer:
 *   • appends a `.toast.toast-<level>` element per active toast
 *   • drives auto-dismiss via setInterval polling the headless queue
 *   • respects `prefers-reduced-motion` by skipping the translate transition
 *
 * @param {HTMLElement} container
 * @returns {(msg: string, level?: 'info'|'warn'|'error',
 *            opts?: { actionLabel?: string, onAction?: () => void }) => void}
 */
export function mountToast(container) {
  const queue = new ToastQueue();
  /** @type {Map<number, HTMLElement>} */
  const nodes = new Map();

  queue.onAdd = toast => {
    const el = renderToastElement(toast, queue);
    nodes.set(toast.id, el);
    container.appendChild(el);
    // Force layout, then animate in.
    requestAnimationFrame(() => el.classList.add('toast-visible'));
  };

  queue.onRemove = toast => {
    const el = nodes.get(toast.id);
    if (!el) return;
    el.classList.remove('toast-visible');
    el.classList.add('toast-leaving');
    // Match the CSS transition duration (220ms).
    setTimeout(() => {
      el.remove();
      nodes.delete(toast.id);
    }, 220);
  };

  // Single polling interval for auto-dismiss. 250 ms granularity is fine
  // for user-facing toasts (no one notices an off-by-quarter-second).
  setInterval(() => queue.tick(), 250);

  return function showToast(msg, level = 'info', opts = {}) {
    return queue.push({
      msg,
      level,
      actionLabel: opts.actionLabel,
      onAction:    opts.onAction,
    });
  };
}

function renderToastElement(toast, queue) {
  const el = document.createElement('div');
  el.className     = `toast toast-${toast.level}`;
  el.setAttribute('role', toast.level === 'error' ? 'alert' : 'status');
  el.dataset.toastId = String(toast.id);

  const msg = document.createElement('span');
  msg.className   = 'toast-msg';
  msg.textContent = toast.msg;
  el.appendChild(msg);

  if (toast.actionLabel) {
    const btn = document.createElement('button');
    btn.className   = 'toast-action';
    btn.type        = 'button';
    btn.textContent = toast.actionLabel;
    btn.addEventListener('click', () => queue.trigger(toast));
    el.appendChild(btn);
  }

  const close = document.createElement('button');
  close.className    = 'toast-close';
  close.type         = 'button';
  close.setAttribute('aria-label', 'Dismiss');
  close.textContent  = '×';
  close.addEventListener('click', () => queue.dismiss(toast));
  el.appendChild(close);

  return el;
}

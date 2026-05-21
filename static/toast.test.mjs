// Run with: node --test static/toast.test.mjs
//
// Pure-logic unit tests for the toast queue manager. We test the headless
// ToastQueue class — DOM rendering is exercised manually in the browser.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ToastQueue, autoDismissMs } from './toast.mjs';

/* ── autoDismissMs ──────────────────────────────────────────────────────── */

test('autoDismissMs: info=3s, warn=5s, error=8s', () => {
  assert.equal(autoDismissMs('info'),  3000);
  assert.equal(autoDismissMs('warn'),  5000);
  assert.equal(autoDismissMs('error'), 8000);
});

test('autoDismissMs: unknown level falls back to info duration', () => {
  assert.equal(autoDismissMs('bogus'), 3000);
});

/* ── ToastQueue: FIFO cap ───────────────────────────────────────────────── */

test('ToastQueue: holds at most 3 concurrent toasts (FIFO eviction)', () => {
  const q = new ToastQueue({ max: 3 });
  const a = q.push({ msg: 'a', level: 'info' });
  const b = q.push({ msg: 'b', level: 'info' });
  const c = q.push({ msg: 'c', level: 'info' });
  const d = q.push({ msg: 'd', level: 'info' });

  const active = q.active().map(t => t.msg);
  assert.deepEqual(active, ['b', 'c', 'd']);
  // The evicted toast should be marked dismissed.
  assert.equal(a.dismissed, true);
  assert.equal(b.dismissed, false);
  assert.equal(d.dismissed, false);
});

/* ── ToastQueue: auto-dismiss timing ────────────────────────────────────── */

test('ToastQueue: info auto-dismisses after 3s', () => {
  let now = 0;
  const q = new ToastQueue({ clock: () => now });
  const t = q.push({ msg: 'hi', level: 'info' });

  now = 2999;
  q.tick();
  assert.equal(t.dismissed, false);

  now = 3000;
  q.tick();
  assert.equal(t.dismissed, true);
});

test('ToastQueue: warn auto-dismisses after 5s, error after 8s', () => {
  let now = 0;
  const q = new ToastQueue({ clock: () => now });
  const w = q.push({ msg: 'w', level: 'warn' });
  const e = q.push({ msg: 'e', level: 'error' });

  now = 5000;
  q.tick();
  assert.equal(w.dismissed, true);
  assert.equal(e.dismissed, false);

  now = 8000;
  q.tick();
  assert.equal(e.dismissed, true);
});

/* ── ToastQueue: action button defers auto-dismiss ──────────────────────── */

test('ToastQueue: toast with action button does NOT auto-dismiss', () => {
  let now = 0;
  const q = new ToastQueue({ clock: () => now });
  const t = q.push({
    msg: 'busy',
    level: 'warn',
    actionLabel: 'Retry',
    onAction: () => {},
  });

  now = 60_000;
  q.tick();
  assert.equal(t.dismissed, false);
});

test('ToastQueue: dismiss() invokes callback', () => {
  const q = new ToastQueue();
  const t = q.push({ msg: 'x', level: 'info' });
  let removed = null;
  q.onRemove = toast => { removed = toast; };
  q.dismiss(t);
  assert.equal(t.dismissed, true);
  assert.equal(removed, t);
});

/* ── ToastQueue: trigger() runs action callback exactly once ────────────── */

test('ToastQueue: trigger() runs onAction and dismisses the toast', () => {
  const q = new ToastQueue();
  let calls = 0;
  const t = q.push({
    msg: 'busy',
    level: 'warn',
    actionLabel: 'Retry',
    onAction: () => { calls++; },
  });

  q.trigger(t);
  assert.equal(calls, 1);
  assert.equal(t.dismissed, true);

  // Calling again is a no-op (already dismissed).
  q.trigger(t);
  assert.equal(calls, 1);
});

/* ── ToastQueue: push returns a toast with stable id ────────────────────── */

test('ToastQueue: each push returns a unique id', () => {
  const q = new ToastQueue();
  const a = q.push({ msg: '1', level: 'info' });
  const b = q.push({ msg: '2', level: 'info' });
  assert.notEqual(a.id, b.id);
});

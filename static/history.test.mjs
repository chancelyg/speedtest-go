// Run with: node --test static/history.test.mjs

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountHistory, computePageWindow } from './history.mjs';

/* ── computePageWindow (pure) ──────────────────────────────────────────── */

test('computePageWindow: 1 page -> [1]', () => {
  assert.deepEqual(computePageWindow(1, 1), [1]);
});

test('computePageWindow: <=7 pages render fully without ellipsis', () => {
  assert.deepEqual(computePageWindow(7, 1), [1, 2, 3, 4, 5, 6, 7]);
  assert.deepEqual(computePageWindow(5, 3), [1, 2, 3, 4, 5]);
});

test('computePageWindow: clamps currentPage to valid range', () => {
  assert.deepEqual(computePageWindow(5, 99), [1, 2, 3, 4, 5]);
  assert.deepEqual(computePageWindow(5, -1), [1, 2, 3, 4, 5]);
  assert.deepEqual(computePageWindow(5, 0),  [1, 2, 3, 4, 5]);
});

test('computePageWindow: ellipsis on the right when current is near the start', () => {
  // total=20, current=1 → [1, 2, 3, …, 20]
  assert.deepEqual(computePageWindow(20, 1), [1, 2, 3, '…', 20]);
});

test('computePageWindow: ellipsis on the left when current is near the end', () => {
  // total=20, current=20 → [1, …, 18, 19, 20]
  assert.deepEqual(computePageWindow(20, 20), [1, '…', 18, 19, 20]);
});

test('computePageWindow: ellipsis on both sides for middle pages', () => {
  // total=20, current=10, window=2 → [1, …, 8, 9, 10, 11, 12, …, 20]
  assert.deepEqual(computePageWindow(20, 10), [1, '…', 8, 9, 10, 11, 12, '…', 20]);
});

test('computePageWindow: windowSize=0 reduces to just the current page', () => {
  // [1, …, 10, …, 20]
  assert.deepEqual(computePageWindow(20, 10, 0), [1, '…', 10, '…', 20]);
});

test('computePageWindow: non-numeric inputs degrade to safe defaults', () => {
  assert.deepEqual(computePageWindow('foo', 'bar'), [1]);
  assert.deepEqual(computePageWindow(null, undefined), [1]);
});

/* ── mountHistory contract ─────────────────────────────────────────────── */

test('mountHistory: returns an instance with the documented methods', () => {
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results', pageSize: 20, lang: 'zh' });
  assert.equal(typeof inst.refresh,  'function');
  assert.equal(typeof inst.setLang,  'function');
  assert.equal(typeof inst.goToPage, 'function');
});

test('mountHistory: refresh() returns a promise even on a stub container', () => {
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results' });
  const p = inst.refresh();
  assert.ok(p && typeof p.then === 'function', 'refresh() must return a Promise');
  p.catch(() => {});
});

test('mountHistory: setLang and goToPage do not throw on the stub', () => {
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results' });
  assert.doesNotThrow(() => inst.setLang('en'));
  assert.doesNotThrow(() => inst.setLang('zh'));
  assert.doesNotThrow(() => { inst.goToPage(2).catch(() => {}); });
});

/* ── helpers ───────────────────────────────────────────────────────────── */

function makeStubContainer() {
  return {
    children: [],
    attributes: {},
    setAttribute(k, v) { this.attributes[k] = v; },
    getAttribute(k) { return this.attributes[k]; },
    appendChild(c) { this.children.push(c); return c; },
    removeChild(c) {
      this.children = this.children.filter(x => x !== c);
      return c;
    },
    addEventListener() {},
    removeEventListener() {},
    classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
    style: {},
    dataset: {},
    hidden: false,
  };
}

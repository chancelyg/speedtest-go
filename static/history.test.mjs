// Run with: node --test static/history.test.mjs
//
// Tests the public shape of mountHistory.  DOM rendering is exercised in
// the browser; here we only verify the contract so that app.js can rely
// on it without integration testing.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountHistory } from './history.mjs';

test('mountHistory: returns an instance with the documented methods', () => {
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results', limit: 20, lang: 'zh' });
  assert.equal(typeof inst.refresh, 'function');
  assert.equal(typeof inst.setLang, 'function');
});

test('mountHistory: refresh() returns a promise even on a stub container', () => {
  // It might reject (no fetch), but it must be thenable. We don't await it —
  // we only assert it conforms to the JSDoc Promise<void> shape.
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results' });
  const p = inst.refresh();
  assert.ok(p && typeof p.then === 'function', 'refresh() must return a Promise');
  // Swallow any rejection from the missing-fetch environment.
  p.catch(() => {});
});

test('mountHistory: setLang accepts string without throwing', () => {
  const stub = makeStubContainer();
  const inst = mountHistory(stub, { apiBase: '/api/results' });
  assert.doesNotThrow(() => inst.setLang('en'));
  assert.doesNotThrow(() => inst.setLang('zh'));
});

/* ── helpers ───────────────────────────────────────────────────────────── */

function makeStubContainer() {
  const el = {
    children: [],
    attributes: {},
    innerHTML: '',
    textContent: '',
    setAttribute(k, v) { this.attributes[k] = v; },
    getAttribute(k) { return this.attributes[k]; },
    appendChild(child) { this.children.push(child); return child; },
    removeChild(child) {
      this.children = this.children.filter(c => c !== child);
      return child;
    },
    addEventListener() {},
    removeEventListener() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
    style: {},
    dataset: {},
    hidden: false,
  };
  return el;
}

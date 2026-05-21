// Run with: node --test static/trends.test.mjs
//
// Tests for the pure aggregator (bucketAndMedian) plus the public shape of
// mountTrends. DOM-heavy behaviour is exercised manually in the browser.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { bucketAndMedian, mountTrends } from './trends.mjs';

/* ── bucketAndMedian ───────────────────────────────────────────────────── */

test('bucketAndMedian: empty array returns empty array', () => {
  assert.deepEqual(bucketAndMedian([], 60_000), []);
});

test('bucketAndMedian: rejects non-positive bucket size', () => {
  assert.deepEqual(bucketAndMedian([{ t: 0, v: 1 }], 0), []);
  assert.deepEqual(bucketAndMedian([{ t: 0, v: 1 }], -1), []);
});

test('bucketAndMedian: single point yields one bucket with that value', () => {
  const out = bucketAndMedian([{ t: 1000, v: 5 }], 1000);
  assert.equal(out.length, 1);
  assert.equal(out[0].v, 5);
  // bucket centre or aligned start — must be a finite number
  assert.ok(Number.isFinite(out[0].t));
});

test('bucketAndMedian: odd-count median is middle value', () => {
  const pts = [
    { t: 0,    v: 10 },
    { t: 100,  v: 30 },
    { t: 200,  v: 20 },
  ];
  const out = bucketAndMedian(pts, 1000);
  assert.equal(out.length, 1);
  assert.equal(out[0].v, 20);
});

test('bucketAndMedian: even-count median is average of two middles', () => {
  const pts = [
    { t: 0,   v: 10 },
    { t: 10,  v: 20 },
    { t: 20,  v: 30 },
    { t: 30,  v: 40 },
  ];
  const out = bucketAndMedian(pts, 1000);
  assert.equal(out.length, 1);
  assert.equal(out[0].v, 25);   // (20 + 30) / 2
});

test('bucketAndMedian: distributes across multiple buckets in time order', () => {
  const pts = [
    { t: 0,     v: 1 },
    { t: 500,   v: 3 },
    { t: 1500,  v: 10 },
    { t: 1700,  v: 20 },
    { t: 3200,  v: 100 },
  ];
  const out = bucketAndMedian(pts, 1000);
  assert.equal(out.length, 3);
  // bucket times must be monotonically increasing
  assert.ok(out[0].t <= out[1].t && out[1].t <= out[2].t);
  // medians: bucket1=[1,3]→2, bucket2=[10,20]→15, bucket3=[100]→100
  assert.equal(out[0].v, 2);
  assert.equal(out[1].v, 15);
  assert.equal(out[2].v, 100);
});

test('bucketAndMedian: ignores NaN / non-finite values', () => {
  const pts = [
    { t: 0, v: NaN },
    { t: 1, v: 10 },
    { t: 2, v: Infinity },
    { t: 3, v: 20 },
  ];
  const out = bucketAndMedian(pts, 1000);
  assert.equal(out.length, 1);
  assert.equal(out[0].v, 15);
});

/* ── mountTrends public API shape ──────────────────────────────────────── */

test('mountTrends: returns an instance with the documented methods', () => {
  // Use a minimal stub container — mountTrends should not throw on a bare
  // object that supports the few DOM ops it needs. We don't render here.
  const stub = makeStubContainer();
  const inst = mountTrends(stub, { apiBase: '/api/results', lang: 'en' });
  assert.equal(typeof inst.setWindow,  'function');
  assert.equal(typeof inst.refresh,    'function');
  assert.equal(typeof inst.setLang,    'function');
});

/* ── helpers ───────────────────────────────────────────────────────────── */

function makeStubContainer() {
  // Minimal duck-type — enough for mountTrends to render its skeleton without
  // touching a real DOM. Anything mountTrends needs that we haven't faked
  // here will throw, which is what we want during a unit test.
  const el = {
    children: [],
    attributes: {},
    listeners: {},
    innerHTML: '',
    textContent: '',
    setAttribute(k, v) { this.attributes[k] = v; },
    getAttribute(k) { return this.attributes[k]; },
    appendChild(child) { this.children.push(child); return child; },
    removeChild(child) {
      this.children = this.children.filter(c => c !== child);
      return child;
    },
    addEventListener(type, fn) {
      (this.listeners[type] ||= []).push(fn);
    },
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

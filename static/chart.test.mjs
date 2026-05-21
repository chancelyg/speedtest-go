// Run with: node --test static/chart.test.mjs
//
// Unit tests for chart.mjs. Tests the pure helpers (pushBounded, pointsString,
// logScaleY) directly, and verifies the public renderChart() returns an
// instance with the expected shape via a minimal mock SVG element.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  renderChart,
  pushBounded,
  pointsString,
  logScaleY,
  CHART_W,
  CHART_H,
} from './chart.mjs';

/* ── pushBounded ─────────────────────────────────────────────────────────── */

test('pushBounded: appends below capacity', () => {
  const out = pushBounded([1, 2], 3, 5);
  assert.deepEqual(out, [1, 2, 3]);
});

test('pushBounded: drops oldest at capacity', () => {
  const out = pushBounded([1, 2, 3], 4, 3);
  assert.deepEqual(out, [2, 3, 4]);
});

test('pushBounded: does not mutate input array', () => {
  const input = [1, 2, 3];
  const before = input.slice();
  pushBounded(input, 4, 5);
  assert.deepEqual(input, before);
});

test('pushBounded: maxLen=1 keeps only most recent', () => {
  let w = [];
  w = pushBounded(w, 'a', 1);
  w = pushBounded(w, 'b', 1);
  w = pushBounded(w, 'c', 1);
  assert.deepEqual(w, ['c']);
});

/* ── logScaleY ───────────────────────────────────────────────────────────── */

test('logScaleY: 0.1 Mbps maps to bottom of chart (y = CHART_H)', () => {
  assert.equal(logScaleY(0.1), CHART_H);
});

test('logScaleY: 1000 Mbps maps to top of chart (y = 0)', () => {
  assert.equal(logScaleY(1000), 0);
});

test('logScaleY: 10 Mbps lands at log-scale midpoint', () => {
  // log10(10) = 1, log10(0.1)=-1, log10(1000)=3 → fraction = (1-(-1))/4 = 0.5
  // y = CHART_H * (1 - 0.5) = CHART_H/2
  assert.equal(logScaleY(10), CHART_H / 2);
});

test('logScaleY: values <= 0 or NaN clamp to bottom', () => {
  assert.equal(logScaleY(0), CHART_H);
  assert.equal(logScaleY(-5), CHART_H);
  assert.equal(logScaleY(NaN), CHART_H);
});

test('logScaleY: values above 1000 clamp to top', () => {
  assert.equal(logScaleY(1e6), 0);
});

test('logScaleY: monotonically decreases as Mbps increases', () => {
  const samples = [0.1, 1, 10, 100, 1000];
  let prev = Infinity;
  for (const s of samples) {
    const y = logScaleY(s);
    assert.ok(y < prev, `logScaleY(${s})=${y} should be < ${prev}`);
    prev = y;
  }
});

/* ── pointsString ────────────────────────────────────────────────────────── */

test('pointsString: empty list returns empty string', () => {
  assert.equal(pointsString([], 60000), '');
});

test('pointsString: single point is rendered', () => {
  const s = pointsString([{ t: 0, v: 10 }], 60000);
  assert.match(s, /^0,\d+(?:\.\d+)?$/);
});

test('pointsString: skips null values (per-series gaps)', () => {
  const s = pointsString([
    { t: 0,    v: 10 },
    { t: 1000, v: null },
    { t: 2000, v: 100 },
  ], 60000);
  // Two coordinates, comma-space-separated by space
  const coords = s.split(' ').filter(Boolean);
  assert.equal(coords.length, 2);
});

test('pointsString: x scales linearly across maxTimeMs', () => {
  const s = pointsString([
    { t: 0,     v: 10 },
    { t: 30000, v: 10 },
    { t: 60000, v: 10 },
  ], 60000);
  const xs = s.split(' ').filter(Boolean).map(p => Number(p.split(',')[0]));
  // Roughly 0, CHART_W/2, CHART_W
  assert.ok(Math.abs(xs[0]) < 0.5);
  assert.ok(Math.abs(xs[1] - CHART_W / 2) < 0.5);
  assert.ok(Math.abs(xs[2] - CHART_W) < 0.5);
});

test('pointsString: t beyond maxTimeMs still clamped to chart width', () => {
  const s = pointsString([{ t: 120000, v: 10 }], 60000);
  const x = Number(s.split(',')[0]);
  assert.equal(x, CHART_W);
});

/* ── renderChart (integration with minimal mock) ─────────────────────────── */

function makeMockSvg() {
  const children = [];
  const attrs = {};
  const ns = 'http://www.w3.org/2000/svg';
  function makeNode(tagName) {
    const node = {
      tagName,
      attrs: {},
      children: [],
      setAttribute(k, v) { this.attrs[k] = String(v); },
      getAttribute(k) { return this.attrs[k]; },
      appendChild(c) { this.children.push(c); return c; },
      removeChild(c) {
        const i = this.children.indexOf(c);
        if (i >= 0) this.children.splice(i, 1);
        return c;
      },
      get textContent() { return this._text || ''; },
      set textContent(v) { this._text = String(v); },
    };
    return node;
  }
  return {
    tagName: 'svg',
    attrs,
    children,
    ownerDocument: {
      createElementNS(_ns, tagName) { return makeNode(tagName); },
    },
    setAttribute(k, v) { this.attrs[k] = String(v); },
    getAttribute(k) { return this.attrs[k]; },
    appendChild(c) { this.children.push(c); return c; },
    removeChild(c) {
      const i = this.children.indexOf(c);
      if (i >= 0) this.children.splice(i, 1);
      return c;
    },
    querySelectorAll() { return []; },
    _ns: ns,
  };
}

test('renderChart: returns instance with pushPoint and reset methods', () => {
  const svg = makeMockSvg();
  const inst = renderChart(svg);
  assert.equal(typeof inst.pushPoint, 'function');
  assert.equal(typeof inst.reset, 'function');
});

test('renderChart: pushPoint accepts (t, dl, ul) without throwing', () => {
  const svg = makeMockSvg();
  const inst = renderChart(svg, { maxPoints: 100, maxTimeMs: 60000 });
  assert.doesNotThrow(() => inst.pushPoint(0, 10, null));
  assert.doesNotThrow(() => inst.pushPoint(1000, null, 5));
  assert.doesNotThrow(() => inst.pushPoint(2000, 50, 20));
});

test('renderChart: reset() clears internal points', () => {
  const svg = makeMockSvg();
  const inst = renderChart(svg, { maxPoints: 100 });
  inst.pushPoint(0, 10, null);
  inst.pushPoint(1000, 50, null);
  assert.doesNotThrow(() => inst.reset());
  // After reset, peek() (test-only helper) should be empty.
  if (typeof inst._peek === 'function') {
    const { dl, ul } = inst._peek();
    assert.equal(dl.length, 0);
    assert.equal(ul.length, 0);
  }
});

test('renderChart: maxPoints caps stored points per series', () => {
  const svg = makeMockSvg();
  const inst = renderChart(svg, { maxPoints: 3, maxTimeMs: 60000 });
  inst.pushPoint(0,    1,  null);
  inst.pushPoint(100,  2,  null);
  inst.pushPoint(200,  3,  null);
  inst.pushPoint(300,  4,  null);
  inst.pushPoint(400,  5,  null);
  if (typeof inst._peek === 'function') {
    const { dl } = inst._peek();
    assert.equal(dl.length, 3);
    assert.deepEqual(dl.map(p => p.v), [3, 4, 5]);
  }
});

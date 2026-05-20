// Run with: node --test static/metrics.test.mjs
//
// Pure-function unit tests for the metrics helpers.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gaugeAngle, windowStats, pushWindow, throughputMbps } from './metrics.mjs';

/* ── gaugeAngle ────────────────────────────────────────────────────────── */

test('gaugeAngle: zero / negative / NaN clamps to 180°', () => {
  assert.equal(gaugeAngle(0), 180);
  assert.equal(gaugeAngle(-5), 180);
  assert.equal(gaugeAngle(NaN), 180);
});

test('gaugeAngle: scale anchor points', () => {
  // log-scale anchors: 1, 10, 100, 1000 Mbps mapped to 135°, 90°, 45°, 0°
  assert.equal(Math.round(gaugeAngle(1)),    135);
  assert.equal(Math.round(gaugeAngle(10)),    90);
  assert.equal(Math.round(gaugeAngle(100)),   45);
  assert.equal(Math.round(gaugeAngle(1000)),   0);
});

test('gaugeAngle: values above scale are clamped to 0°', () => {
  assert.equal(gaugeAngle(5000),  0);
  assert.equal(gaugeAngle(1e9),   0);
});

test('gaugeAngle: monotonically decreases as speed increases', () => {
  const samples = [1, 5, 10, 50, 100, 500, 1000];
  let prev = Infinity;
  for (const s of samples) {
    const a = gaugeAngle(s);
    assert.ok(a < prev, `gaugeAngle(${s})=${a} should be < ${prev}`);
    prev = a;
  }
});

/* ── windowStats ───────────────────────────────────────────────────────── */

test('windowStats: empty window returns zeros', () => {
  const r = windowStats([]);
  assert.deepEqual(r, { latency: 0, jitter: 0, packetLoss: 0 });
});

test('windowStats: all-failed window reports 100% loss, 0 latency/jitter', () => {
  const r = windowStats([{ rtt: 0, ok: false }, { rtt: 0, ok: false }]);
  assert.equal(r.packetLoss, 100);
  assert.equal(r.latency, 0);
  assert.equal(r.jitter, 0);
});

test('windowStats: mixed success/failure', () => {
  const r = windowStats([
    { rtt: 10, ok: true },
    { rtt: 0,  ok: false },  // dropped from rtt computations
    { rtt: 14, ok: true },
    { rtt: 12, ok: true },
  ]);
  assert.equal(r.packetLoss, 25);          // 1/4
  assert.equal(r.latency, (10 + 14 + 12) / 3);
  // jitter on [10, 14, 12] = (|14-10| + |12-14|) / 2 = 6/2 = 3
  assert.equal(r.jitter, 3);
});

/* ── pushWindow ────────────────────────────────────────────────────────── */

test('pushWindow: under capacity appends', () => {
  const out = pushWindow([1, 2], 3, 5);
  assert.deepEqual(out, [1, 2, 3]);
});

test('pushWindow: at capacity drops oldest', () => {
  const out = pushWindow([1, 2, 3], 4, 3);
  assert.deepEqual(out, [2, 3, 4]);
});

test('pushWindow: does not mutate input', () => {
  const input = [1, 2, 3];
  const frozen = Object.freeze(input.slice());
  const out = pushWindow(input, 4, 3);
  assert.deepEqual(input, frozen);
  assert.notEqual(out, input);
});

test('pushWindow: handles maxLen=1', () => {
  let w = [];
  w = pushWindow(w, 'a', 1);
  w = pushWindow(w, 'b', 1);
  w = pushWindow(w, 'c', 1);
  assert.deepEqual(w, ['c']);
});

/* ── throughputMbps ────────────────────────────────────────────────────── */

test('throughputMbps: 1 MB in 1000 ms = 8 Mbps', () => {
  // 1 MB = 1,048,576 bytes → 8,388,608 bits → 8.388608 Mbps
  const m = throughputMbps(1024 * 1024, 1000);
  assert.ok(Math.abs(m - 8.388608) < 1e-9, `got ${m}`);
});

test('throughputMbps: zero or negative elapsed returns 0', () => {
  assert.equal(throughputMbps(1024, 0),  0);
  assert.equal(throughputMbps(1024, -1), 0);
});

test('throughputMbps: NaN inputs return 0', () => {
  assert.equal(throughputMbps(NaN, 100), 0);
  assert.equal(throughputMbps(100, NaN), 0);
});

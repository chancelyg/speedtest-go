// Run with: node --test static/metrics.test.mjs
//
// Pure-function unit tests for the metrics helpers.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { gaugeAngle, windowStats, pushWindow, throughputMbps, jitterRFC3550, percentile } from './metrics.mjs';

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

/* ── jitterRFC3550 ─────────────────────────────────────────────────────── */

test('jitterRFC3550: empty input returns 0', () => {
  assert.equal(jitterRFC3550([]), 0);
});

test('jitterRFC3550: single sample returns 0 (no pair to diff)', () => {
  assert.equal(jitterRFC3550([42]), 0);
});

test('jitterRFC3550: constant series stays at 0', () => {
  assert.equal(jitterRFC3550([10, 10, 10, 10, 10]), 0);
});

test('jitterRFC3550: hand-computed reference [10, 12, 11, 13]', () => {
  // J(0)=0
  // i=1: D=|12-10|=2, J = 0 + (2 - 0)/16 = 0.125
  // i=2: D=|11-12|=1, J = 0.125 + (1 - 0.125)/16 = 0.1796875
  // i=3: D=|13-11|=2, J = 0.1796875 + (2 - 0.1796875)/16 = 0.29345703125
  const j = jitterRFC3550([10, 12, 11, 13]);
  assert.ok(Math.abs(j - 0.29345703125) < 1e-12, `got ${j}`);
});

test('jitterRFC3550: two samples returns |D|/16', () => {
  // J = 0 + (|D| - 0)/16 = |D|/16
  assert.equal(jitterRFC3550([10, 18]), 0.5);   // (8/16)
  assert.equal(jitterRFC3550([100, 90]), 0.625); // (10/16)
});

test('jitterRFC3550: converges toward the mean absolute diff for long series', () => {
  // Repeated diff of 4 → equilibrium J ≈ 4 (since J += (4 - J)/16 settles where 4 - J ≈ 0).
  const rtts = [];
  let x = 0;
  for (let i = 0; i < 500; i++) { rtts.push(x); x = x === 0 ? 4 : 0; } // diffs = 4 each step
  const j = jitterRFC3550(rtts);
  assert.ok(Math.abs(j - 4) < 1e-3, `expected ~4, got ${j}`);
});

test('jitterRFC3550: ignores NaN by treating it as a non-finite drop', () => {
  // Robustness: must not blow up if a stray non-finite slips in.
  const j = jitterRFC3550([10, NaN, 12, 14]);
  // Filter -> [10, 12, 14]; J(0)=0, i=1 D=2 J=0.125, i=2 D=2 J=0.125 + (2-0.125)/16 = 0.2421875
  assert.ok(Math.abs(j - 0.2421875) < 1e-12, `got ${j}`);
});

/* ── percentile ────────────────────────────────────────────────────────── */

test('percentile: empty samples returns 0', () => {
  assert.equal(percentile([], 50), 0);
  assert.equal(percentile([], 99), 0);
});

test('percentile: single sample returns that sample for any p', () => {
  assert.equal(percentile([7], 0), 7);
  assert.equal(percentile([7], 50), 7);
  assert.equal(percentile([7], 100), 7);
});

test('percentile: p0 = min, p100 = max', () => {
  const s = [3, 1, 4, 1, 5, 9, 2, 6];
  assert.equal(percentile(s, 0),   1);
  assert.equal(percentile(s, 100), 9);
});

test('percentile: p50 on 1..100 ≈ 50.5 (linear interpolation)', () => {
  const s = Array.from({ length: 100 }, (_, i) => i + 1);
  const m = percentile(s, 50);
  assert.ok(Math.abs(m - 50.5) < 1e-9, `got ${m}`);
});

test('percentile: p95 on 1..100 ≈ 95.05', () => {
  const s = Array.from({ length: 100 }, (_, i) => i + 1);
  const m = percentile(s, 95);
  assert.ok(Math.abs(m - 95.05) < 1e-9, `got ${m}`);
});

test('percentile: does not require pre-sorted input (sorts internally)', () => {
  const a = percentile([5, 1, 4, 3, 2], 50);
  const b = percentile([1, 2, 3, 4, 5], 50);
  assert.equal(a, b);
});

test('percentile: does not mutate caller array', () => {
  const s = [5, 1, 4, 3, 2];
  const frozen = s.slice();
  percentile(s, 50);
  assert.deepEqual(s, frozen);
});

test('percentile: clamps p outside [0,100]', () => {
  const s = [1, 2, 3, 4, 5];
  assert.equal(percentile(s, -10),   1);   // clamps to 0 → min
  assert.equal(percentile(s, 1000),  5);   // clamps to 100 → max
});

// Run with: node --test static/jitter.test.mjs
//
// Pure-function unit tests for computeJitter. Uses Node's built-in test
// runner and assert library — zero npm dependencies, no package.json
// required, no build pipeline introduced.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeJitter } from './jitter.mjs';

test('empty input returns 0', () => {
  assert.equal(computeJitter([]), 0);
});

test('single sample returns 0 (no pairs to diff)', () => {
  assert.equal(computeJitter([100]), 0);
});

test('constant RTTs return 0 jitter', () => {
  assert.equal(computeJitter([10, 10, 10, 10]), 0);
});

test('two samples: jitter is the absolute diff', () => {
  assert.equal(computeJitter([10, 12]), 2);
});

test('classic RFC-style reference vector', () => {
  // rtts = [10, 12, 11, 15, 10]
  // diffs: |12-10|=2, |11-12|=1, |15-11|=4, |10-15|=5
  // sum = 12, divided by (n-1)=4 ⇒ 3
  assert.equal(computeJitter([10, 12, 11, 15, 10]), 3);
});

test('not influenced by mean (vs. stddev)', () => {
  // stddev of [1, 1, 1, 100] is huge; mean-abs-diff of consecutive pairs is
  // |0|+|0|+|99| = 99 / 3 ≈ 33. Verifies we are NOT computing stddev.
  const j = computeJitter([1, 1, 1, 100]);
  assert.equal(j, 33);
});

test('handles floating-point RTTs without precision drift', () => {
  // performance.now() returns sub-millisecond floats. Confirm result is
  // numerically sensible.
  const j = computeJitter([10.5, 12.25, 11.0, 10.75]);
  // diffs: 1.75, 1.25, 0.25  → sum 3.25  → /3 ≈ 1.0833...
  assert.ok(Math.abs(j - 1.0833333333333333) < 1e-9, `got ${j}`);
});

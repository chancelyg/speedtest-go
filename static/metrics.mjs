/**
 * Pure-logic helpers for the speed-test frontend.
 *
 * Kept in their own module (with matching .test.mjs) so they can be unit
 * tested with `node --test` — zero npm dependencies, no build pipeline.
 *
 * @module metrics
 */

import { computeJitter } from './jitter.mjs';

/**
 * Map a throughput value (Mbps) to an arc angle in degrees for the
 * semicircle gauge. The semicircle sweeps from 180° (0 Mbps, left) to
 * 0° (right, top of scale). A log10 scale gives sensible resolution
 * across 10 Mbps → 10 Gbps home links.
 *
 * Reference scale points:
 *   0.1 Mbps → 180°
 *     1 Mbps → 135°
 *    10 Mbps →  90°
 *   100 Mbps →  45°
 *  1000 Mbps →   0°
 *
 * Values outside the range are clamped, NaN/negative collapses to 0 Mbps.
 *
 * @param {number} mbps
 * @returns {number} angle in degrees, 0..180
 */
export function gaugeAngle(mbps) {
  if (!Number.isFinite(mbps) || mbps <= 0.1) return 180;
  const clamped = Math.min(mbps, 1000);
  // log10(0.1)=-1, log10(1000)=3 → range = 4 decades → 45°/decade
  const decadesFromZero = Math.log10(clamped) - Math.log10(0.1);
  return Math.max(0, 180 - decadesFromZero * 45);
}

/**
 * Aggregate latency, jitter and packet loss statistics from a rolling
 * window of ping samples. A sample is `{ rtt, ok }` where `rtt` is the
 * round-trip time in ms (ignored when `ok === false`).
 *
 * Returns zeros for an empty window so callers can render "--" safely.
 *
 * @param {Array<{rtt:number, ok:boolean}>} samples
 * @returns {{latency:number, jitter:number, packetLoss:number}}
 */
export function windowStats(samples) {
  if (!samples || samples.length === 0) {
    return { latency: 0, jitter: 0, packetLoss: 0 };
  }
  const rtts = samples.filter(s => s.ok).map(s => s.rtt);
  const failed = samples.length - rtts.length;
  const avg = rtts.length === 0 ? 0 : rtts.reduce((a, b) => a + b, 0) / rtts.length;
  return {
    latency:    avg,
    jitter:     computeJitter(rtts),
    packetLoss: (failed / samples.length) * 100,
  };
}

/**
 * Push a new sample into a bounded rolling window. Returns a new array
 * (does not mutate input) — the oldest sample is dropped once `maxLen`
 * is reached so the window stays a fixed size.
 *
 * @template T
 * @param {ReadonlyArray<T>} window
 * @param {T} sample
 * @param {number} maxLen
 * @returns {T[]}
 */
export function pushWindow(window, sample, maxLen) {
  const next = window.length >= maxLen
    ? window.slice(window.length - maxLen + 1)
    : window.slice();
  next.push(sample);
  return next;
}

/**
 * Compute the throughput (Mbps) from a byte count and elapsed wall time.
 * Returns 0 for non-positive elapsed time so callers don't divide by zero.
 *
 * @param {number} bytes
 * @param {number} elapsedMs
 * @returns {number} Mbps (megabits per second)
 */
export function throughputMbps(bytes, elapsedMs) {
  if (!Number.isFinite(bytes) || !Number.isFinite(elapsedMs) || elapsedMs <= 0) return 0;
  return (bytes * 8) / (elapsedMs * 1000);
}

/**
 * RFC 3550 inter-packet jitter (smoothed mean absolute deviation):
 *
 *     J(0) = 0
 *     J(i) = J(i-1) + (|D(i, i-1)| - J(i-1)) / 16
 *
 * where D(i, i-1) = rtt[i] - rtt[i-1]. The /16 weight is the canonical
 * exponential moving average constant from the RFC — it smooths bursts
 * while still tracking sustained changes. The first sample seeds the
 * series but contributes nothing on its own (no pair to diff yet).
 *
 * Non-finite values (NaN, ±Infinity, null, undefined) are filtered out
 * before the recurrence so a single bad sample does not poison the
 * series. Returns 0 for inputs with fewer than 2 finite samples.
 *
 * @param {number[]} samples  Per-ping RTT values in milliseconds.
 * @returns {number}          Jitter in the same unit; 0 for < 2 samples.
 */
export function jitterRFC3550(samples) {
  if (!samples || samples.length < 2) return 0;
  const finite = samples.filter(Number.isFinite);
  if (finite.length < 2) return 0;
  let j = 0;
  for (let i = 1; i < finite.length; i++) {
    const d = Math.abs(finite[i] - finite[i - 1]);
    j = j + (d - j) / 16;
  }
  return j;
}

/**
 * Linear-interpolation percentile (matches NumPy / Excel "PERCENTILE.INC"
 * and the C=1 variant in Hyndman & Fan 1996). For an empty input returns
 * 0 so callers can render "--" safely.
 *
 *   rank = (p / 100) * (n - 1)
 *   floor = sorted[⌊rank⌋]
 *   ceil  = sorted[⌈rank⌉]
 *   value = floor + (rank - ⌊rank⌋) * (ceil - floor)
 *
 * The input array is copied before sorting so callers' state is preserved
 * (immutability requirement). `p` is clamped to [0, 100].
 *
 * @param {number[]} samples
 * @param {number} p  Percentile in [0, 100].
 * @returns {number}  Interpolated percentile value; 0 when samples is empty.
 */
export function percentile(samples, p) {
  if (!samples || samples.length === 0) return 0;
  const sorted = samples.slice().sort((a, b) => a - b);
  const n = sorted.length;
  if (n === 1) return sorted[0];
  const clamped = Math.max(0, Math.min(100, p));
  const rank    = (clamped / 100) * (n - 1);
  const lo      = Math.floor(rank);
  const hi      = Math.ceil(rank);
  if (lo === hi) return sorted[lo];
  return sorted[lo] + (rank - lo) * (sorted[hi] - sorted[lo]);
}

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

/**
 * Compute jitter as the mean absolute difference between consecutive RTT
 * samples. This is the metric reported by LibreSpeed and fast.com style
 * tools — closer to RFC 3550 "interarrival jitter" than the previous
 * implementation, which used standard deviation around the mean and was
 * sensitive to single outliers.
 *
 *   jitter = Σ |rtt[i] - rtt[i-1]| / (n - 1)
 *
 * @param {number[]} rtts  Per-ping round-trip times in milliseconds.
 * @returns {number}       Jitter in the same unit; 0 for inputs with < 2 samples.
 */
export function computeJitter(rtts) {
  if (!rtts || rtts.length < 2) return 0;
  let sum = 0;
  for (let i = 1; i < rtts.length; i++) {
    sum += Math.abs(rtts[i] - rtts[i - 1]);
  }
  return sum / (rtts.length - 1);
}

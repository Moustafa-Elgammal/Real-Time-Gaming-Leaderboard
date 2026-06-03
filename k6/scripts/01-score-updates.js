/**
 * Scenario 01 — Write path / batcher stress
 *
 * Ramps to 300 VUs all hammering POST /v1/scores/:username.
 * This saturates the EventBatcher: at 300 VUs the 500-event flush
 * threshold triggers constantly, exercising the MySQL batch insert
 * path under real concurrency.
 *
 * Thresholds:
 *   p95 < 200 ms   — Redis ZINCRBY + batcher enqueue should be fast
 *   error rate < 1%
 */

import { sleep } from 'k6';
import { generateHtmlReport, postScore, randomUsername } from '../utils/helpers.js';

export const options = {
  stages: [
    { duration: '30s', target: 50 },   // warm-up
    { duration: '1m',  target: 300 },  // ramp to peak
    { duration: '2m',  target: 300 },  // hold — sustained batcher stress
    { duration: '30s', target: 0 },    // ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<200', 'p(99)<500'],
    http_req_failed:   ['rate<0.01'],
  },
};

export default function () {
  postScore(randomUsername());
  sleep(0.1); // 10 req/s per VU max; total ≈ 3 000 req/s at peak
}

export function handleSummary(data) {
  return {
    '/reports/01-score-updates.html': generateHtmlReport(data),
    '/reports/01-score-updates.json': JSON.stringify(data, null, 2),
  };
}

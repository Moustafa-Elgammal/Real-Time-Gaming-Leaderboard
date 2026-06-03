/**
 * Scenario 02 — Read throughput: GET /v1/scores (TopN)
 *
 * Steady read load against the top-scores endpoint.
 * All reads hit Redis ZREVRANGEWITHSCORES — no MySQL involved.
 * Verifies Redis can sustain concurrent sorted-set reads without latency creep.
 *
 * Thresholds:
 *   p95 < 50 ms   — Redis sorted set reads should be sub-millisecond at the store level
 *   error rate < 0.1%
 */

import { sleep } from 'k6';
import { generateHtmlReport, getTopScores } from '../utils/helpers.js';

export const options = {
  stages: [
    { duration: '20s', target: 50 },
    { duration: '2m',  target: 200 },  // steady read load
    { duration: '1m',  target: 200 },  // hold
    { duration: '20s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<50', 'p(99)<150'],
    http_req_failed:   ['rate<0.001'],
  },
};

export default function () {
  getTopScores();
  sleep(0.05); // ~20 req/s per VU max
}

export function handleSummary(data) {
  return {
    '/reports/02-top-scores.html': generateHtmlReport(data),
    '/reports/02-top-scores.json': JSON.stringify(data, null, 2),
  };
}

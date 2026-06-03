/**
 * Scenario 03 — Neighborhood queries: GET /v1/scores/:username
 *
 * Each VU picks a random player and fetches their rank + neighbors.
 * The handler executes ZREVRANK + ZREVRANGEWITHSCORES in a pipeline.
 * Tests that the pipeline latency stays acceptable under concurrency.
 * Expects some 404s (player not yet on leaderboard) — those are allowed.
 *
 * Thresholds:
 *   p95 < 100 ms
 *   error rate < 0.1% (404 is not counted as an error by k6 by default,
 *     but we explicitly accept both 200 and 404 in the check)
 */

import { sleep } from 'k6';
import { generateHtmlReport, getNeighborhood, randomUsername } from '../utils/helpers.js';

export const options = {
  stages: [
    { duration: '20s', target: 30 },
    { duration: '2m',  target: 100 },
    { duration: '1m',  target: 100 },
    { duration: '20s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<100', 'p(99)<250'],
    http_req_failed:   ['rate<0.001'],
  },
};

export default function () {
  getNeighborhood(randomUsername());
  sleep(0.1);
}

export function handleSummary(data) {
  return {
    '/reports/03-neighborhood.html': generateHtmlReport(data),
    '/reports/03-neighborhood.json': JSON.stringify(data, null, 2),
  };
}

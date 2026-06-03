/**
 * Full Suite — all three endpoint types running concurrently
 *
 * Simulates realistic mixed traffic:
 *   - score_updates   : write path (batcher + MySQL + Redis)
 *   - read_top        : GET /v1/scores (Redis ZREVRANGEWITHSCORES)
 *   - read_neighbors  : GET /v1/scores/:username (Redis pipeline)
 *
 * Reads start 30s after writes so the leaderboard has some data before
 * neighborhood queries begin (reduces 404 noise in the results).
 *
 * Total duration: ~5 minutes.
 * Run this against the full Docker stack to get baseline numbers before
 * any scalability work.
 */

import { sleep } from 'k6';
import { generateHtmlReport, postScore, getTopScores, getNeighborhood, randomUsername } from '../utils/helpers.js';

export const options = {
  scenarios: {
    score_updates: {
      executor: 'ramping-vus',
      exec: 'scoreUpdates',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 50  },
        { duration: '1m',  target: 200 },
        { duration: '2m',  target: 200 },
        { duration: '30s', target: 0   },
      ],
      tags: { scenario: 'score-updates' },
    },

    read_top: {
      executor: 'constant-vus',
      exec: 'readTop',
      vus: 50,
      duration: '3m',
      startTime: '30s',
      tags: { scenario: 'top-scores' },
    },

    read_neighbors: {
      executor: 'constant-vus',
      exec: 'readNeighbors',
      vus: 30,
      duration: '3m',
      startTime: '30s',
      tags: { scenario: 'neighborhood' },
    },
  },

  thresholds: {
    'http_req_duration{name:post-score}':   ['p(95)<200'],
    'http_req_duration{name:top-scores}':   ['p(95)<50'],
    'http_req_duration{name:neighborhood}': ['p(95)<100'],
    http_req_failed: ['rate<0.01'],
  },
};

export function scoreUpdates() {
  postScore(randomUsername());
  sleep(0.1);
}

export function readTop() {
  getTopScores();
  sleep(0.05);
}

export function readNeighbors() {
  getNeighborhood(randomUsername());
  sleep(0.1);
}

export function handleSummary(data) {
  return {
    '/reports/full-suite.html': generateHtmlReport(data),
    '/reports/full-suite.json': JSON.stringify(data, null, 2),
  };
}

/**
 * Scenario 04 — Spike test
 *
 * Uses ramping-arrival-rate (open model) to inject a sudden traffic spike
 * regardless of response time. This stresses:
 *   - The EventBatcher buffer (does it drop events or overflow?)
 *   - Redis ZINCRBY under burst concurrency
 *   - The Gin HTTP server's ability to queue and drain connections
 *
 * Pattern: warm-up → spike (10× baseline) → recovery → cool-down
 *
 * Thresholds are intentionally relaxed during a spike — we care more
 * about zero errors and eventual recovery than sub-100ms p95.
 */

import { generateHtmlReport, postScore, getTopScores, randomUsername } from '../utils/helpers.js';

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 200,
      maxVUs: 500,
      stages: [
        { duration: '30s', target: 30  },  // warm-up baseline
        { duration: '15s', target: 500 },  // spike: 500 req/s
        { duration: '1m',  target: 500 },  // sustained spike
        { duration: '15s', target: 30  },  // recovery
        { duration: '30s', target: 30  },  // stable post-spike
      ],
    },
  },
  thresholds: {
    http_req_failed:               ['rate<0.02'],          // allow up to 2% errors during spike
    'http_req_duration{name:post-score}': ['p(95)<1000'],  // relaxed during spike
    'http_req_duration{name:top-scores}': ['p(95)<200'],   // reads should stay fast
  },
};

export default function () {
  // Mix of writes (80%) and reads (20%) to simulate a real traffic burst.
  if (Math.random() < 0.8) {
    postScore(randomUsername());
  } else {
    getTopScores();
  }
}

export function handleSummary(data) {
  return {
    '/reports/04-spike.html': generateHtmlReport(data),
    '/reports/04-spike.json': JSON.stringify(data, null, 2),
  };
}

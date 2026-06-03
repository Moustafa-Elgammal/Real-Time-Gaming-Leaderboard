import { check } from 'k6';
import http from 'k6/http';

export var BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// Returns params object with auth header + Content-Type.
// Uses Object.assign (not spread) for k6 0.48.0 Babel compatibility.
export function authParams(extraParams) {
  var headers = { 'Content-Type': 'application/json' };
  if (__ENV.INTERNAL_API_KEY) {
    headers['X-Internal-Token'] = __ENV.INTERNAL_API_KEY;
  }
  return Object.assign({ headers: headers }, extraParams || {});
}

// Pool of 500 virtual players.
var POOL_SIZE = 500;
var players = [];
for (var i = 0; i < POOL_SIZE; i++) {
  players.push('player_' + (i + 1));
}

export function randomUsername() {
  return players[Math.floor(Math.random() * players.length)];
}

export function postScore(username) {
  var res = http.post(
    BASE_URL + '/v1/scores/' + username,
    null,
    authParams({ tags: { name: 'post-score' } })
  );
  check(res, { 'score update 200': function(r) { return r.status === 200; } });
  return res;
}

export function getTopScores() {
  var res = http.get(
    BASE_URL + '/v1/scores',
    authParams({ tags: { name: 'top-scores' } })
  );
  check(res, { 'top scores 200': function(r) { return r.status === 200; } });
  return res;
}

export function getNeighborhood(username) {
  var res = http.get(
    BASE_URL + '/v1/scores/' + username,
    authParams({ tags: { name: 'neighborhood' } })
  );
  check(res, { 'neighborhood 2xx/404': function(r) { return r.status === 200 || r.status === 404; } });
  return res;
}

// generateHtmlReport produces a self-contained HTML summary page.
// Pass its return value directly in handleSummary.
export function generateHtmlReport(data) {
  var metrics = data.metrics;

  function fmt(v, decimals) {
    if (decimals === undefined) { decimals = 2; }
    if (v === undefined || v === null) { return 'n/a'; }
    return typeof v === 'number' ? v.toFixed(decimals) : String(v);
  }

  function metricRow(label, measurement, stat) {
    var m = metrics[measurement];
    if (!m) { return ''; }
    var val = m.values[stat];
    var unit = (measurement.indexOf('duration') !== -1 || measurement.indexOf('waiting') !== -1) ? ' ms' : '';
    return '<tr><td>' + label + '</td><td>' + fmt(val) + unit + '</td></tr>';
  }

  var errorRate = 'n/a';
  if (metrics['http_req_failed']) {
    errorRate = (metrics['http_req_failed'].values['rate'] * 100).toFixed(2);
  }
  var totalReqs = metrics['http_reqs'] ? metrics['http_reqs'].values['count'] : 'n/a';
  var rps = metrics['http_reqs'] ? fmt(metrics['http_reqs'].values['rate']) : 'n/a';

  var checkRows = '';
  var checks = data.root_group.checks || {};
  for (var name in checks) {
    if (!checks.hasOwnProperty(name)) { continue; }
    var c = checks[name];
    var total = c.passes + c.fails;
    var pct = total > 0 ? ((c.passes / total) * 100).toFixed(1) : '0.0';
    var color = c.fails === 0 ? '#4caf50' : '#f44336';
    checkRows += '<tr>' +
      '<td>' + name + '</td>' +
      '<td style="color:' + color + ';font-weight:bold">' + pct + '%</td>' +
      '<td>' + c.passes + '</td>' +
      '<td style="color:' + (c.fails > 0 ? '#f44336' : 'inherit') + '">' + c.fails + '</td>' +
      '</tr>';
  }

  var dur = metrics['http_req_duration'];
  var p95val = dur ? dur.values['p(95)'] : 0;
  var p99val = dur ? dur.values['p(99)'] : 0;
  var p95class = p95val > 500 ? 'warn' : '';
  var p99class = p99val > 1000 ? 'error' : '';
  var errClass = parseFloat(errorRate) > 1 ? 'error' : (parseFloat(errorRate) > 0.1 ? 'warn' : '');

  return '<!DOCTYPE html>\n' +
'<html lang="en">\n' +
'<head>\n' +
'<meta charset="UTF-8">\n' +
'<meta name="viewport" content="width=device-width, initial-scale=1">\n' +
'<title>k6 Load Test Report</title>\n' +
'<style>\n' +
'* { box-sizing: border-box; margin: 0; padding: 0; }\n' +
'body { font-family: system-ui, -apple-system, sans-serif; background: #0f1117; color: #e2e8f0; padding: 2rem; }\n' +
'h1 { font-size: 1.6rem; margin-bottom: 0.25rem; color: #7ee8a2; }\n' +
'.subtitle { color: #64748b; font-size: 0.85rem; margin-bottom: 2rem; }\n' +
'.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem; margin-bottom: 2rem; }\n' +
'.card { background: #1e2330; border-radius: 8px; padding: 1.25rem; border: 1px solid #2d3748; }\n' +
'.card .label { font-size: 0.75rem; color: #64748b; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.5rem; }\n' +
'.card .value { font-size: 2rem; font-weight: 700; color: #7ee8a2; }\n' +
'.card .value.warn { color: #fbbf24; } .card .value.error { color: #f87171; }\n' +
'h2 { font-size: 1.1rem; color: #94a3b8; margin-bottom: 0.75rem; margin-top: 1.5rem; }\n' +
'table { width: 100%; border-collapse: collapse; background: #1e2330; border-radius: 8px; overflow: hidden; margin-bottom: 1.5rem; }\n' +
'th { background: #2d3748; color: #94a3b8; font-size: 0.75rem; text-transform: uppercase; padding: 0.75rem 1rem; text-align: left; }\n' +
'td { padding: 0.6rem 1rem; border-top: 1px solid #2d3748; font-size: 0.875rem; }\n' +
'tr:hover td { background: #252d3d; }\n' +
'</style>\n' +
'</head>\n' +
'<body>\n' +
'<h1>k6 Load Test Report</h1>\n' +
'<p class="subtitle">Real-Time Gaming Leaderboard &mdash; ' + new Date().toISOString() + '</p>\n' +
'<div class="grid">\n' +
'  <div class="card"><div class="label">Total Requests</div><div class="value">' + totalReqs + '</div></div>\n' +
'  <div class="card"><div class="label">Requests / sec</div><div class="value">' + rps + '</div></div>\n' +
'  <div class="card"><div class="label">Error Rate</div><div class="value ' + errClass + '">' + errorRate + '%</div></div>\n' +
'  <div class="card"><div class="label">Avg Response Time</div><div class="value">' + fmt(dur ? dur.values['avg'] : null) + ' ms</div></div>\n' +
'  <div class="card"><div class="label">P95 Response Time</div><div class="value ' + p95class + '">' + fmt(p95val) + ' ms</div></div>\n' +
'  <div class="card"><div class="label">P99 Response Time</div><div class="value ' + p99class + '">' + fmt(p99val) + ' ms</div></div>\n' +
'</div>\n' +
'<h2>Response Time Distribution</h2>\n' +
'<table><thead><tr><th>Metric</th><th>Value</th></tr></thead><tbody>\n' +
metricRow('Min', 'http_req_duration', 'min') +
metricRow('Avg', 'http_req_duration', 'avg') +
metricRow('Median (p50)', 'http_req_duration', 'med') +
metricRow('P90', 'http_req_duration', 'p(90)') +
metricRow('P95', 'http_req_duration', 'p(95)') +
metricRow('P99', 'http_req_duration', 'p(99)') +
metricRow('Max', 'http_req_duration', 'max') +
metricRow('Time to First Byte (avg)', 'http_req_waiting', 'avg') +
metricRow('Connect Time (avg)', 'http_req_connecting', 'avg') +
'</tbody></table>\n' +
(checkRows ? '<h2>Check Results</h2>\n<table><thead><tr><th>Check</th><th>Pass Rate</th><th>Passes</th><th>Fails</th></tr></thead><tbody>' + checkRows + '</tbody></table>\n' : '') +
'</body></html>';
}

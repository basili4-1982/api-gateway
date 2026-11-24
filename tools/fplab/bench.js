import http from 'k6/http';
import { check } from 'k6';

// Один прогон = один случай. URL задаётся снаружи:
//   k6 run -e URL=https://127.0.0.1:8443/fp tools/fplab/bench.js
export const options = {
  vus: __ENV.VUS ? Number(__ENV.VUS) : 200,
  duration: __ENV.DUR ? __ENV.DUR : '15s',
  insecureSkipTLSVerify: true,
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const res = http.get(__ENV.URL);
  check(res, { 'status 200': (r) => r.status === 200 });
}

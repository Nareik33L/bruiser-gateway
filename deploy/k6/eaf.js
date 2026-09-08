// Bruiser EAF profile: many agents of one customer against one resource.
// Expect ~1 forwarded execution and a high observed EAF.
//
//   k6 run deploy/k6/eaf.js
//   k6 run -e BASE=http://127.0.0.1:8081 -e N=2000 deploy/k6/eaf.js
import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
  vus: Number(__ENV.VUS || 50),
  duration: __ENV.DURATION || "20s",
  thresholds: {
    http_req_failed: ["rate<0.05"],
  },
};

const BASE = __ENV.BASE || "http://127.0.0.1:8081";
const CUSTOMER = __ENV.CUSTOMER || "1001234";

export default function () {
  const res = http.post(
    `${BASE}/api/holds`,
    JSON.stringify({ event_id: "ars-che" }),
    {
      headers: {
        "Content-Type": "application/json",
        "X-Customer-Id": CUSTOMER,
        "X-Principal-Id": `agent-${__VU}-${__ITER}`,
      },
    }
  );
  check(res, {
    "controlled": (r) => r.status === 200 || r.status === 409 || r.status === 202,
  });
  sleep(0.05);
}

import http from "k6/http";
import { check, group, sleep } from "k6";

// All tunables read from environment variables (set in the TestRun CRD)
// with sensible defaults so the script still works standalone.

// Infrastructure
const ENDPOINTS  = parseInt(__ENV.ENDPOINTS || "10", 10);
const BASE_DOMAIN = __ENV.BASE_DOMAIN;

// Scenario timing & load shape
const SCENARIO_DURATION_SECONDS = parseInt(__ENV.SCENARIO_DURATION_SECONDS || "1200", 10);  // 20m
const WAIT_BETWEEN_SCENARIOS    = parseInt(__ENV.WAIT_BETWEEN_SCENARIOS    || "300",  10);   // 5m
const ARRIVAL_RATE              = parseInt(__ENV.ARRIVAL_RATE              || "26",   10);   // ~50 HTTP req/s
const PRE_ALLOCATED_VUS         = parseInt(__ENV.PRE_ALLOCATED_VUS        || "50",   10);
const MAX_VUS                   = parseInt(__ENV.MAX_VUS                  || "150",  10);
const GRACEFUL_STOP             = __ENV.GRACEFUL_STOP || "30s";

// SLO thresholds — align with giantswarm/giantswarm#35147 recommendations.
const SLO_P95_LATENCY_MS = __ENV.SLO_P95_LATENCY_MS || "500";
const SLO_P99_LATENCY_MS = __ENV.SLO_P99_LATENCY_MS || "1000";
const SLO_ERROR_RATE     = __ENV.SLO_ERROR_RATE      || "0.001";  // 0.1%
const SLO_CHECKS_RATE    = __ENV.SLO_CHECKS_RATE     || "0.95";

function pickEnvoyBaseUrl() {
  const n = Math.floor(Math.random() * ENDPOINTS);
  return `https://onlineboutique.loadtesting-${n}.${BASE_DOMAIN}`;
}

function pickNginxBaseUrl() {
  const n = Math.floor(Math.random() * ENDPOINTS);
  return `https://nginx-onlineboutique-${n}.loadtesting.${BASE_DOMAIN}`;
}

function pickKongBaseUrl() {
  const n = Math.floor(Math.random() * ENDPOINTS);
  return `https://kong-onlineboutique-${n}.loadtesting.${BASE_DOMAIN}`;
}

const PRODUCTS = [
  "OLJCESPC7Z", "66VCHSJNUP", "1YMWWN1N4O", "L9ECAV7KIM",
  "2ZYFJ3GM2N", "0PUK6V6EV0", "LS4PSXUNUM", "9SIQT8TOJO", "6E92ZMYYFZ",
];

const CURRENCIES = ["EUR", "USD", "JPY", "CAD", "GBP", "TRY"];

const FLOWS = [
  { name: "home", weight: 1 },
  { name: "browseProduct", weight: 10 },
  { name: "viewCart", weight: 3 },
  { name: "addToCart", weight: 2 },
  { name: "setCurrency", weight: 2 },
  { name: "checkout", weight: 1 },
];

const SCENARIO_CONFIG = {
  executor: "constant-arrival-rate",
  rate: ARRIVAL_RATE,
  timeUnit: "1s",
  duration: `${SCENARIO_DURATION_SECONDS}s`,
  preAllocatedVUs: PRE_ALLOCATED_VUS,
  maxVUs: MAX_VUS,
  gracefulStop: GRACEFUL_STOP,
};

// Stagger scenario start times to avoid synchronized request bursts; Envoy starts immediately, nginx starts after Envoy's duration
const nginxStartTime = `${SCENARIO_DURATION_SECONDS + WAIT_BETWEEN_SCENARIOS}s`;

export const options = {
  scenarios: {
    envoy_simulation: {
      ...SCENARIO_CONFIG,
      exec: "envoyScenario",
      startTime: "0s",
    },
    nginx_simulation: {
      ...SCENARIO_CONFIG,
      exec: "nginxScenario",
      startTime: nginxStartTime,
    },
  },
  thresholds: {
    // Per-controller latency thresholds aligned with SLO targets from
    // giantswarm/giantswarm#35147 (default: p95 < 500ms, p99 < 1000ms).
    "http_req_duration{scenario:envoy_simulation}": [
      `p(95)<${SLO_P95_LATENCY_MS}`,
      `p(99)<${SLO_P99_LATENCY_MS}`,
    ],
    "http_req_duration{scenario:nginx_simulation}": [
      `p(95)<${SLO_P95_LATENCY_MS}`,
      `p(99)<${SLO_P99_LATENCY_MS}`,
    ],
    // Error rate: default < 0.1% (issue recommends < 0.1% steady state).
    "http_req_failed{scenario:envoy_simulation}": [`rate<${SLO_ERROR_RATE}`],
    "http_req_failed{scenario:nginx_simulation}": [`rate<${SLO_ERROR_RATE}`],
    // HTTP/2 check applies to Envoy; scoped to avoid tainting nginx checks rate.
    "checks{scenario:envoy_simulation}": [`rate>${SLO_CHECKS_RATE}`],
    "checks{scenario:nginx_simulation}": [`rate>${SLO_CHECKS_RATE}`],
  },
};

// --- Utility functions ---

function weightedRandom(items) {
  const total = items.reduce((sum, item) => sum + item.weight, 0);
  let rand = Math.random() * total;
  for (const item of items) {
    rand -= item.weight;
    if (rand <= 0) return item.name;
  }
  return items[items.length - 1].name;
}

function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

function randomInt(min, max) {
  return Math.floor(Math.random() * (max - min + 1)) + min;
}

function thinkTime(minSec, maxSec) {
  sleep(Math.random() * (maxSec - minSec) + minSec);
}

// --- User flow functions ---
// checkHttp2=true for Envoy (HTTP/2 end-to-end), false for NGINX (HTTP/1.1 upstream)

function browseHome(baseUrl, checkHttp2 = true) {
  group("Browse Homepage", function () {
    const res = http.get(`${baseUrl}/`);
    check(res, {
      "homepage status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });
  });
}

function browseProduct(baseUrl, checkHttp2 = true) {
  group("Browse Product", function () {
    const res = http.get(`${baseUrl}/`);
    check(res, {
      "homepage status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });

    thinkTime(1, 3);

    const productId = randomItem(PRODUCTS);
    const productRes = http.get(`${baseUrl}/product/${productId}`);
    check(productRes, {
      "product page status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });
  });
}

function viewCart(baseUrl, checkHttp2 = true) {
  group("View Cart", function () {
    const res = http.get(`${baseUrl}/cart`);
    check(res, {
      "cart page status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });
  });
}

function addToCart(baseUrl, checkHttp2 = true) {
  group("Add to Cart", function () {
    const productId = randomItem(PRODUCTS);

    const productRes = http.get(`${baseUrl}/product/${productId}`);
    check(productRes, {
      "product page status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });

    thinkTime(1, 3);

    const cartRes = http.post(`${baseUrl}/cart`, {
      product_id: productId,
      quantity: String(randomInt(1, 5)),
    });
    check(cartRes, {
      "add to cart redirected to cart page": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });
  });
}

function setCurrencyFlow(baseUrl, checkHttp2 = true) {
  group("Set Currency", function () {
    const res = http.post(`${baseUrl}/setCurrency`, {
      currency_code: randomItem(CURRENCIES),
    });
    check(res, {
      "set currency redirected OK": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
    });
  });
}

function checkoutFlow(baseUrl, checkHttp2 = true) {
  group("Checkout", function () {
    const productId = randomItem(PRODUCTS);
    const cartRes = http.post(`${baseUrl}/cart`, {
      product_id: productId,
      quantity: "1",
    });
    check(cartRes, {
      "add to cart for checkout OK": (r) => r.status === 200,
    });

    thinkTime(1, 3);

    const orderRes = http.post(`${baseUrl}/cart/checkout`, {
      email: `user${randomInt(1, 10000)}@example.com`,
      street_address: "1600 Amphitheatre Parkway",
      zip_code: "94043",
      city: "Mountain View",
      state: "CA",
      country: "United States",
      credit_card_number: "4432801561520454",
      credit_card_expiration_month: "1",
      credit_card_expiration_year: "2027",
      credit_card_cvv: "672",
    });
    check(orderRes, {
      "checkout status 200": (r) => r.status === 200,
      ...(checkHttp2 && { "protocol is HTTP/2": (r) => r.proto === "HTTP/2.0" }),
      "order confirmation page": (r) => r.body.includes("Your order is complete"),
    });
  });
}

// --- Shared flow dispatcher ---

function runFlow(baseUrl, checkHttp2) {
  const flow = weightedRandom(FLOWS);
  switch (flow) {
    case "home":
      browseHome(baseUrl, checkHttp2); 
      break;
    case "browseProduct":
      browseProduct(baseUrl, checkHttp2);
      break;
    case "viewCart":
      viewCart(baseUrl, checkHttp2);
      break;
    case "addToCart":
      addToCart(baseUrl, checkHttp2);
      break;
    case "setCurrency":
      setCurrencyFlow(baseUrl, checkHttp2);
      break;
    case "checkout":
      checkoutFlow(baseUrl, checkHttp2);
      break;
  }
}

// --- Scenario entry points ---

export function envoyScenario() {
  runFlow(pickEnvoyBaseUrl(), true);
}

export function nginxScenario() {
  runFlow(pickNginxBaseUrl(), false);
}

export function kongScenario() {
  runFlow(pickKongBaseUrl(), false);
}

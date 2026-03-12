import http from "k6/http";
import { check, group, sleep } from "k6";

const ENDPOINTS = 10;

// const ENVOY_BASE_DOMAIN = "envoytesting.gaws2.gigantic.io";
// const NGINX_BASE_DOMAIN = "nginxtesting.gaws2.gigantic.io";
const BASE_DOMAIN = "envoyloadtesting.gaws2.gigantic.io";

function pickEnvoyBaseUrl() {
  const n = Math.floor(Math.random() * ENDPOINTS);
  return `https://onlineboutique.loadtesting-${n}.${BASE_DOMAIN}`;
}

function pickNginxBaseUrl() {
  const n = Math.floor(Math.random() * ENDPOINTS);
  return `https://nginx-onlineboutique-${n}.loadtesting.${BASE_DOMAIN}`;
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

// Scenario config shared between both controllers
const SCENARIO_CONFIG = {
  executor: "constant-arrival-rate",
  rate: 26,        // ~1.95 req/iter → ~50 HTTP req/s per controller
  timeUnit: "1s",
  duration: "5m",
  preAllocatedVUs: 50,
  maxVUs: 150,
  gracefulStop: "30s",
};

export const options = {
  scenarios: {
    envoy_simulation: {
      ...SCENARIO_CONFIG,
      exec: "envoyScenario",
    },
    nginx_simulation: {
      ...SCENARIO_CONFIG,
      exec: "nginxScenario",
    },
  },
  thresholds: {
    // Per-controller latency thresholds for direct comparison
    "http_req_duration{scenario:envoy_simulation}": ["p(95)<3000", "p(99)<5000"],
    "http_req_duration{scenario:nginx_simulation}": ["p(95)<3000", "p(99)<5000"],
    "http_req_failed{scenario:envoy_simulation}": ["rate<0.05"],
    "http_req_failed{scenario:nginx_simulation}": ["rate<0.05"],
    // HTTP/2 check only applies to Envoy; scope it to avoid tainting nginx checks rate
    "checks{scenario:envoy_simulation}": ["rate>0.90"],
    "checks{scenario:nginx_simulation}": ["rate>0.90"],
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

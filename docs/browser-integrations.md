# Browser integrations

ProxySieve ships runtime-dependency-free adapters under `integrations/playwright` and
`integrations/puppeteer`. They fetch a short-lived active policy snapshot from the
local control plane, classify requests in the Node process and abort confirmed
blocks before the browser contacts its configured upstream proxy.

Create an enabled client and one-time API key in the Admin Clients workspace. The
key is sent only to the loopback control API. The adapters reject non-loopback
control URLs and never inject client or session metadata into origin requests.

## Playwright

Configure the ProxySieve listener when creating the browser context, then attach
the adapter to either the context or one page:

```js
import { attachProxySieve } from "./integrations/playwright/index.js";

const context = await browser.newContext({
  proxy: { server: "http://127.0.0.1:8080" },
});
const detach = await attachProxySieve(context, {
  controlUrl: "http://127.0.0.1:9090",
  apiKey: process.env.PROXYSIEVE_API_KEY,
  preset: "browser-aggressive",
});

// Use the context, then flush reports and remove the route hook.
await detach();
```

The Playwright adapter calls `route.fallback()` for allowed requests, so other
registered routes may continue handling them. Its own attach/detach lifecycle is
idempotent.

## Puppeteer

Launch Chrome with the ProxySieve listener, then attach to each page:

```js
import puppeteer from "puppeteer";
import { attachProxySieve } from "./integrations/puppeteer/index.js";

const browser = await puppeteer.launch({
  args: ["--proxy-server=http://127.0.0.1:8080"],
});
const page = await browser.newPage();
const detach = await attachProxySieve(page, {
  controlUrl: "http://127.0.0.1:9090",
  apiKey: process.env.PROXYSIEVE_API_KEY,
  preset: "bandwidth-saver",
});
```

The adapter uses Puppeteer's cooperative interception priority. If an application
already owns interception, set `manageInterception: false`; the application must
enable/disable interception itself. Legacy immediate-resolution handlers can still
win before cooperative handlers, so attach ProxySieve before those handlers.

## Presets and policy safety

Available presets are `passthrough`, `browser-lite`, `browser-aggressive`,
`api-only`, `bandwidth-saver` and `privacy-safe`. Presets never block the main
document. `browser-aggressive` combines heavy-resource and known tracker blocking;
`bandwidth-saver` blocks images, media and fonts.

The local evaluator supports `equals`, `any` and `suffix` conditions with
`all`/`any`/`not` composition. Unsupported operators, unavailable attributes,
expired snapshots and ambiguous multi-policy snapshots fail open; the gateway
remains authoritative. Set `policyId` when a client can see multiple active policy
documents. Reports are bounded batches of untrusted estimated telemetry and do not
relax destination security, routing or budgets.

Browser hooks do not see requests served wholly inside a Service Worker and do not
intercept WebSocket frames after the handshake. Test those application-specific
paths separately. For deterministic Playwright filtering, create the context with
`serviceWorkers: "block"`; otherwise Service Worker-owned network traffic is an
explicit fail-open limitation.

## Selenium foundation

Selenium works in standard proxy-only mode without an SDK:

```python
from selenium import webdriver

options = webdriver.ChromeOptions()
options.add_argument("--proxy-server=http://127.0.0.1:8080")
driver = webdriver.Chrome(options=options)
```

This path receives gateway host-level policy, accounting and routing but cannot
classify encrypted URL paths or resource types before a CONNECT tunnel. A future
CDP/BiDi adapter can reuse the version-1 policy and report endpoints; ProxySieve
does not ship browser-version-specific Selenium hooks in the Go core.

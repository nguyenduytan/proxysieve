import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";

import { attachProxySieve as attachPlaywright } from "../playwright/index.js";
import { attachProxySieve as attachPuppeteer } from "../puppeteer/index.js";

function options() {
  const reports = [];
  return {
    reports,
    value: {
      controlUrl: "http://localhost:9090",
      apiKey: `psk_${"a".repeat(43)}`,
      preset: "bandwidth-saver",
      sessionId: "browser-session",
      fetch: async (url, request = {}) => {
        if (String(url).endsWith("/policy")) {
          return Response.json({
            version: 1,
            revision: 1,
            expires_at: new Date(Date.now() + 300_000).toISOString(),
            client_id: "client",
            policies: [],
          });
        }
        reports.push(JSON.parse(request.body));
        return Response.json(
          { accepted: 1, recording_failures: 0 },
          { status: 202 },
        );
      },
    },
  };
}

test("Playwright adapter aborts blocked requests and unregisters its route", async () => {
  const fixture = options();
  let handler;
  let removed;
  const target = {
    async route(pattern, value) {
      assert.equal(pattern, "**/*");
      handler = value;
    },
    async unroute(pattern, value) {
      removed = pattern === "**/*" && value === handler;
    },
  };
  const detach = await attachPlaywright(target, fixture.value);
  const state = { aborted: false, continued: false };
  await handler({
    request: () => ({
      url: () => "https://example.invalid/a.png",
      method: () => "GET",
      resourceType: () => "image",
    }),
    abort: async () => {
      state.aborted = true;
    },
    fallback: async () => {
      state.continued = true;
    },
  });
  await detach();
  await detach();
  assert.deepEqual(state, { aborted: true, continued: false });
  assert.equal(removed, true);
  assert.equal(fixture.reports.length, 1);
});

test("Puppeteer adapter cooperates with interception and restores owned state", async () => {
  const fixture = options();
  const states = [];
  class Page extends EventEmitter {
    async setRequestInterception(value) {
      states.push(value);
    }
  }
  const page = new Page();
  const detach = await attachPuppeteer(page, fixture.value);
  let aborted = false;
  page.emit("request", {
    url: () => "https://example.invalid/a.woff2",
    method: () => "GET",
    resourceType: () => "font",
    isInterceptResolutionHandled: () => false,
    abort: async () => {
      aborted = true;
    },
    continue: async () => assert.fail("blocked request continued"),
  });
  await new Promise((resolve) => setImmediate(resolve));
  await detach();
  assert.equal(aborted, true);
  assert.deepEqual(states, [true, false]);
  assert.equal(fixture.reports.length, 1);
});

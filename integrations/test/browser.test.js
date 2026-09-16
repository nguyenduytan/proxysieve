import assert from "node:assert/strict";
import test from "node:test";

import { createBrowserIntegration } from "../browser/index.js";

function policySnapshot(policies) {
  return {
    version: 1,
    revision: 3,
    expires_at: new Date(Date.now() + 300_000).toISOString(),
    client_id: "browser-client",
    policies,
  };
}

function blockPolicy(operator = "equals") {
  return {
    version: 1,
    id: "browser-policy",
    name: "Browser policy",
    rules: [
      {
        id: "block-images",
        name: "Block images",
        priority: 100,
        enabled: true,
        stop_processing: true,
        conditions: { field: "resource_type", operator, values: ["image"] },
        actions: [{ type: "block" }],
      },
    ],
  };
}

function fixture(snapshot = policySnapshot([blockPolicy()])) {
  const calls = [];
  const fetch = async (url, options = {}) => {
    calls.push({ url: String(url), options });
    if (String(url).endsWith("/policy")) return Response.json(snapshot);
    return Response.json(
      { accepted: 1, recording_failures: 0 },
      { status: 202 },
    );
  };
  return { calls, fetch };
}

test("policy block is classified and reported with control-plane metadata only", async () => {
  const { calls, fetch } = fixture();
  const integration = await createBrowserIntegration({
    controlUrl: "http://127.0.0.1:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    sessionId: "browser-session",
    fetch,
  });
  const request = {
    url: "https://assets.example.invalid/image.png?token=secret",
    method: "GET",
    resourceType: "image",
  };
  const decision = await integration.classify(request);
  assert.deepEqual(decision, {
    block: true,
    source: "policy",
    policyId: "browser-policy",
    ruleId: "block-images",
  });
  integration.report(request, decision, 2048);
  await integration.close();

  assert.equal(calls.length, 2);
  assert.equal(
    calls[0].options.headers.Authorization.startsWith("Bearer psk_"),
    true,
  );
  const report = JSON.parse(calls[1].options.body);
  assert.deepEqual(report.blocks[0], {
    host: "assets.example.invalid",
    resource_type: "image",
    session_id: "browser-session",
    policy_id: "browser-policy",
    rule_id: "block-images",
    estimated_bytes: 2048,
  });
  assert.equal(calls[1].options.body.includes("token=secret"), false);
});

test("visible HTTP modifiers do not hide a later browser block", async () => {
  const document = blockPolicy();
  document.rules[0].actions = [
    { type: "cache" },
    { type: "rewrite", value: "/small.png" },
    { type: "throttle", value: "1024" },
    { type: "block" },
  ];
  const { fetch } = fixture(policySnapshot([document]));
  const integration = await createBrowserIntegration({
    controlUrl: "http://localhost:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    fetch,
  });
  const decision = await integration.classify({
    url: "https://example.invalid/image.png",
    resourceType: "image",
  });
  assert.equal(decision.block, true);
  await integration.close();
});

test("unsupported policy semantics and ambiguous policy sets fail open", async () => {
  const unsupported = fixture(policySnapshot([blockPolicy("regex")]));
  const first = await createBrowserIntegration({
    controlUrl: "http://localhost:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    fetch: unsupported.fetch,
  });
  assert.deepEqual(
    await first.classify({
      url: "https://example.invalid/a",
      resourceType: "image",
    }),
    { block: false },
  );
  await first.close();

  const ambiguous = fixture(
    policySnapshot([blockPolicy(), { ...blockPolicy(), id: "second-policy" }]),
  );
  const second = await createBrowserIntegration({
    controlUrl: "http://localhost:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    fetch: ambiguous.fetch,
  });
  assert.deepEqual(
    await second.classify({
      url: "https://example.invalid/a",
      resourceType: "image",
    }),
    { block: false },
  );
  await second.close();
});

test("browser-aggressive blocks heavy and tracker requests but preserves documents", async () => {
  const { fetch } = fixture(policySnapshot([]));
  const integration = await createBrowserIntegration({
    controlUrl: "http://[::1]:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    preset: "browser-aggressive",
    fetch,
  });
  assert.equal(
    (
      await integration.classify({
        url: "https://example.invalid/hero.jpg",
        resourceType: "image",
      })
    ).block,
    true,
  );
  assert.equal(
    (
      await integration.classify({
        url: "https://stats.google-analytics.com/a",
        resourceType: "script",
      })
    ).block,
    true,
  );
  assert.equal(
    (
      await integration.classify({
        url: "https://example.invalid/",
        resourceType: "document",
      })
    ).block,
    false,
  );
  assert.equal(
    (
      await integration.classify({
        url: "https://example.invalid/api",
        resourceType: "xhr",
      })
    ).block,
    false,
  );
  await integration.close();
});

test("API keys cannot be sent to a non-loopback control URL", async () => {
  await assert.rejects(
    createBrowserIntegration({
      controlUrl: "https://control.example.com",
      apiKey: `psk_${"a".repeat(43)}`,
    }),
    /loopback/,
  );
});

test("an expired snapshot that cannot refresh fails open", async () => {
  let calls = 0;
  const fetch = async () => {
    calls += 1;
    if (calls > 1) throw new Error("control plane offline");
    return Response.json({
      ...policySnapshot([blockPolicy()]),
      expires_at: new Date(Date.now() + 5).toISOString(),
    });
  };
  const integration = await createBrowserIntegration({
    controlUrl: "http://localhost:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    fetch,
  });
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.deepEqual(
    await integration.classify({
      url: "https://example.invalid/a",
      resourceType: "image",
    }),
    { block: false },
  );
  assert.equal(calls, 2);
  await integration.close();
});

test("integration instances keep concurrent session reports isolated", async () => {
  const { calls, fetch } = fixture(policySnapshot([]));
  const shared = {
    controlUrl: "http://localhost:9090",
    apiKey: `psk_${"a".repeat(43)}`,
    preset: "bandwidth-saver",
    fetch,
  };
  const [first, second] = await Promise.all([
    createBrowserIntegration({ ...shared, sessionId: "first-session" }),
    createBrowserIntegration({ ...shared, sessionId: "second-session" }),
  ]);
  const request = {
    url: "https://example.invalid/a.png",
    resourceType: "image",
  };
  first.report(request, await first.classify(request));
  second.report(request, await second.classify(request));
  await Promise.all([first.close(), second.close()]);
  const sessions = calls
    .filter((call) => call.options.method === "POST")
    .map((call) => JSON.parse(call.options.body).blocks[0].session_id)
    .sort();
  assert.deepEqual(sessions, ["first-session", "second-session"]);
});

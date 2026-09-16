const terminalActions = new Set([
  "block",
  "reject",
  "direct",
  "proxy",
  "chain",
  "mock",
  "redirect",
]);

const presetNames = new Set([
  "passthrough",
  "browser-lite",
  "browser-aggressive",
  "api-only",
  "bandwidth-saver",
  "privacy-safe",
]);

const trackerDomains = [
  "doubleclick.net",
  "google-analytics.com",
  "googletagmanager.com",
  "facebook.net",
  "hotjar.com",
  "segment.io",
];

export async function createBrowserIntegration(options) {
  const controlUrl = localControlURL(options?.controlUrl);
  const apiKey = options?.apiKey;
  const preset = options?.preset ?? "passthrough";
  if (
    typeof apiKey !== "string" ||
    !apiKey.startsWith("psk_") ||
    !presetNames.has(preset)
  ) {
    throw new TypeError("A ProxySieve API key and valid preset are required");
  }

  const fetcher = options.fetch ?? globalThis.fetch;
  if (typeof fetcher !== "function")
    throw new TypeError("fetch is unavailable");
  const sessionId = options.sessionId ?? crypto.randomUUID();
  if (!validID(sessionId)) throw new TypeError("sessionId is invalid");

  let snapshot;
  let refreshAt = 0;
  let refreshPromise;
  let closed = false;
  let timer;
  const reports = [];

  async function refresh(strict = false) {
    if (refreshPromise) return refreshPromise;
    refreshPromise = (async () => {
      try {
        const response = await fetcher(
          new URL("api/v1/browser/policy", controlUrl),
          {
            headers: { Authorization: `Bearer ${apiKey}` },
          },
        );
        if (!response.ok)
          throw new Error(
            `ProxySieve policy request failed (${response.status})`,
          );
        const next = await response.json();
        if (!validSnapshot(next))
          throw new Error("ProxySieve returned an invalid policy snapshot");
        snapshot = next;
        refreshAt = Math.min(Date.parse(next.expires_at), Date.now() + 60_000);
      } catch (error) {
        if (!snapshot || Date.parse(snapshot.expires_at) <= Date.now())
          snapshot = undefined;
        if (strict) throw error;
      } finally {
        refreshPromise = undefined;
      }
    })();
    return refreshPromise;
  }

  async function classify(request) {
    if (closed) return allow();
    if (!snapshot || Date.now() >= refreshAt) await refresh();
    const normalized = normalizeRequest(
      request,
      snapshot?.client_id,
      options.listener,
    );
    if (!normalized || normalized.resourceType === "document") return allow();

    const presetDecision = classifyPreset(preset, normalized);
    if (presetDecision.block) return presetDecision;
    if (!snapshot || Date.parse(snapshot.expires_at) <= Date.now())
      return allow();

    const policies = options.policyId
      ? snapshot.policies.filter((item) => item.id === options.policyId)
      : snapshot.policies.length === 1
        ? snapshot.policies
        : [];
    for (const document of policies) {
      const decision = classifyPolicy(document, normalized);
      if (decision) return decision;
    }
    return allow();
  }

  function report(request, decision, estimatedBytes = 0) {
    if (closed || !decision.block || reports.length >= 100) return;
    const url = safeURL(request.url);
    if (!url) return;
    const estimate = Number(estimatedBytes);
    reports.push({
      host: url.hostname,
      resource_type: request.resourceType,
      session_id: sessionId,
      policy_id: decision.policyId ?? "",
      rule_id: decision.ruleId ?? "",
      estimated_bytes:
        Number.isSafeInteger(estimate) && estimate >= 0 ? estimate : 0,
    });
    if (reports.length === 1)
      timer = setTimeout(flush, options.reportIntervalMs ?? 1000);
    if (reports.length === 100) void flush();
  }

  async function flush() {
    clearTimeout(timer);
    timer = undefined;
    if (reports.length === 0) return;
    const blocks = reports.splice(0, 100);
    try {
      await fetcher(new URL("api/v1/browser/blocks", controlUrl), {
        method: "POST",
        headers: {
          Authorization: `Bearer ${apiKey}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ version: 1, blocks }),
      });
    } catch {
      // Reporting is best effort and must not change the browser route decision.
    }
  }

  async function close() {
    if (closed) return;
    closed = true;
    await flush();
  }

  await refresh(true);
  return { classify, report, flush, close, sessionId };
}

function classifyPolicy(document, request) {
  const rules = document.rules
    .map((rule, index) => ({ rule, index }))
    .sort(
      (left, right) =>
        right.rule.priority - left.rule.priority || left.index - right.index,
    );
  const actions = [];
  let terminalRule;
  for (const { rule } of rules) {
    if (!rule.enabled || matchCondition(rule.conditions, request) !== 1)
      continue;
    if (
      !terminalRule &&
      rule.actions.some((action) => terminalActions.has(action.type))
    ) {
      terminalRule = rule.id;
    }
    actions.push(...rule.actions);
    if (rule.stop_processing) break;
  }
  const terminal = actions.find((action) => terminalActions.has(action.type));
  if (!terminal) return undefined;
  return terminal.type === "block"
    ? {
        block: true,
        source: "policy",
        policyId: document.id,
        ruleId: terminalRule,
      }
    : allow();
}

function matchCondition(condition, request) {
  if (!condition || Object.keys(condition).length === 0) return 1;
  if (Array.isArray(condition.all) && condition.all.length > 0) {
    let result = 1;
    for (const child of condition.all) {
      const state = matchCondition(child, request);
      if (state === 0) return 0;
      if (state === -1) result = -1;
    }
    return result;
  }
  if (Array.isArray(condition.any) && condition.any.length > 0) {
    let result = 0;
    for (const child of condition.any) {
      const state = matchCondition(child, request);
      if (state === 1) return 1;
      if (state === -1) result = -1;
    }
    return result;
  }
  if (condition.not) {
    const state = matchCondition(condition.not, request);
    return state === -1 ? -1 : state === 1 ? 0 : 1;
  }
  const value = request[condition.field];
  if (value === undefined || !Array.isArray(condition.values)) return -1;
  if (condition.operator === "equals" || condition.operator === "any") {
    return condition.values.includes(value) ? 1 : 0;
  }
  if (condition.operator === "suffix") {
    const actual = value.toLowerCase();
    return condition.values.some((wanted) =>
      actual.endsWith(String(wanted).toLowerCase()),
    )
      ? 1
      : 0;
  }
  return -1;
}

function classifyPreset(preset, request) {
  const resource = request.resourceType;
  const bandwidth =
    resource === "image" || resource === "media" || resource === "font";
  const tracker = trackerDomains.some(
    (domain) => request.host === domain || request.host.endsWith(`.${domain}`),
  );
  const block =
    (preset === "browser-lite" && resource === "media") ||
    ((preset === "bandwidth-saver" || preset === "browser-aggressive") &&
      bandwidth) ||
    ((preset === "privacy-safe" || preset === "browser-aggressive") &&
      tracker) ||
    (preset === "api-only" &&
      !["xhr", "fetch", "websocket", "eventsource"].includes(resource));
  return block ? { block: true, source: "preset" } : allow();
}

function normalizeRequest(request, client, listener) {
  const url = safeURL(request?.url);
  const resourceType = request?.resourceType;
  if (!url || typeof resourceType !== "string") return undefined;
  return {
    host: url.hostname,
    client,
    listener,
    protocol: url.protocol.slice(0, -1),
    scheme: url.protocol.slice(0, -1),
    method: String(request.method ?? "GET").toUpperCase(),
    path: url.pathname,
    resourceType,
    resource_type: resourceType,
    hour_utc: String(new Date().getUTCHours()).padStart(2, "0"),
  };
}

function validSnapshot(value) {
  return (
    value?.version === 1 &&
    Number.isSafeInteger(value.revision) &&
    value.revision >= 0 &&
    validID(value.client_id) &&
    Number.isFinite(Date.parse(value.expires_at)) &&
    Array.isArray(value.policies)
  );
}

function localControlURL(value) {
  const url = new URL(value);
  if (
    !["http:", "https:"].includes(url.protocol) ||
    !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  ) {
    throw new TypeError("controlUrl must be a loopback HTTP(S) URL");
  }
  if (!url.pathname.endsWith("/")) url.pathname += "/";
  return url;
}

function safeURL(value) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:"
      ? url
      : undefined;
  } catch {
    return undefined;
  }
}

function validID(value) {
  return (
    typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(value)
  );
}

function allow() {
  return { block: false };
}

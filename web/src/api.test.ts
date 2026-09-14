import { afterEach, describe, expect, it, vi } from "vitest";
import {
  api,
  ApiError,
  csrfValue,
  discoverSession,
  formatBytes,
  formatConfiguredCosts,
} from "./api";

afterEach(() => vi.unstubAllGlobals());
function mockFetch(...responses: Response[]) {
  vi.stubGlobal("document", { cookie: "proxysieve_csrf=session-bound-value" });
  const fetch = vi.fn();
  for (const response of responses) fetch.mockResolvedValueOnce(response);
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
describe("control-plane client", () => {
  it("leaves the checking state for first-run setup", async () => {
    const fetch = mockFetch(Response.json({ setup_required: true }));
    expect(await discoverSession()).toEqual({ kind: "setup" });
    expect(fetch).toHaveBeenCalledTimes(1);
  });
  it("shows sign-in for an expired session", async () => {
    mockFetch(
      Response.json({ setup_required: false }),
      Response.json(
        { error: { message: "Sign in is required." } },
        { status: 401 },
      ),
    );
    expect(await discoverSession()).toEqual({ kind: "login" });
  });
  it("uses the authenticated identity from the server", async () => {
    const user = { id: "tony", username: "tony", role: "admin", enabled: true };
    mockFetch(Response.json({ setup_required: false }), Response.json(user));
    expect(await discoverSession()).toEqual({ kind: "ready", user });
  });
  it("never switches to demo data on API failure", async () => {
    mockFetch(new Response("<html>not an API</html>", { status: 503 }));
    await expect(discoverSession()).rejects.toBeInstanceOf(ApiError);
  });
  it("preserves structured API error codes for recovery flows", async () => {
    mockFetch(
      Response.json(
        {
          error: {
            code: "POOL_IN_USE",
            message: "Pool is still referenced by another pool.",
          },
        },
        { status: 409 },
      ),
    );

    await expect(api("/api/v1/pools/pool-a")).rejects.toMatchObject({
      status: 409,
      code: "POOL_IN_USE",
      message: "Pool is still referenced by another pool.",
    });
  });
  it("sends same-origin credentials and CSRF on mutations", async () => {
    const fetch = mockFetch(new Response(null, { status: 204 }));
    await api("/api/v1/auth/logout", { method: "POST" });
    const options = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(options.credentials).toBe("same-origin");
    expect(new Headers(options.headers).get("X-CSRF-Token")).toBe(
      "session-bound-value",
    );
  });
  it("reads only the exact CSRF cookie and formats binary byte units", () => {
    expect(csrfValue("unrelated=1; proxysieve_csrf=abc%3D; another=2")).toBe(
      "abc=",
    );
    expect(csrfValue("prefix_proxysieve_csrf=bad")).toBe("");
    expect(formatBytes(1024)).toBe("1.0 KiB");
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(-1)).toBe("—");
    const cost = formatConfiguredCosts([
      {
        amount: { currency: "USD", micros: 1_250_000 },
        priced_upstream_upload_bytes: 1,
        priced_upstream_download_bytes: 2,
      },
    ]);
    expect(cost).toContain("1.25");
    expect(formatConfiguredCosts([])).toBe("—");
  });
});

import assert from "node:assert/strict";
import http from "node:http";
import test from "node:test";

import { chromium } from "playwright-core";
import puppeteer from "puppeteer-core";
import { attachProxySieve as attachPlaywright } from "../playwright/index.js";
import { attachProxySieve as attachPuppeteer } from "../puppeteer/index.js";

for (const adapter of ["playwright", "puppeteer"]) {
  test(`${adapter} browser e2e blocks heavy resources before the upstream proxy`, async (t) => {
    const fixture = await createFixture();
    t.after(fixture.close);
    let browser;
    try {
      browser =
        adapter === "playwright"
          ? await chromium.launch({ channel: "chrome", headless: true })
          : await puppeteer.launch({
              channel: "chrome",
              headless: true,
              args: [`--proxy-server=${fixture.proxyUrl}`],
            });
    } catch (error) {
      if (process.env.CI) throw error;
      t.skip(`Chrome is unavailable: ${error.message}`);
      return;
    }
    t.after(() => browser.close());

    let page;
    let detach;
    const options = {
      controlUrl: fixture.controlUrl,
      apiKey: `psk_${"a".repeat(43)}`,
      preset: "browser-aggressive",
      sessionId: `${adapter}-session`,
      reportIntervalMs: 10,
    };
    if (adapter === "playwright") {
      const context = await browser.newContext({
        proxy: { server: fixture.proxyUrl },
      });
      detach = await attachPlaywright(context, options);
      page = await context.newPage();
    } else {
      page = await browser.newPage();
      detach = await attachPuppeteer(page, options);
    }

    const response = await page.goto(fixture.siteUrl, {
      waitUntil: "domcontentloaded",
    });
    assert.equal(response.ok(), true);
    await page.waitForFunction(() => globalThis.fixtureXHR === "ok");
    await new Promise((resolve) => setTimeout(resolve, 250));
    await detach();

    const paths = fixture.proxyRequests.map((item) => item.pathname);
    assert.equal(paths.includes("/"), true);
    assert.equal(paths.includes("/data"), true);
    for (const blocked of [
      "/image.png",
      "/media.mp4",
      "/font.woff2",
      "/tracker.js",
    ]) {
      assert.equal(
        paths.includes(blocked),
        false,
        `${blocked} reached the upstream proxy`,
      );
    }
    assert.deepEqual(
      new Set(fixture.blockReports.map((item) => item.resource_type)),
      new Set(["image", "media", "font", "script"]),
    );
  });
}

async function createFixture() {
  const proxyRequests = [];
  const blockReports = [];
  const target = http.createServer((request, response) => {
    switch (new URL(request.url, "http://site.test").pathname) {
      case "/":
        response.setHeader("Content-Type", "text/html");
        response.end(
          `<!doctype html><link rel="stylesheet" href="/style.css"><img src="/image.png"><video preload="auto" src="/media.mp4"></video><script src="http://stats.google-analytics.com:${target.address().port}/tracker.js"></script><script>fetch('/data').then(r=>r.text()).then(v=>globalThis.fixtureXHR=v)</script>`,
        );
        break;
      case "/style.css":
        response.setHeader("Content-Type", "text/css");
        response.end(
          "@font-face{font-family:fixture;src:url('/font.woff2')}body{font-family:fixture}",
        );
        break;
      case "/data":
        response.end("ok");
        break;
      default:
        response.end("fixture");
    }
  });
  await listen(target);

  const proxy = http.createServer((request, response) => {
    const destination = new URL(request.url);
    proxyRequests.push({
      hostname: destination.hostname,
      pathname: destination.pathname,
    });
    const upstream = http.request(
      {
        hostname: "127.0.0.1",
        port: target.address().port,
        method: request.method,
        path: destination.pathname + destination.search,
        headers: { ...request.headers, host: destination.host },
      },
      (upstreamResponse) => {
        response.writeHead(
          upstreamResponse.statusCode,
          upstreamResponse.headers,
        );
        upstreamResponse.pipe(response);
      },
    );
    upstream.on("error", () => response.destroy());
    request.pipe(upstream);
  });
  await listen(proxy);

  const control = http.createServer((request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.url === "/api/v1/browser/policy") {
      response.end(
        JSON.stringify({
          version: 1,
          revision: 1,
          expires_at: new Date(Date.now() + 300_000).toISOString(),
          client_id: "browser-client",
          policies: [],
        }),
      );
      return;
    }
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      body += chunk;
    });
    request.on("end", () => {
      blockReports.push(...JSON.parse(body).blocks);
      response.statusCode = 202;
      response.end('{"accepted":1,"recording_failures":0}');
    });
  });
  await listen(control);

  return {
    proxyRequests,
    blockReports,
    proxyUrl: `http://127.0.0.1:${proxy.address().port}`,
    controlUrl: `http://127.0.0.1:${control.address().port}`,
    siteUrl: `http://site.test:${target.address().port}/`,
    close: async () =>
      Promise.all([close(control), close(proxy), close(target)]),
  };
}

function listen(server) {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
}

function close(server) {
  return new Promise((resolve) => server.close(resolve));
}

import { createBrowserIntegration } from "../browser/index.js";

export async function attachProxySieve(target, options) {
  if (!target?.route || !target?.unroute)
    throw new TypeError("A Playwright BrowserContext or Page is required");
  const integration = await createBrowserIntegration(options);
  const handler = async (route) => {
    const request = route.request();
    const input = {
      url: request.url(),
      method: request.method(),
      resourceType: request.resourceType(),
    };
    try {
      const decision = await integration.classify(input);
      if (decision.block) {
        const estimated = options?.estimateBytes
          ? await options.estimateBytes(request)
          : 0;
        integration.report(input, decision, estimated);
        await route.abort("blockedbyclient");
        return;
      }
    } catch {
      // Browser routing stays fail-open when the local integration fails.
    }
    if (route.fallback) await route.fallback();
    else await route.continue();
  };
  await target.route("**/*", handler);
  let detached = false;
  return async () => {
    if (detached) return;
    detached = true;
    await target.unroute("**/*", handler);
    await integration.close();
  };
}

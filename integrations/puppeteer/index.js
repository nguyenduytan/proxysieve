import { createBrowserIntegration } from "../browser/index.js";

export async function attachProxySieve(page, options) {
  if (!page?.setRequestInterception || !page?.on || !page?.off)
    throw new TypeError("A Puppeteer Page is required");
  const integration = await createBrowserIntegration(options);
  const priority = options?.interceptResolutionPriority ?? 0;
  const handler = async (request) => {
    if (request.isInterceptResolutionHandled?.()) return;
    const input = {
      url: request.url(),
      method: request.method(),
      resourceType: request.resourceType(),
    };
    try {
      const decision = await integration.classify(input);
      if (request.isInterceptResolutionHandled?.()) return;
      if (decision.block) {
        const estimated = options?.estimateBytes
          ? await options.estimateBytes(request)
          : 0;
        integration.report(input, decision, estimated);
        await request.abort("blockedbyclient", priority);
        return;
      }
    } catch {
      // Browser routing stays fail-open when the local integration fails.
    }
    if (!request.isInterceptResolutionHandled?.())
      await request.continue({}, priority);
  };
  if (options?.manageInterception !== false)
    await page.setRequestInterception(true);
  page.on("request", handler);
  let detached = false;
  return async () => {
    if (detached) return;
    detached = true;
    page.off("request", handler);
    if (options?.manageInterception !== false)
      await page.setRequestInterception(false);
    await integration.close();
  };
}

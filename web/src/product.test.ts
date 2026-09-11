import { describe, expect, it } from "vitest";
import { product } from "./product";

describe("bootstrap product metadata", () => {
  it("credits the creator", () => {
    expect(product.author).toBe("Tony Nguyen");
    expect(product.name).toBe("ProxySieve");
    expect(product.license).toBe("Apache-2.0");
  });

  it("reports an immutable local foundation identity", () => {
    expect(product.stage).toBe("local-gateway-foundation");
    expect(Object.isFrozen(product)).toBe(true);
  });
});

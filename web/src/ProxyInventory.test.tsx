import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ProxyEditor, ProxyInventory } from "./ProxyInventory";

describe("proxy inventory permissions", () => {
  it("requires explicit remote DNS trust and preserves saved editor values", () => {
    const callbacks = { onSaved() {}, onCancel() {}, onExpired() {} };
    const fresh = renderToStaticMarkup(<ProxyEditor {...callbacks} />);
    expect(fresh).toContain("Trust this proxy&#x27;s remote DNS");
    expect(fresh).not.toContain('checked=""');
    const saved = renderToStaticMarkup(
      <ProxyEditor
        {...callbacks}
        initial={{
          revision: 4,
          endpoint: {
            id: "upstream",
            name: "Approved upstream",
            protocol: "https",
            host: "proxy.example.com",
            port: 8443,
            enabled: false,
            trusted_remote_dns: true,
          },
        }}
      />,
    );
    expect(saved).toContain("Edit endpoint metadata");
    expect(saved).toContain('value="proxy.example.com"');
    expect(saved).toContain('value="8443"');
    expect(saved).toContain('checked=""');
  });
  it("shows inventory mutation controls to operators", () => {
    const html = renderToStaticMarkup(
      <ProxyInventory role="operator" onExpired={() => {}} />,
    );
    expect(html).toContain("Preview import");
    expect(html).toContain("Add proxy");
  });

  it("keeps mutation controls hidden from viewers", () => {
    const html = renderToStaticMarkup(
      <ProxyInventory role="viewer" onExpired={() => {}} />,
    );
    expect(html).not.toContain("Preview import");
    expect(html).not.toContain("Add proxy");
  });
});

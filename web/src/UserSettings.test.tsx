import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { UserSettings } from "./UserSettings";

describe("user settings workspace", () => {
  it("renders an admin-only account management surface without duplicate alerts", () => {
    const html = renderToStaticMarkup(
      <UserSettings currentUserID="admin" onExpired={() => {}} />,
    );
    expect(html).toContain("User accounts");
    expect(html).toContain("Add user");
    expect(html).not.toContain("Alerts");
  });
});

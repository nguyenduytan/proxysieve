import type { ComponentType } from "react";
import {
  Activity,
  DatabaseZap,
  Globe2,
  KeyRound,
  Layers3,
  Fingerprint,
  Route,
  LayoutDashboard,
  ScrollText,
  Settings,
} from "lucide-react";
import type { Role } from "./api";

export type Page =
  | "Overview"
  | "Traffic"
  | "Proxies"
  | "Sources"
  | "Pools"
  | "Sessions"
  | "Policies"
  | "Clients"
  | "Audit"
  | "System";

export interface NavigationItem {
  page: Page;
  label: string;
  description: string;
  icon: ComponentType<{ size?: number }>;
  roles?: readonly Role[];
}

// The sidebar is a product navigation surface, not a roadmap. Only working,
// API-backed destinations belong here.
export const navigationItems: readonly NavigationItem[] = Object.freeze([
  {
    page: "Overview",
    label: "Overview",
    description: "Runtime summary",
    icon: LayoutDashboard,
  },
  {
    page: "Traffic",
    label: "Traffic",
    description: "Live and retained gateway events",
    icon: Activity,
  },
  {
    page: "Proxies",
    label: "Proxies",
    description: "Saved endpoint inventory",
    icon: Globe2,
  },
  {
    page: "Sources",
    label: "Sources",
    description: "Scheduled proxy feeds",
    icon: DatabaseZap,
  },
  {
    page: "Pools",
    label: "Pools",
    description: "Revisioned routing groups",
    icon: Layers3,
  },
  {
    page: "Sessions",
    label: "Sessions",
    description: "Runtime proxy affinity",
    icon: Fingerprint,
  },
  {
    page: "Policies",
    label: "Policies",
    description: "Revisioned routing rules",
    icon: Route,
  },
  {
    page: "Clients",
    label: "Clients",
    description: "Downstream clients and API keys",
    icon: KeyRound,
    roles: ["admin"],
  },
  {
    page: "Audit",
    label: "Audit",
    description: "Administrative activity history",
    icon: ScrollText,
    roles: ["admin"],
  },
  {
    page: "System",
    label: "System",
    description: "Build and runtime information",
    icon: Settings,
  },
]);

export function navigationForRole(role: Role): readonly NavigationItem[] {
  return navigationItems.filter(
    (item) => item.roles === undefined || item.roles.includes(role),
  );
}

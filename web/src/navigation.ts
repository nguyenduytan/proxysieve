import type { ComponentType } from "react";
import {
  Activity,
  DatabaseZap,
  Globe2,
  KeyRound,
  Layers3,
  Link2,
  Fingerprint,
  Route,
  LayoutDashboard,
  ScrollText,
  Settings,
  UsersRound,
  HeartPulse,
  Gauge,
  HardDrive,
  RadioTower,
  BellRing,
} from "lucide-react";
import type { Role } from "./api";

export type Page =
  | "Overview"
  | "Traffic"
  | "Events"
  | "Alerts"
  | "Budgets"
  | "Cache"
  | "Proxies"
  | "Sources"
  | "Pools"
  | "Chains"
  | "Health"
  | "Sessions"
  | "Policies"
  | "Clients"
  | "Users"
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
    page: "Events",
    label: "Events",
    description: "Live control-plane activity",
    icon: RadioTower,
  },
  {
    page: "Alerts",
    label: "Alerts",
    description: "Rules and webhook delivery",
    icon: BellRing,
    roles: ["admin"],
  },
  {
    page: "Budgets",
    label: "Budgets",
    description: "Active limits and usage",
    icon: Gauge,
  },
  {
    page: "Cache",
    label: "Cache",
    description: "Response cache usage and controls",
    icon: HardDrive,
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
    page: "Chains",
    label: "Chains",
    description: "Ordered multi-proxy routes",
    icon: Link2,
  },
  {
    page: "Health",
    label: "Health",
    description: "Proxy and pool availability",
    icon: HeartPulse,
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
    page: "Users",
    label: "Users",
    description: "Administrator accounts and roles",
    icon: UsersRound,
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

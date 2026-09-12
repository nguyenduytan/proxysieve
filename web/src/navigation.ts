import type { ComponentType } from "react";
import {
  Activity,
  DatabaseZap,
  Globe2,
  LayoutDashboard,
  Settings,
} from "lucide-react";

export type Page = "Overview" | "Traffic" | "Proxies" | "Sources" | "System";

export interface NavigationItem {
  page: Page;
  label: string;
  description: string;
  icon: ComponentType<{ size?: number }>;
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
    page: "System",
    label: "System",
    description: "Build and runtime information",
    icon: Settings,
  },
]);

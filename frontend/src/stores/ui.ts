import { defineStore } from "pinia";
import type { AppearanceSettings } from "@/types/api";

export type ThemeMode = "system" | "light" | "dark";
export type ToastTone = "success" | "warning" | "danger" | "info";

export interface ToastItem {
  id: number;
  title: string;
  message?: string;
  tone: ToastTone;
}

interface ConfirmState {
  open: boolean;
  title: string;
  message: string;
  confirmLabel: string;
  danger: boolean;
  resolve?: (accepted: boolean) => void;
}

let toastId = 0;

export const useUiStore = defineStore("ui", {
  state: () => ({
    theme: "system" as ThemeMode,
    sidebarCompact: false,
    mobileSidebarOpen: false,
    commandPaletteOpen: false,
    toasts: [] as ToastItem[],
    confirm: {
      open: false,
      title: "",
      message: "",
      confirmLabel: "确认",
      danger: false,
    } as ConfirmState,
  }),
  actions: {
    initializeTheme() {
      const saved = localStorage.getItem("mcp-devdesk-theme") as ThemeMode | null;
      this.setTheme(saved ?? "system");
    },
    setTheme(theme: ThemeMode) {
      this.theme = theme;
      localStorage.setItem("mcp-devdesk-theme", theme);
      const resolved = theme === "system"
        ? (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
        : theme;
      document.documentElement.dataset.theme = resolved;
    },
    applyAppearance(settings: AppearanceSettings) {
      this.setTheme(settings.theme);
      const root = document.documentElement;
      if (settings.customColorsEnabled) {
        const resolved = root.dataset.theme === "dark" ? "dark" : "light";
        root.style.setProperty("--accent", settings.primaryColor);
        root.style.setProperty("--accent-hover", `color-mix(in srgb, ${settings.primaryColor} 82%, ${resolved === "dark" ? "white" : "black"})`);
        root.style.setProperty("--accent-soft", `color-mix(in srgb, ${settings.primaryColor} 15%, transparent)`);
        root.style.setProperty("--indigo", settings.secondaryColor);
        root.style.setProperty("--indigo-soft", `color-mix(in srgb, ${settings.secondaryColor} 15%, transparent)`);
      } else {
        for (const property of ["--accent", "--accent-hover", "--accent-soft", "--indigo", "--indigo-soft"]) {
          root.style.removeProperty(property);
        }
      }
      root.style.setProperty("--appearance-background-opacity", String(Math.max(0, Math.min(100, settings.backgroundOpacity)) / 100));
      root.style.setProperty(
        "--appearance-background-image",
        settings.hasBackgroundImage
          ? `url("/api/appearance/background?rev=${encodeURIComponent(String(settings.backgroundRevision))}")`
          : "none",
      );
    },
    toast(title: string, message = "", tone: ToastTone = "success") {
      const id = ++toastId;
      this.toasts.push({ id, title, message, tone });
      window.setTimeout(() => this.dismissToast(id), 4200);
    },
    dismissToast(id: number) {
      this.toasts = this.toasts.filter((toast) => toast.id !== id);
    },
    ask(options: { title: string; message: string; confirmLabel?: string; danger?: boolean }) {
      return new Promise<boolean>((resolve) => {
        this.confirm = {
          open: true,
          title: options.title,
          message: options.message,
          confirmLabel: options.confirmLabel ?? "确认",
          danger: Boolean(options.danger),
          resolve,
        };
      });
    },
    resolveConfirm(accepted: boolean) {
      this.confirm.resolve?.(accepted);
      this.confirm = {
        open: false,
        title: "",
        message: "",
        confirmLabel: "确认",
        danger: false,
      };
    },
  },
});

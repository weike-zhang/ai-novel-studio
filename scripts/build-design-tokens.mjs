import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const sourcePath = path.join(root, "design-system/tokens/design-tokens.json");
const outputPath = path.join(root, "design-system/tokens/tokens.css");
const source = JSON.parse(await readFile(sourcePath, "utf8"));

const kebab = (value) => value.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
const leaves = [];
const visit = (value, segments = []) => {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    leaves.push([`--jj-${segments.map(kebab).join("-")}`, String(value)]);
    return;
  }
  for (const [key, child] of Object.entries(value)) {
    if (key.startsWith("$") || ["name", "version", "source"].includes(key)) continue;
    visit(child, [...segments, key]);
  }
};
visit(source);

const alias = {
  "--jj-color-brand-ink": "--jj-primitive-color-brand-ink",
  "--jj-color-brand-rust": "--jj-primitive-color-brand-rust",
  "--jj-color-brand-rust-hover": "--jj-primitive-color-brand-rust-hover",
  "--jj-color-brand-rust-pressed": "--jj-primitive-color-brand-rust-pressed",
  "--jj-color-brand-rust-soft": "--jj-primitive-color-brand-rust-soft",
  "--jj-color-brand-moss": "--jj-primitive-color-brand-moss",
  "--jj-color-neutral-muted": "--jj-primitive-color-neutral-muted",
  "--jj-color-surface-canvas": "--jj-semantic-surface-canvas",
  "--jj-color-surface-default": "--jj-semantic-surface-default",
  "--jj-color-surface-muted": "--jj-semantic-surface-muted",
  "--jj-color-surface-inverse": "--jj-semantic-surface-inverse",
  "--jj-color-text-primary": "--jj-semantic-text-primary",
  "--jj-color-text-secondary": "--jj-semantic-text-secondary",
  "--jj-color-text-tertiary": "--jj-semantic-text-tertiary",
  "--jj-color-text-disabled": "--jj-semantic-text-disabled",
  "--jj-color-text-inverse": "--jj-semantic-text-inverse",
  "--jj-color-border-default": "--jj-semantic-border-default",
  "--jj-color-border-strong": "--jj-semantic-border-strong",
  "--jj-color-border-subtle": "--jj-semantic-border-subtle",
  "--jj-color-success": "--jj-primitive-color-semantic-success",
  "--jj-color-warning": "--jj-primitive-color-semantic-warning",
  "--jj-color-danger": "--jj-primitive-color-semantic-danger",
  "--jj-color-info": "--jj-primitive-color-semantic-info",
  "--jj-color-success-soft": "--jj-primitive-color-semantic-soft-success",
  "--jj-color-warning-soft": "--jj-primitive-color-semantic-soft-warning",
  "--jj-color-danger-soft": "--jj-primitive-color-semantic-soft-danger",
  "--jj-color-info-soft": "--jj-primitive-color-semantic-soft-info",
  "--jj-focus-ring": "--jj-semantic-focus-ring",
  "--jj-font-sans": "--jj-primitive-typography-font-family-sans",
  "--jj-font-reading": "--jj-primitive-typography-font-family-reading",
  "--jj-size-caption": "--jj-primitive-typography-size-caption",
  "--jj-size-label": "--jj-primitive-typography-size-label",
  "--jj-size-body": "--jj-primitive-typography-size-body",
  "--jj-size-body-large": "--jj-primitive-typography-size-body-large",
  "--jj-size-micro": "--jj-primitive-typography-size-micro",
  "--jj-size-tiny": "--jj-primitive-typography-size-tiny",
  "--jj-size-reading-mobile": "--jj-primitive-typography-size-reading-mobile",
  "--jj-size-reading-desktop": "--jj-primitive-typography-size-reading-desktop",
  "--jj-size-title-mobile": "--jj-primitive-typography-size-title-mobile",
  "--jj-size-display-mobile": "--jj-primitive-typography-size-display-mobile",
  "--jj-size-display-desktop": "--jj-primitive-typography-size-display-desktop",
  "--jj-leading-tight": "--jj-primitive-typography-line-height-tight",
  "--jj-leading-body": "--jj-primitive-typography-line-height-body",
  "--jj-leading-reading": "--jj-primitive-typography-line-height-reading",
  "--jj-weight-regular": "--jj-primitive-typography-weight-regular",
  "--jj-weight-medium": "--jj-primitive-typography-weight-medium",
  "--jj-weight-semibold": "--jj-primitive-typography-weight-semibold",
  "--jj-weight-bold": "--jj-primitive-typography-weight-bold",
  "--jj-radius-control": "--jj-primitive-radius-control",
  "--jj-radius-card": "--jj-primitive-radius-card",
  "--jj-radius-chip": "--jj-primitive-radius-chip",
  "--jj-radius-composer": "--jj-primitive-radius-composer",
  "--jj-radius-pill": "--jj-primitive-radius-pill",
  "--jj-space-1": "--jj-primitive-spacing-1",
  "--jj-space-2": "--jj-primitive-spacing-2",
  "--jj-space-3": "--jj-primitive-spacing-3",
  "--jj-space-4": "--jj-primitive-spacing-4",
  "--jj-space-5": "--jj-primitive-spacing-5",
  "--jj-space-6": "--jj-primitive-spacing-6",
  "--jj-space-8": "--jj-primitive-spacing-8",
  "--jj-space-10": "--jj-primitive-spacing-10",
  "--jj-space-12": "--jj-primitive-spacing-12",
  "--jj-space-14": "--jj-primitive-spacing-14",
  "--jj-space-16": "--jj-primitive-spacing-16",
  "--jj-shadow-hairline": "--jj-primitive-elevation-hairline",
  "--jj-shadow-surface": "--jj-primitive-elevation-surface",
  "--jj-shadow-popover": "--jj-primitive-elevation-popover",
  "--jj-motion-fast": "--jj-primitive-motion-fast",
  "--jj-motion-standard": "--jj-primitive-motion-standard",
  "--jj-motion-easing": "--jj-primitive-motion-easing",
  "--jj-control-height": "--jj-component-control-height",
  "--jj-touch-height": "--jj-component-control-touch-height",
  "--jj-chip-height": "--jj-component-chip-height",
  "--jj-content-max": "--jj-semantic-layout-content-max",
  "--jj-reading-max": "--jj-semantic-layout-reading-max",
  "--jj-story-card-radius": "--jj-component-story-card-radius",
  "--jj-story-card-padding": "--jj-component-story-card-padding",
  "--jj-story-card-cover-height": "--jj-component-story-card-cover-height",
  "--jj-story-card-cover-height-mobile": "--jj-component-story-card-cover-height-mobile",
  "--jj-story-card-grid-gap": "--jj-component-story-card-grid-gap",
  "--jj-story-card-section-gap": "--jj-component-story-card-section-gap",
  "--jj-story-card-content-gap": "--jj-component-story-card-content-gap",
  "--jj-story-card-title-gap": "--jj-component-story-card-title-gap",
  "--jj-story-card-meta-gap": "--jj-component-story-card-meta-gap",
  "--jj-story-card-hook-min-height": "--jj-component-story-card-hook-min-height",
  "--jj-story-card-promise-min-height": "--jj-component-story-card-promise-min-height",
  "--jj-story-card-action-height": "--jj-component-story-card-action-height",
  "--jj-story-card-border-width": "--jj-component-story-card-border-width"
};

const declarations = [
  ...leaves.map(([name, value]) => `  ${name}: ${value};`),
  ...Object.entries(alias).map(([name, target]) => `  ${name}: var(${target});`),
].join("\n");

const css = `/* Generated from design-tokens.json. Do not edit by hand. */\n:root {\n${declarations}\n}\n\n*:focus-visible { outline: 2px solid var(--jj-color-brand-rust); outline-offset: 2px; box-shadow: 0 0 0 4px var(--jj-focus-ring); }\n\n@media (prefers-reduced-motion: reduce) {\n  *, *::before, *::after { scroll-behavior: auto !important; transition-duration: 0.01ms !important; animation-duration: 0.01ms !important; animation-iteration-count: 1 !important; }\n}\n`;

if (process.argv.includes("--check")) {
  const current = await readFile(outputPath, "utf8");
  if (current !== css) {
    console.error("Design token CSS is out of date. Run npm run tokens:build.");
    process.exit(1);
  }
} else {
  await writeFile(outputPath, css);
}

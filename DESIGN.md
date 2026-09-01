# MCP DevDesk Design System

## Product character

MCP DevDesk is a Windows desktop control center for local MCP services, Cloudflare Tunnel, permissions, logs and project workspaces.

The interface should feel like a native productivity application rather than a website or an operations dashboard. The desired tone is:

- quiet and precise;
- calm Apple-like minimalism;
- strong information hierarchy;
- soft depth instead of heavy borders;
- desktop-first and keyboard-friendly;
- technical without looking like a terminal theme;
- trustworthy around dangerous actions.

The visual system combines Apple desktop clarity, Linear-like information order and Raycast-like desktop utility patterns.

## Layout

Use a three-part desktop shell:

1. A compact translucent sidebar for global navigation.
2. A flexible primary workspace for the active page.
3. An optional inspector drawer for details, errors and process actions.

The application should remain usable from 1100 × 720 upward. Content width should not be artificially constrained on desktop, but individual reading sections should stay between 640 and 960 pixels when appropriate.

## Spacing

Base spacing unit: `4px`.

Preferred steps:

```text
4, 8, 12, 16, 20, 24, 32, 40, 48, 64
```

Use 20–24px internal padding for primary cards and 14–16px for compact rows. Avoid dense grids where one calm list can communicate the same information.

## Shape

- Main surfaces: 18–22px corner radius.
- Compact controls: 10–12px corner radius.
- Pills and status chips: fully rounded.
- Borders: 1px, low contrast.
- Shadows: soft and broad, never black or harsh.

## Color

### Light theme

```text
Canvas             #F5F5F7
Primary surface    rgba(255,255,255,.82)
Solid surface      #FFFFFF
Raised surface     #FBFBFD
Primary text       #1D1D1F
Secondary text     #6E6E73
Tertiary text      #98989D
Hairline           rgba(29,29,31,.10)
Accent blue        #007AFF
Accent indigo      #5856D6
Success            #248A3D
Warning            #B25000
Danger             #D70015
```

### Dark theme

```text
Canvas             #101114
Primary surface    rgba(30,31,36,.82)
Solid surface      #1C1C1E
Raised surface     #242428
Primary text       #F5F5F7
Secondary text     #AEAEB2
Tertiary text      #73737A
Hairline           rgba(255,255,255,.10)
Accent blue        #0A84FF
Accent indigo      #5E5CE6
Success            #30D158
Warning            #FF9F0A
Danger             #FF453A
```

Accent colors are reserved for status, selection and primary actions. Large saturated backgrounds are not part of the core interface.

## Typography

Use the Windows system UI stack with Apple-compatible fallbacks:

```css
font-family: Inter, "SF Pro Text", "Segoe UI Variable", "Segoe UI", sans-serif;
```

Use `SFMono-Regular`, `Cascadia Code`, `Consolas`, monospace for URLs, ports, process IDs and logs.

Type scale:

```text
Page title          30/36, 650
Section title       18/24, 650
Card value          24/30, 650
Body                14/20, 400
Compact body        13/18, 400
Caption             12/16, 500
Eyebrow             11/14, 650, uppercase optional
```

Avoid all-caps except for very small category labels.

## Navigation

Primary navigation:

- Overview
- Projects
- Services
- Cloudflare
- Logs & diagnostics
- Security
- Settings

Navigation labels must be short and use familiar terms. The active item uses a soft accent background, not a solid saturated rectangle.

## Cards and lists

Cards should group related actions, not decorate every number. Prefer:

- one headline;
- one short explanation;
- a clear status or primary value;
- no more than two actions in the card header.

For process and project collections, use calm row lists with hover selection. Avoid spreadsheet-like tables unless columns are necessary for comparison.

## Buttons

- Primary: accent blue fill, white label.
- Secondary: translucent neutral surface.
- Quiet: text or icon-only.
- Destructive: red label on a soft red surface; solid red only for final confirmation.

Buttons should be 34–38px tall in dense desktop areas and 40–44px in forms.

## Forms

Labels sit above fields. Supporting text appears directly below the control. Keep related fields in one card and expose advanced options progressively.

Dangerous changes such as permission mode, port takeover and process termination require an explanatory confirmation sheet.

## Feedback

- Use inline status strips for persistent warnings.
- Use toast notifications for completed operations.
- Use skeletons for initial loading.
- Use an inspector drawer for detailed errors and process metadata.
- Log source switching should prefer a compact dropdown above the console instead of a permanently tall source sidebar; log consoles should use a bounded scrollable height.
- Never block the whole application for routine refresh operations.

## Motion

Motion is short and functional:

```text
Fast interaction    120–160ms
Panel transition    180–240ms
Drawer transition   220–280ms
```

Use ease-out curves. Respect `prefers-reduced-motion`.

## Windows desktop rules

- Do not imitate macOS traffic-light window controls inside the WebView.
- Use the native Windows title bar supplied by the host application.
- Keep primary actions away from the extreme top-right where native window controls live.
- Use hover and focus states that work with mouse and keyboard.
- Use visible focus rings for all interactive elements.
- Avoid browser-specific language such as “open this webpage”.

## Page responsibilities

### Overview

Summarize system health, active project, MCP status, public endpoint, tunnel processes and recent activity. It is not a full configuration page.

### Projects

Reserve the architecture for upcoming multi-project support. The first redesign may show the current project and an explanatory empty state.

### Services

Own MCP process controls, ports, watchdog, auto-start and executable configuration.

### Cloudflare

Own authentication, domain configuration, Tunnel identity, local target synchronization and cloudflared process supervision.

### Logs & diagnostics

Use an IDE-like split view: source list, log output and optional inspector.

### Security

Explain permission mode, file scope and network access in plain language. Risk must be clear before applying changes.

### Settings

Own appearance, desktop integration, launch behavior, data paths and product information.

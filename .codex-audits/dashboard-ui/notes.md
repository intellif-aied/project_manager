# Dashboard UI Audit

Original screenshot: `01-dashboard-current.png`
Final style-only screenshot: `03-dashboard-style-only.png`

## Scope

- Surface: `http://localhost:5180/dashboard`
- Viewport: current in-app browser desktop viewport
- Focus: header, sidebar menu, dashboard first screen visual hierarchy

## Current Impression

The screen is usable and structured, but it feels closer to a styled admin template than a premium operations console. The sidebar carries most of the visual drama, while the header and content cards stay pale and flat. The result is readable in parts, but the screen does not establish a strong dashboard hierarchy.

## Findings

1. Sidebar is too dominant for the amount of information it contains.
   The dark blue gradient, grid texture, cyan active state, and animated divider make the navigation feel heavier than the dashboard content. It attracts attention even after the user is already on the page.

2. Header does not create a clear product command layer.
   The header has a pale gradient and user pill, but the left side only says `AIDA OPS CONSOLE` and the page title. It does not carry operational context such as sync state, current scope, day summary, or quick actions, so it reads like decoration rather than a useful top bar.

3. Dashboard content lacks a hero-level focal point.
   `我关注的事项` and `我的风险提示` are important, but they appear as equal white panels with small headers. The first screen needs one decisive "what matters now" band before the lists.

4. Cards and rows are too similar.
   Panels, list rows, status tags, empty token state, and report widgets all share similar white/blue-gray treatment. This makes the page visually calm, but not sharp. Priority, risk, action, and passive information need clearer differences.

5. The color system still leans on the login palette.
   The sidebar and header reuse navy, cyan, and bright blue from the login atmosphere. For a logged-in operations product, the palette should feel more precise: cool neutrals for structure, blue only for navigation/action, amber/red only for risk, and a more deliberate accent color for AI intelligence.

6. Motion exists but does not communicate state.
   The sidebar signal line animates, but it is decorative. More useful motion would be restrained: live status pulse, panel entrance, risk row emphasis, active filter transitions, or subtle chart reveal.

## Direction Options

### Direction A: Precision Ops Console

- Sidebar becomes quieter: matte near-black, less glow, no broad radial highlight.
- Header becomes a compact command bar with current workspace, date/status, and primary quick action.
- Dashboard gains a top insight strip: risk count, blocked items, pending reports, token trend.
- Best if the product should feel serious, enterprise, and clear.

### Direction B: AI Mission Control

- Keep the dark sidebar but make it more technical and less decorative.
- Add a glassy dashboard hero band with AI summary, system health, and action queue.
- Add subtle scan-line or shimmer motion only where data updates.
- Best if the platform wants a stronger "AI operations" brand signal.

### Direction C: Calm Executive Workspace

- Lighten sidebar into a slate/white split layout and reduce contrast.
- Header becomes spacious and editorial, with richer typography and user context.
- Dashboard cards use larger spacing and fewer borders, more typography contrast.
- Best if PMs and managers need focus more than technical density.

## Recommended Route

Use Direction A with a small amount of Direction B. The app is an internal Aida operations platform, so the strongest fit is: precise, dense, premium, and not flashy. Keep advanced effects tied to operational meaning rather than decoration.

## Implementation Targets

1. Redesign `Header.css` into a clearer command bar with better depth and a stronger page title block.
2. Rebalance `Sidebar.css` by reducing glow, making active state crisper, and improving menu group readability.
3. Upgrade `console-dashboard.css` first screen with an insight band, stronger panel hierarchy, better empty state treatment, and motion that respects reduced-motion.
4. Keep Ant Design components and current layout structure; polish CSS and local dashboard markup before changing shared architecture.

## Accessibility Risks

- Cyan-on-blue sidebar states may be low contrast for some text/icon states.
- Small 10px uppercase labels in header/sidebar may be hard to read.
- Decorative animation should remain disabled under `prefers-reduced-motion`.
- Screenshot review cannot verify keyboard focus, screen reader order, or color contrast numerically.

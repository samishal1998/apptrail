---
title: Dashboards & sharing
description: Create private or public dashboard pages, arrange cards, and control what visitors can see.
---

## Registry versus dashboard

**Apps** contains all known applications. A dashboard page contains the subset you select. This keeps discovery automatic without letting infrastructure changes rearrange your personal starting point.

From **Overview**, use the dashboard selector to switch pages. Choose **New page** to create another view over the same registry.

Each page has its own name, URL slug, visibility, app selection, and card order. The default page can be renamed but not deleted.

## Choose and arrange apps

1. Select a dashboard.
2. Open **Choose apps**.
3. Select the apps you want on it and save.
4. Open **Layout** to reorder them.

Drag cards on desktop, or use the earlier/later buttons for keyboard and touchscreen access. The responsive grid keeps one persistent ordering across screen sizes; freely resized or independently positioned tiles are not part of this release.

## Private by default

New pages are private and available through the signed-in owner workspace. They are not published to anonymous visitors.

Apptrail has one owner account. It controls provider configuration, app edits, metadata refreshes, layouts, and publication.

## Publish a page

In **Page settings**, set a slug such as `family` and choose **Anyone**. The public page is:

```text
https://your-apptrail-host/d/family
```

Use **Open public page** to visit it. Public means anonymous, read-only access—not a secret-link access system.

Visitors can see that page's visible apps, including:

- Name, description, and category.
- Launch URL.
- Icon and health state.

They cannot read the global registry, provider endpoints, discovery diagnostics, field provenance, or owner settings. They cannot edit, favorite, or reorder apps.

Publishing Apptrail links does not change authentication or network access requirements on the applications themselves. A private LAN URL still requires access to that network.

## Hide or unpublish

**Hide from dashboards** is an application-level preference. It removes that app from all dashboard views, including public pages. Find hidden apps through **Apps → Hidden** to show them again.

Changing a page back to **Just me** blocks subsequent anonymous reads. Open public pages recheck every 30 seconds and when their browser window regains focus. Content already received by a visitor cannot be recalled.

## Alternate launch addresses

On **Apps**, entries with launch URLs appear first. Cards show their primary URL directly and, when available, the Traefik router and entrypoint. The source button identifies Docker, Caddy, or Traefik. A container without an inferred route explicitly shows **No launch URL discovered · Set URL**; it is not a failed Traefik route.

The owner workspace groups Traefik hostname aliases for the same router/path into one card. Expand the card's **alternate addresses** to open another hostname. To change the main link, edit the app, select an address under **Available launch addresses**, and save.

Only the primary launch URL is sent to public dashboard visitors. Changing the primary URL changes the link those visitors receive.

## Favorites and search

Favorites belong to the owner's app preferences. Use the **Favorites** filter to narrow a page or the registry. Search matches names, descriptions, categories, hostnames, and provider names in the owner workspace.

Public-page search is limited to that page's published apps.

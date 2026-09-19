---
title: Metadata & health
description: Enrich discovered apps, inspect provenance, and publish an Apptrail discovery manifest.
---

## What Apptrail reads

Once an app has a launch URL, Apptrail can fetch:

- HTML title and description, including useful Open Graph metadata.
- Declared favicons and web app manifest icons.
- A version-1 Apptrail discovery manifest.
- A lightweight HTTP health result.

Background refreshes run alongside the five-minute discovery cycle. Use **Refresh metadata** from an app's edit or source panel for an immediate request.

## Private networks

HTTP probing of LAN, localhost, and Tailscale addresses is off by default. Enable **Settings → Allow private-network probes** when your services live there.

Requests have short timeouts, a 1 MiB response-body limit, bounded redirects, and DNS/IP validation at connection time. Link-local, known cloud metadata endpoints, and reserved address ranges remain blocked. Apptrail does not execute page JavaScript or forward browser credentials to apps.

Automatically discovered icon and manifest resource declarations stay on the application's origin. An explicit owner icon override may use another HTTP(S) origin. Icons are fetched through Apptrail's bounded proxy; public icon access follows dashboard visibility.

## Your preferences win

For display fields, the general priority is:

1. Owner overrides.
2. Apptrail discovery manifest.
3. Explicit Apptrail labels.
4. Manual application data.
5. HTML metadata.
6. Proxy/container defaults and generated fallbacks.

Open an app card's **source indicator** to see the effective values and their sources. A temporary probe failure retains the last successful HTTP metadata while updating health and diagnostics.

## Add an Apptrail manifest to your app

Serve JSON at `/.well-known/apptrail.json`:

```json
{
  "version": "1",
  "id": "photos",
  "name": "Photo library",
  "description": "Our favorite memories, all together.",
  "icon": "/icon.png",
  "category": "Media",
  "health": {
    "url": "/health",
    "method": "GET",
    "expected_status": [200]
  }
}
```

- Use a stable ID for this application instance. Conflicts are diagnosed rather than automatically merging unrelated apps.
- Relative Apptrail resource URLs resolve against the application's public base URL.
- Health supports `GET` or `HEAD`; the default expected status is `200`.
- Unknown fields are ignored, and unsupported major versions are not interpreted.
- Keep credentials out of the manifest.

The working pack includes the [discovery manifest schema](https://github.com/samishal1998/apptrail/tree/main/apptrail_working_pack/schemas) and additional [examples](https://github.com/samishal1998/apptrail/tree/main/apptrail_working_pack/examples). Rich actions and embedded views described in the PRD are later capabilities, not active widgets in this release.

## Understand health

Health may come from Docker, Traefik's upstream status, an explicit Apptrail health endpoint, or a launch-URL probe. The source inspector shows which one supplied the effective state. Caddy configuration discovery alone does not perform an availability check.

| State | Meaning |
| --- | --- |
| Not checked | No usable check yet, or probing is blocked by policy. |
| Healthy | The effective health check passed. |
| Needs attention | The launch URL returned a client error, such as an authentication requirement. |
| Unhealthy | A service/health endpoint reported failure. |
| Unreachable | The connection could not be completed. |
| Missing / stale | Discovery no longer observes the app; this is separate from health. |

Hiding an app does not disable its configured checks.

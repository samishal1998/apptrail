---
title: Your first dashboard
description: Go from an empty Apptrail workspace to a personal starting point for your self-hosted apps.
---

Once you have [installed Apptrail](../installation/) and created your owner account, you can start with automatic discovery or add an app by hand.

## 1. Bring in your apps

### Connect Docker

Open **Providers → Connect provider**, select **Docker**, give the provider a name, and enter a Docker API endpoint:

```text
unix:///var/run/docker.sock
```

For a remote or restricted proxy endpoint, use its HTTP(S) URL instead. Enable the provider, save it, and choose **Scan now**.

If Apptrail runs in Docker, first give its container [Docker API access](../discovery/#container-discovery-access) through a restricted API proxy or a socket mount with the correct group permissions. Downloading the Compose deployment files alone does not grant infrastructure access.

Apptrail reads containers, Docker health, and Apptrail/Traefik labels. It does not start, stop, or deploy containers. See [Docker discovery](../discovery/) for access requirements and supported routing rules.

### Connect Caddy or Traefik directly

In Apptrail v0.3+, select **Caddy** or **Traefik** in the same provider form. Enter Caddy's admin base endpoint, such as `http://127.0.0.1:2019` when both processes share the host, or Traefik's protected API base URL. Use an authorization file if that endpoint requires Basic or Bearer authentication.

Save, then choose **Scan now**. Hostnames, paths, TLS, and listener ports are read from the API; management endpoints are not autodetected. See [proxy API setup](../discovery/#caddy-and-traefik-apis) for container networking, supported routes, and authentication.

### Add a manual app

Choose **Add app**, then enter:

- **App name:** a name you recognize.
- **Launch URL:** the address you use in your browser.
- **Category:** a label such as Media, Home, or Development.
- **Description and icon URL:** optional details.

Save the app. It appears in **Apps**, your complete application registry.

## 2. Choose what belongs on your page

Open **Overview → Choose apps**, select your applications, and save.

Your registry and your dashboard are separate: discovery can add apps without rearranging your curated page. Add a newly discovered app to a page whenever you are ready.

## 3. Make it your own

From an app card:

- Use the **star** to favorite it.
- Open its **edit menu** to rename it, change the description/category/icon, or hide it.
- Click its **source indicator** to inspect where its fields came from.
- Click the **app name** to launch it in a new tab.

User preferences take precedence over later scans. Editing one field leaves the other discovery-derived fields free to update.

## 4. Put things in order

Open **Layout** and choose the dashboard you want to arrange. Drag cards into position, or use each card's **earlier/later buttons** with a keyboard or touchscreen.

The order is saved automatically. Apptrail uses an ordered responsive grid: columns adapt to the screen, while your chosen order stays the same.

## 5. Decide who can visit

Pages are private by default. In **Page settings**, choose **Anyone** to publish a read-only page at `/d/<slug>`.

Visitors see only that page's visible applications. Administration stays behind owner login. Learn more about [dashboards and sharing](../dashboards/).

## Useful next steps

- For services on your LAN or Tailscale network, enable **Settings → Allow private-network probes** before requesting HTTP metadata.
- Use **Refresh metadata** in an app's edit/source panel for an immediate check.
- Use **Discover** to inspect missing services or apps without a launch URL.
- Switch between **After sunset** and **First light** in Settings.

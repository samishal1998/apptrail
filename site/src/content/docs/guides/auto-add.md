---
title: Auto-add rules with CEL
description: Automatically add matching applications to a dashboard section using CEL expressions.
---

In Apptrail v0.5+, each dashboard can have a **CEL** rule that automatically adds matching apps. Rules are disabled by default and are configured independently for each page.

## Set up a rule

1. Select the dashboard and open **Page settings**.
2. Enter an **Auto-add rule (CEL)**.
3. Choose the destination section. Create sections in **Layout** first if needed.
4. Use **Preview matching apps** to check the result.
5. Save the page.

For example, automatically include launchable apps discovered through Traefik:

```text title="CEL"
url != "" && "traefik" in provider_types
```

Saving applies the rule to existing apps. Future successful provider reconciliations, manual app additions/edits, and metadata refresh cycles evaluate the saved rule again.

## Examples

All launchable applications:

```text title="CEL"
url != ""
```

Media applications:

```text title="CEL"
category in ["Media", "media"] && url != ""
```

Only apps you have favorited:

```text title="CEL"
favorite && url != ""
```

Apps on a particular domain:

```text title="CEL"
host.endsWith(".example.com")
```

A matching alternate address:

```text title="CEL"
urls.exists(u, u.startsWith("https://photos."))
```

Match a Traefik router only when the fact exists:

```text title="CEL"
"router" in facts && facts.router.startsWith("media-")
```

CEL uses typed expressions, boolean operators, string comparisons, lists, and macros such as `exists`. Field names and string comparisons are case-sensitive. This is CEL, not JavaScript; expressions must return a boolean.

## Available fields

| Fields | Type | Meaning |
| --- | --- | --- |
| `id`, `name`, `description` | String | Registry identity and effective display metadata |
| `url`, `host`, `path` | String | Effective primary launch URL and its hostname/path |
| `urls` | List of strings | Known launch addresses, including the primary URL |
| `category`, `health`, `lifecycle` | String | Effective category and current registry state |
| `favorite`, `hidden` | Boolean | Owner preferences |
| `provider_types` | List of strings | Types of present observations: `docker`, `caddy`, `traefik`, or `manual` |
| `provider_ids` | List of strings | IDs of present discovery/manual sources |
| `facts` | Map of strings to strings | Effective fact values such as `router`, `entrypoints`, `backend`, or `server` |

Guard optional facts using `"key" in facts` or `has(facts.key)`. Accessing a missing key without a guard is an evaluation error.

## Membership and layout behavior

- Only **present, non-hidden** apps can be automatically added.
- Rules are **add-only**. A card stays in place if it later stops matching; its current health/lifecycle still updates.
- New matches are placed in free space near the bottom of the chosen section. Existing tile positions and sizes are preserved.
- Removing a match through **Choose apps** records an exclusion, preventing the next scan from adding it again.
- Manually re-adding that app clears its exclusion. **Reset exclusions** in Page settings allows excluded matches to be added again.
- Disabling/changing a rule does not remove existing cards.
- Forgetting an app removes that registry identity. If discovery later creates a new identity, it may match the rule again; use a negative predicate or provider label when you want a persistent source-level exclusion.

On a **public dashboard**, automatically added apps become publicly visible immediately. Use a restrictive rule, such as `favorite && url != ""`, if publication should follow an explicit owner action. The CEL expression, exclusions, provenance, and private provider information are not exposed to visitors.

## Validation and limits

Rules are checked when saved, and the preview shows matching names and URLs. Syntax/type errors are rejected. If a runtime error appears after discovery introduces new data, that page's evaluation adds nothing and records a diagnostic; other pages can still evaluate their rules.

Expressions are limited to 4,096 bytes and 32 levels of parser recursion. Evaluation is bounded by a cost limit per app and a two-second deadline per dashboard evaluation. CEL has no configured filesystem, process, or network functions. Dashboards support up to 1,000 selected apps and 1,000 manual exclusions.

If an automatic update occurs while a layout is being edited, a stale save is rejected rather than overwriting the new membership. Reload the latest layout and retry the change.

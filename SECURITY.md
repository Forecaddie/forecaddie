# Security policy

Forecaddie decides what is reachable on a network, so a flaw in it can expose services that were meant to be private. Please report security problems privately.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: go to the **Security** tab of this repository and choose **Report a vulnerability**. Do not open a public issue, pull request or Discussion.

Useful things to include:

- What an attacker can do, and from where (the public internet, the LAN, the overlay, a compromised app host, an authenticated Operator or Viewer)
- Steps to reproduce, with versions of Forecaddie, NetBird, Caddy and caddy-l4
- Whether you have seen it exploited

Redact real tokens, setup keys and credentials from the report, as described in [CONTRIBUTING.md](CONTRIBUTING.md#reporting-bugs). A description of where the secret appears is enough.

## What to expect

This is a volunteer project with no paid support. Reports will be acknowledged as soon as practical, and you will be kept informed while a fix is prepared. Once a fix is released, the advisory will credit you unless you ask not to be named.

## Scope

In scope: the coordinator, the reconciler, the enrollment snippet and scripts, and the configuration they generate (NetBird policies and Caddy fragments), including any way they allow an app to be reachable beyond what its approval permits.

Out of scope, and better reported upstream: vulnerabilities in NetBird, Caddy, caddy-l4, caddy-security or your IdP themselves. If you are unsure whether a problem is Forecaddie's or an upstream project's, report it here and it will be routed.

## Supported versions

There are no releases yet. Once there are, only the latest release will receive security fixes.

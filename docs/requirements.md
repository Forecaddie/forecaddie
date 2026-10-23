# Forecaddie — Requirements

## Purpose

The coordinator lets any app on any Proxmox host be published through Caddy without the host being reachable on the LAN, without Caddy knowing the host's address, and with every published app approved and auditable. It replaces per-host Caddy upstream entries with a registry that drives NetBird policies and Caddy configuration from one place.

Four problems with the current setup motivate it:

1. Every LXC or VM must expose its app on the LAN so Caddy can reach it, so every host is a public-facing service on a flat network.
2. Caddy's config names each upstream by LAN hostname or IP, so a network move, re-addressing or host migration means editing Caddy by hand.
3. Hosts get little or no firewall protection from each other; a compromised app can reach every other app.
4. Apps rarely speak TLS on the LAN, so traffic between Caddy and the app is plaintext.

## Scope

Project name: **Forecaddie** — the caddie who walks ahead and reports where everything landed. The product is the coordination layer only; NetBird is the transport and Caddy is the edge, both unmodified.

**In scope**

- A registry of apps: which NetBird peer, which port, which protocol, which public hostname, which Caddy node(s).
- An approval lifecycle for each app (pending, approved, disabled, destroyed) that controls both NetBird policy and Caddy configuration.
- A reconciler per Caddy node that owns exactly one imported Caddy config fragment and never touches the rest of the Caddyfile.
- HTTP reverse-proxy publishing and L4 passthrough (TCP with SNI or dedicated port, UDP on dedicated port) via caddy-l4.
- Health and status: NetBird peer presence plus Caddy upstream health, surfaced in the UI and reflected in Caddy (503 page, static responder).
- A web management surface and a REST API, both authenticated only through an external OIDC provider, with group-claim-based roles.
- An audit log of every state change and who or what made it.
- Host enrollment via NetBird setup keys, including a cloud-init path.
- Multiple Caddy nodes, including one per site.

**Out of scope**

- Writing a tunnel protocol or a host agent that carries traffic. NetBird does this.
- Replacing caddy-security, the IdP, or any request-level authentication. The coordinator decides *what* is reachable, not *who* may log in to it.
- Site-to-site routing. Existing site-to-site VPN links keep that role; no NetBird subnet routes.
- Managing the NetBird management server or its upgrades beyond documenting the dependency.
- Certificate management. Caddy's ACME handling is unchanged.

## Glossary

| Term | Meaning |
| --- | --- |
| Peer | A host enrolled in NetBird. Has a stable WireGuard key and overlay IP. One peer per LXC or VM. |
| Peer group | A NetBird group. Every enrolled app host gets its own group, assigned by its setup key. |
| App | One service on a peer: protocol + port + public hostname. A peer may host many apps (Docker-host VM). |
| Binding | The approved relationship app → peer → Caddy node(s). Moving an app means destroying one binding and creating another. |
| Caddy node | One Caddy instance (in the reference deployment, a single Caddy LXC). Also a NetBird peer, in group `caddy`. |
| Fragment | The one Caddy config file per node that the coordinator owns and Caddy imports. |
| Policy | A NetBird access-control rule: source group → destination group, protocol, ports. The coordinator owns all policies touching `caddy`. |
| Reconciler | The per-Caddy-node process that turns registry state into a fragment and policies and applies them. |
| Management | The NetBird management server (self-hosted) and its API. |
| IdP | The external OIDC provider (Pocket ID, Authentik, Keycloak, or similar). |

## Actors

| Actor | Role in the system |
| --- | --- |
| Admin | Full control: approve, destroy, manage Caddy nodes, manage roles. |
| Operator | Register apps, enable/disable approved apps, view everything. Cannot approve or destroy. Intended for agents. |
| Viewer | Read-only access to status and audit. |
| Automation agents | Machine clients of the API. Act as Operator by default; Admin only by explicit grant. Authenticate as OIDC clients, never with shared passwords. |
| App host | A peer. Runs the app and the NetBird client. Has no coordinator software unless the optional enrollment helper is used. |
| Caddy node | Runs Caddy and the reconciler. Reads registry state, writes its fragment, reloads Caddy. |
| NetBird management | Source of truth for peers, groups, policies and peer status. The coordinator writes groups and policies through its API. |
| IdP | Sole source of human and agent identity for the coordinator. |

## Functional requirements

An approved app is reachable only from its Caddy node(s), only on its declared port, and only while approved. Everything below serves that sentence.

**Enrollment**

- FR-01 A new host joins by running the NetBird client with a setup key. No other software is required to become a peer.
- FR-02 Each setup key auto-assigns the host to its own peer group. The coordinator can mint setup keys (single-use, expiring) for this purpose.
- FR-03 Enrollment works from cloud-init user-data: install NetBird from its package repo, run `netbird up` with the setup key, apply the host firewall rule (FR-05).
- FR-04 Admin may require NetBird peer approval before a new peer joins the mesh; the coordinator surfaces pending peers.
- FR-05 Enrollment applies a firewall rule on the host or at the Proxmox firewall: drop inbound on the LAN interface except SSH; allow inbound on `wt0`. The coordinator documents and, where it has a path to do so, verifies this.
- FR-06 On a Docker-host peer, published container ports must bind to the peer's overlay IP, not `0.0.0.0`. The UI shows a warning for apps on peers flagged as Docker hosts.

**App registry**

- FR-10 An app record holds: name, peer, protocol (`http`, `https`, `tcp`, `udp`), peer-side port, public hostname(s), L4 mode (SNI or dedicated public port), Caddy node(s), health-check definition, owner, notes.
- FR-11 A peer may hold any number of apps.
- FR-12 An app may be published through more than one Caddy node (e.g. a local node per site plus a central node).
- FR-13 Caddy node records hold: NetBird peer, admin API endpoint, fragment path, supported features (http, l4), site label.
- FR-14 The registry stores overlay IPs as read from NetBird management, not hostnames; the reconciler writes IPs into the fragment. NetBird DNS is not required on Caddy nodes.

**Approval lifecycle**

- FR-20 States: `pending` → `approved` → `disabled` ↔ `approved`; any state → `destroyed`. Only Admin may approve or destroy; Operator may create, enable, disable.
- FR-21 Approving an app performs, as one transaction: create/update the NetBird policy (`caddy` → peer group, protocol, port), write the Caddy fragment(s), validate, reload. If any step fails the app stays `pending` and the failure is recorded.
- FR-22 Disabling removes the Caddy route and the NetBird policy rule for that app. The peer stays enrolled.
- FR-23 Destroying an app removes its policy, route and record. If the peer has no remaining apps, the coordinator offers to remove the peer from NetBird.
- FR-24 Moving an app to a new host is modelled as destroy + create; there is no in-place rebind that keeps the old peer's access.
- FR-25 Re-approval is required after any change to peer, port, protocol or hostname.

**Caddy reconciliation**

- FR-30 The reconciler owns exactly one fragment per Caddy node, imported by the hand-maintained Caddyfile, and writes nothing outside it.
- FR-31 Every write is followed by `caddy validate` on the full merged config before `caddy reload`; a failed validation leaves the previous fragment in place and the app in its previous state.
- FR-32 The fragment is deterministic from registry state: the same state produces byte-identical output, so drift is detectable by diff.
- FR-33 Reconciliation runs on every registry change and on a timer (default every 60 s) to catch overlay IP changes and manual tampering.
- FR-34 HTTP apps are emitted as `reverse_proxy` to `<overlay-ip>:<port>` with `X-Forwarded-*` headers set, active health checks enabled, and a 503 error page served via `handle_errors` when the upstream is unhealthy.
- FR-35 HTTP apps may opt into caddy-security protection by referencing a named policy defined in the hand-maintained Caddyfile; the coordinator never defines auth policies itself.

**L4 passthrough**

- FR-40 TCP apps may be published by SNI on 443 (TLS passthrough to the peer) or on a dedicated public port.
- FR-41 UDP apps are published on a dedicated public port only.
- FR-42 The reconciler emits L4 routes into the layer4 app alongside HTTP routes; both live in the same fragment.
- FR-43 Dedicated public ports are allocated from a configurable range and are unique per Caddy node.

**Health and status**

- FR-50 Peer presence is read from NetBird management (connected / last seen) at least every 30 s.
- FR-51 Upstream health for HTTP apps comes from Caddy's active health checks via the admin API.
- FR-52 For L4 apps, health is a TCP connect check from the Caddy node to the overlay IP and port (UDP: peer presence only).
- FR-53 When a peer is absent, the reconciler replaces the app's route with a static 503 responder so clients are not left waiting on upstream timeouts.
- FR-54 Status per app is shown as one of: healthy, degraded (peer present, upstream failing), offline (peer absent), disabled, pending.

**Multiple Caddy nodes and sites**

- FR-60 Any number of Caddy nodes may be registered; each runs its own reconciler against the shared registry.
- FR-61 A Caddy node is itself a NetBird peer in group `caddy`; policies are written per node group where per-node scoping is wanted (`caddy-site-a`, `caddy-site-b`).
- FR-62 Adding a Caddy node requires only: enroll it as a peer, install the reconciler, register it in the UI.

**Management surface**

- FR-70 A web UI lists peers, apps, Caddy nodes, pending approvals, status and audit, and lets each role perform its allowed actions.
- FR-71 The UI has no local accounts. Login is OIDC authorization-code flow against the configured IdP. A break-glass admin credential may exist but only via a file on the coordinator host, never via the UI.
- FR-72 Roles are derived from an IdP group claim; the claim name and the group → role map are configuration.

**API for agents**

- FR-80 Every UI action is available through a documented REST API with an OpenAPI description.
- FR-81 Machine clients authenticate with OIDC client-credentials tokens from the IdP; the same group-to-role mapping applies via token claims.
- FR-82 List and status endpoints are paginated and filterable so an agent can answer "what is pending" or "what is offline" in one call.
- FR-83 Write endpoints are idempotent on a client-supplied key so an agent retry cannot double-register.

**Audit**

- FR-90 Every state change records: timestamp, actor (subject claim), action, target, before/after, and the resulting NetBird policy and Caddy fragment hashes.
- FR-91 Audit entries are append-only and exportable.

## Non-functional requirements

**Security**

- NFR-01 Deny by default: the NetBird default `All → All` policy is removed during setup and the coordinator refuses to run while it exists.
- NFR-02 The coordinator holds one NetBird API token and one Caddy admin credential per node; both live in files with mode 0600 or in systemd credentials, never in the database.
- NFR-03 The Caddy admin API is reachable only over the overlay from the coordinator, or over localhost when the reconciler runs on the node.
- NFR-04 No shared secrets between hosts. Host identity is the NetBird peer key; setup keys are single-use and expire within 24 h.
- NFR-05 All coordinator ↔ NetBird and coordinator ↔ Caddy traffic is TLS or overlay-encrypted.

**Availability and degraded modes**

- NFR-10 Existing tunnels and routes keep working when the coordinator is down. The coordinator is a control plane; the data plane is NetBird plus Caddy.
- NFR-11 Existing tunnels keep working when NetBird management is down (established WireGuard sessions need no control plane). New peers cannot join and peer status goes stale; the UI shows this clearly.
- NFR-12 A Caddy node keeps serving its last valid fragment if it loses contact with the coordinator.
- NFR-13 Cold start order is documented: NetBird management → Caddy node → coordinator → app hosts. Any other order converges within one reconcile interval.

**Performance**

- NFR-20 Approval to live route in under 10 s on a healthy system.
- NFR-21 Peer absence reflected in Caddy within 60 s.
- NFR-22 The system is sized for tens of peers and low hundreds of apps; no design choice should prevent ten times that.

**Operability**

- NFR-30 Coordinator and reconciler ship as single static binaries plus systemd units; also as a container image.
- NFR-31 NetBird clients update from the NetBird package repository via unattended-upgrades / dnf-automatic; update windows are staggered per host because a client restart drops `wt0` briefly.
- NFR-32 All configuration is a single file; no configuration lives only in the UI.
- NFR-33 The database is SQLite in a single file, backed up by PBS with the coordinator host.

**Observability**

- NFR-40 Prometheus metrics: apps by state, peers by presence, reconcile duration and failures, Caddy reload failures.
- NFR-41 Structured JSON logs; every log line carrying an app or peer id.

**Portability**

- NFR-50 Nothing in the registry references a LAN address. A host that changes LAN, site or IP keeps working without any coordinator change.
- NFR-51 Caddy nodes are addressed by DNS alias; moving a Caddy node is a DNS change plus re-enrollment.

## Constraints and assumptions

- Caddy is a custom xcaddy build with caddy-security and the Cloudflare DNS module; `caddy-l4` is added for this project. The hand-maintained Caddyfile, auth portal and JWKS setup stay as they are.
- The reference deployment uses Pocket ID as the IdP for humans and agents. Any OIDC provider with a configurable group claim must work.
- NetBird management is self-hosted and reachable from every site over the public internet with DNS-only (grey-cloud) records; proxying it through Cloudflare breaks the gRPC clients.
- Hosts are Proxmox LXCs and VMs on flat LANs at several sites. Inter-site routing is an existing site-to-site WireGuard link; the coordinator must not depend on it or conflict with it.
- A Docker-host VM is a first-class case: one peer, many apps.
- Automation agents will be regular API clients and must be able to do everything an Operator can without a browser.
- Hairpinning through a site-local Caddy node is accepted; a Caddy node per site is the intended topology, with a single node acceptable for experiments.

## Rejected alternatives

| Alternative | Why not |
| --- | --- |
| [Pangolin](https://github.com/fosrl/pangolin) | Closest existing match: outbound WireGuard tunnels (Newt), dashboard, OIDC, Integration API, clustering. But it is built on Traefik and brings its own request-auth layer (Badger), which would duplicate caddy-security and the external IdP and require abandoning the existing Caddy build. Dual AGPLv3 / commercial license. |
| [frp](https://github.com/fatedier/frp) | Solid transport with client IDs, OIDC client-credentials auth and a dashboard, but it is a tunnel, not a mesh: no per-peer firewall policy, no group model, and a coordinator would still have to be written on top. NetBird gives identity, policy and enrollment for free. |
| [Gopher](https://github.com/smalex-z/gopher) | Caddy + rathole in one binary with a web UI and host agent. Pre-1.0, no OIDC, owns Caddy rather than cooperating with an existing build. |
| Cloudflare Tunnel | Previously evaluated. Hands TLS termination and access control to a third party; the point here is self-hosting. |
| Split-horizon DNS to app hosts | Removes hairpinning but bypasses Caddy, so no central TLS, no auth gate, and apps back on the LAN. Rejected per the purpose section. |
| NetBird subnet routes as inter-site glue | The configuration that caused trouble before. Out of scope; the existing site-to-site VPN keeps that role. |
| Building the tunnel and agent from scratch | Most control, most work, and reproduces what NetBird already does well. |

## Open questions

- [ ] Does the coordinator mint NetBird setup keys itself, or are they created in the NetBird UI and pasted in? Minting is more convenient for cloud-init and for agents; it also means the coordinator's NetBird token needs setup-key scope.
- [ ] Per-node Caddy groups (`caddy-site-a`, `caddy-site-b`) from day one, or one `caddy` group until a second node exists?
- [ ] Should the reconciler run on each Caddy node (writes the fragment locally, reloads via localhost admin API) or centrally (pushes over the overlay)? Local is more resilient; central is simpler to deploy.
- [ ] For Docker-host peers, is the overlay-IP bind enforced only by documentation and a UI warning, or does an optional enrollment helper rewrite compose files?
- [ ] Where does the coordinator itself live? It must not be behind a tunnel it manages. Candidate: a small LXC on the main Proxmox host, or alongside NetBird management.
- [ ] Is HTTP/3 (QUIC) to the app host over the overlay wanted, or is HTTP/1.1–2 between Caddy and the app sufficient?
- [ ] Retention period for audit entries.

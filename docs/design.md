# Forecaddie — System Design

## At a glance

The coordinator never carries traffic. It writes NetBird policies and a Caddy config fragment; NetBird enforces who may reach whom on the overlay, and Caddy carries the requests.

![Architecture](diagrams/architecture.svg)

Tinted boxes are the two pieces to build; every Caddy node is itself a NetBird peer. The coordinator's own UI and API are published through Caddy like any other app, by a hand-maintained route, so it is never behind a tunnel it manages.

Machine-readable form of the same diagram:

```json
{
  "diagram": "architecture",
  "nodes": [
    {"id": "idp", "name": "OIDC IdP", "kind": "existing", "role": "OIDC provider for humans and agents"},
    {"id": "coordinator", "name": "Coordinator", "kind": "build", "role": "registry, approval state machine, policy writer, API/UI", "placement": "own LXC on the main Proxmox; NetBird peer in group coordinator"},
    {"id": "netbird", "name": "NetBird management", "kind": "existing", "role": "peers, keys, groups, policies, peer status"},
    {"id": "reconciler", "name": "Reconciler", "kind": "build", "role": "renders fragment, validates, reloads Caddy, reports health", "placement": "one per Caddy node, same host as Caddy", "parent": "caddy_node"},
    {"id": "caddy", "name": "Caddy", "kind": "existing", "role": "TLS, caddy-security, HTTP reverse proxy, layer4", "parent": "caddy_node"},
    {"id": "caddy_node", "name": "Caddy node", "kind": "container", "role": "one per site; NetBird peer in group caddy-<site>"},
    {"id": "app", "name": "App", "kind": "existing", "role": "listens on wt0:<port>", "parent": "app_host"},
    {"id": "app_host", "name": "App host", "kind": "container", "role": "LXC or VM; NetBird peer in group host-<name>; may hold many apps"},
    {"id": "users", "name": "Users and agents", "kind": "external", "role": "browsers; automation agents as OIDC clients"}
  ],
  "edges": [
    {"from": "coordinator", "to": "idp", "protocol": "OIDC", "what": "verifies tokens (auth-code for humans, client-credentials for agents)"},
    {"from": "coordinator", "to": "netbird", "protocol": "HTTPS REST", "what": "writes groups, policies, setup keys; reads peers and status"},
    {"from": "reconciler", "to": "coordinator", "protocol": "HTTPS over wt0", "what": "pulls desired bindings and peer presence; reports applied hash, errors, upstream health"},
    {"from": "reconciler", "to": "caddy", "protocol": "admin API, localhost:2019", "what": "caddy validate, then reload"},
    {"from": "caddy", "to": "app", "protocol": "HTTP or TCP/UDP over wt0", "what": "proxied traffic; allowed only by NetBird policy caddy-<site> -> host-<name> on the app port"},
    {"from": "app_host", "to": "netbird", "protocol": "gRPC", "what": "enrolls with single-use setup key; keeps peer session"},
    {"from": "caddy_node", "to": "netbird", "protocol": "gRPC", "what": "enrolled as a peer like any host"},
    {"from": "users", "to": "caddy", "protocol": "HTTPS", "what": "all published apps, including the coordinator UI/API via a hand-maintained route"}
  ],
  "data_plane": ["users -> caddy -> app"],
  "control_plane": ["coordinator -> netbird", "reconciler -> coordinator", "reconciler -> caddy"],
  "invariant": "the coordinator is never on the data path; existing tunnels and routes survive its outage"
}
```

## Components

The system is three things you build and three you already run. Built: the **coordinator** (API, UI, registry, policy writer), the **reconciler** (one per Caddy node), and the **enrollment snippet** (cloud-init). Existing: **NetBird** (identity, overlay, policy enforcement), **Caddy** (edge, TLS, auth, L4), **an OIDC** IdP.

| Component | Runs where | Owns | Talks to |
| --- | --- | --- | --- |
| Coordinator | Its own LXC on the main Proxmox host (not behind any tunnel it manages); also a NetBird peer in group `coordinator` | SQLite registry, approval state machine, audit log, NetBird groups and policies touching `caddy*` | NetBird management API (HTTPS), the IdP (OIDC), reconcilers (pull over overlay) |
| Reconciler | On each Caddy node, as a systemd service beside Caddy | One fragment file, Caddy reload, upstream health readout | Coordinator API (pull), Caddy admin API on localhost, overlay IPs (TCP connect checks) |
| Enrollment snippet | cloud-init user-data or a one-shot script on existing hosts | NetBird install, `netbird up`, host firewall rule | NetBird management (setup key), package repo |
| NetBird management | Existing self-hosted instance; DNS-only records | Peers, keys, groups, policies, peer status | Every peer (gRPC); coordinator (REST) |
| Caddy node | Existing Caddy LXC today; one per site later; NetBird peer in `caddy-<site>` | Caddyfile, certificates, caddy-security, layer4 | Clients, app peers over `wt0`, reconciler on localhost |
| App host | LXC or VM; NetBird peer in its own group | The app, listening on `wt0` only | Caddy nodes over `wt0` (inbound, policy-gated) |
| IdP (e.g. Pocket ID) | Existing | Users, groups, OIDC clients for humans and agents | Coordinator UI and API |

The reconciler pulls rather than being pushed to. A Caddy node behind a flaky link keeps its last fragment and catches up when it can, and the coordinator never needs inbound reachability to a node.

## Data model

Six tables in SQLite. `apps` is the centre; everything else exists to make an app reachable or to record that it was.

| Table | Key fields | Notes |
| --- | --- | --- |
| `peers` | id, transport\_ref (JSON, driver-specific peer and group ids), group\_name, hostname, overlay\_ip, site, kind (`lxc`, `vm`, `docker_host`), last\_seen, approved\_at | Mirror of the transport driver's peer list; refreshed every 30 s. `overlay_ip` is the only address ever stored. |
| `caddy_nodes` | id, name, site, peer\_id, group\_name (`caddy-<site>`), admin\_api\_url, fragment\_path, features (`http`, `l4`), l4\_port\_range, last\_reconcile\_at, last\_fragment\_hash | A Caddy node is also a row in `peers`. |
| `apps` | id, name, peer\_id, protocol (`http`, `https`, `tcp`, `udp`), port, hostnames\[\], l4\_mode (`sni`, `port`), public\_port, health (uri, interval, expect), auth\_policy (name or null), state, idempotency\_key, owner, notes | `auth_policy` names a caddy-security policy defined in the hand-maintained Caddyfile; the coordinator only references it. |
| `bindings` | app\_id, caddy\_node\_id, desired, applied\_fragment\_hash, applied\_at, error | One row per app × node. `desired` is derived from `apps.state`; `applied_*` is what the reconciler last confirmed. The difference is drift. |
| `setup_keys` | id, transport\_ref, group\_name, created\_by, expires\_at, used\_at | Single-use, 24 h. Minted by the coordinator for enrollment. |
| `audit` | id, ts, actor\_sub, actor\_kind (`human`, `agent`, `system`), action, target\_type, target\_id, before, after, policy\_hash, fragment\_hash | Append-only. `before`/`after` are JSON. |

State machine for `apps.state`: `pending → approved`, `approved ↔ disabled`, `any → destroyed`. Edits to `peer_id`, `port`, `protocol`, `hostnames` or `l4_mode` on an approved app move it back to `pending` (FR-25).

Two invariants the coordinator enforces on every write:

- A NetBird policy may reference a peer group only if some `approved` app on that peer requires it. Policies are regenerated from `apps`, never edited independently.
- `public_port` is unique per `caddy_node` and lies within that node's `l4_port_range`.

## Approval flow

Approval is one transaction: policy first, config second, and the policy is removed again if the config does not validate. An app is never reachable on the overlay without a live Caddy route, and never routed without an overlay policy.

![Approval flow](diagrams/approval-flow.svg)

The policy is written before the fragment because a policy with no route is harmless (nothing connects), while a route with no policy produces confusing timeouts. If the reload fails after a successful validate — rare, usually a port conflict — the reconciler reports the error, the fragment is rolled back to its previous bytes, and the app returns to pending with the policy removed. Disable and enable are the same transaction in reverse and forward without the approval step; destroy is disable plus deletion of the record and, if the peer has no other apps, an offer to remove the peer.

Machine-readable form of the same flow:

```json
{
  "diagram": "approval_flow",
  "states": ["pending", "approved", "disabled", "destroyed"],
  "initial": "pending",
  "transitions": [
    {"name": "register", "from": null, "to": "pending", "actor": ["operator", "admin", "agent"], "api": "POST /apps"},
    {"name": "approve", "from": "pending", "to": "approved", "actor": ["admin"], "api": "POST /apps/{id}/approve",
     "steps": [
       {"n": 1, "id": "policy",   "do": "create or update NetBird policy caddy-<site> -> host-<name>, protocol+port", "on_fail": "abort; state stays pending; error.step=policy"},
       {"n": 2, "id": "render",   "do": "render http.caddy and l4.caddy for each bound Caddy node", "on_fail": "remove policy from step 1; state stays pending; error.step=render"},
       {"n": 3, "id": "validate", "do": "caddy validate with staged fragments", "on_fail": "remove policy from step 1; live fragment untouched; state stays pending; error.step=validate"},
       {"n": 4, "id": "reload",   "do": "atomic rename staged over live, POST /load", "on_fail": "restore previous fragment bytes; remove policy; state stays pending; error.step=reload"}
     ],
     "on_success": "state=approved; bindings.applied_fragment_hash set; audit entry with policy_hash and fragment_hash"},
    {"name": "disable", "from": "approved", "to": "disabled", "actor": ["operator", "admin", "agent"], "api": "POST /apps/{id}/disable", "steps": ["remove route from fragment", "validate", "reload", "remove policy rule"]},
    {"name": "enable", "from": "disabled", "to": "approved", "actor": ["operator", "admin", "agent"], "api": "POST /apps/{id}/enable", "steps": "same as approve, no approval step"},
    {"name": "edit_identity", "from": "approved", "to": "pending", "actor": ["operator", "admin", "agent"], "api": "PATCH /apps/{id}", "trigger": "change to peer_id, port, protocol, hostnames or l4_mode", "effect": "same as disable, then requires approve again"},
    {"name": "destroy", "from": "*", "to": "destroyed", "actor": ["admin"], "api": "DELETE /apps/{id}", "steps": ["disable if approved", "delete record", "if peer has no remaining apps, offer peer removal"]}
  ],
  "ordering_rule": "policy before route on the way up, route before policy on the way down; a policy without a route is harmless, a route without a policy times out",
  "idempotency": "POST /apps requires Idempotency-Key; approve/disable/enable are safe to retry"
}
```

## Caddy fragment design

The contract is one line in the hand-maintained Caddyfile and one file the reconciler owns. Nothing else about Caddy changes.

**Import contract.** The Caddyfile gains, inside its global options and at site scope respectively:

```
{
    # existing global options unchanged
    layer4 {
        import /etc/caddy/forecaddie/l4.caddy
    }
}

import /etc/caddy/forecaddie/http.caddy
```

The reconciler writes exactly those two files and nothing else under `/etc/caddy`. Both are regenerated whole on every reconcile; a header comment carries the registry revision and fragment hash so drift is visible with `diff`.

**Generated HTTP block, per app** (what the reconciler emits for `immich.example.net` on peer `100.74.12.9:2283` with health check `/api/server/ping` and auth policy `trusted`):

```
immich.example.net {
    import forecaddie_common
    authorize with trusted
    reverse_proxy 100.74.12.9:2283 {
        health_uri /api/server/ping
        health_interval 15s
        health_timeout 5s
        header_up X-Forwarded-For {remote_host}
        header_up X-Real-IP {remote_host}
    }
    handle_errors 502 503 {
        rewrite * /503.html
        file_server {
            root /etc/caddy/forecaddie/pages
        }
    }
}
```

`forecaddie_common` is a snippet defined in the hand-maintained Caddyfile (logging, TLS DNS challenge, security headers), so site-wide choices stay yours. `authorize with trusted` is emitted only when `auth_policy` is set and refers to a policy caddy-security already knows; the reconciler validates the name exists by parsing the Caddyfile before emitting.

**Offline substitution.** When the peer is absent (health section), the `reverse_proxy` block is replaced by a static `respond` with status 503 and the same page, so the route answers immediately instead of after upstream timeouts.

**Generated L4 routes.** SNI mode shares 443 with HTTP by matching TLS ClientHello server names before the HTTP app; port mode opens a dedicated listener:

```
# SNI passthrough: TLS stays end-to-end to the peer
:443 {
    @mqtt tls sni mqtt.example.net
    route @mqtt {
        proxy 100.74.12.31:8883
    }
}

# dedicated port
:32022 {
    route {
        proxy 100.74.12.44:22
    }
}

# UDP
udp/:32100 {
    route {
        proxy udp/100.74.12.50:51820
    }
}
```

SNI mode requires caddy-l4 to own 443 and hand non-matching connections to Caddy's HTTP app; this is the documented `layer4` + `http` arrangement and is the one Caddy-build change this project requires (`--with github.com/mholt/caddy-l4`).

**Apply sequence** (every reconcile, idempotent):

1. Fetch desired bindings for this node from the coordinator; fetch peer presence.
2. Render `http.caddy` and `l4.caddy` to temp files.
3. If byte-identical to current files, stop.
4. Copy to a staging path and run `caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile` with the staging files swapped in via a bind mount or a temporary Caddyfile copy that imports them.
5. On success, atomically rename staging over live and `POST /load` to the admin API on localhost (or `caddy reload`).
6. Report `applied_fragment_hash` and any error back to the coordinator. On failure, live files are untouched and the binding keeps its previous `applied_*` values; the app's state is not changed by the reconciler, only its `bindings.error`.

Secrets never appear in the fragment: there is nothing in it but hostnames, overlay IPs, ports and policy names.

## NetBird integration

NetBird supplies identity, enrollment and enforcement; the coordinator only ever writes groups, policies and setup keys, and only ever reads peers.

**Groups.** Fixed groups: `coordinator`, `caddy-site-a`, `caddy-site-b`, `caddy-site-c` (created as nodes are added). Per host: one group named `host-<hostname>`, created by the coordinator when it mints the host's setup key and attached to that key as its auto-group. No host is ever in `All`-style shared groups.

**Policies.** All generated, none hand-edited. One policy per (Caddy node group, peer group) pair with one rule per app port:

| Policy | Source | Destination | Protocol / ports | Direction |
| --- | --- | --- | --- | --- |
| `coord→<caddy-site>` | `coordinator` | `caddy-<site>` | tcp 2019 only if the admin API is reached over overlay; otherwise none | one-way |
| `<caddy-site>→host-<name>` | `caddy-<site>` | `host-<name>` | one rule per approved app: tcp/udp + port | one-way |

Nothing targets `host-*` as a source. The default `All → All` policy is deleted during setup; the coordinator checks for it at startup and on every reconcile and refuses to approve anything while it exists (NFR-01).

**Setup keys.** Minted via the management API as single-use, 24 h, with `auto_groups = [host-<name>]`. The key is shown once in the UI or returned once by the API; the coordinator stores only its id and expiry. Peer approval in NetBird can be left on; the coordinator lists unapproved peers and lets an Admin approve them from its own UI by calling the management API.

**Peer status.** Polled every 30 s: `connected`, `last_seen`, `ip`. A changed `ip` triggers an immediate reconcile. Status is cached with its fetch time; if management is unreachable the cache is served with a `stale` flag and the UI says so.

**API token.** One service-account token with scopes for peers (read, approve), groups (read, write), policies (read, write), setup keys (write). Stored as a systemd credential. Rotation is a config change plus restart.

**What the coordinator does not do with NetBird:** no subnet routes, no DNS zones, no posture checks, no user management. NetBird's own UI remains the place for those if they are ever wanted.

## Transport driver interface

NetBird is the only driver shipped, but the coordinator talks to it through an interface whose policy operation is declarative, so a Tailscale or Headscale driver is a new package rather than a rewrite. The registry never stores NetBird-specific ids outside the opaque `transport_ref` column.

```go
type Peer struct {
    Ref       json.RawMessage // driver-specific ids
    Hostname  string
    OverlayIP netip.Addr
    Groups    []string
    Connected bool
    LastSeen  time.Time
    Approved  bool
}

type Rule struct {
    SrcGroup string // e.g. caddy-site-a
    DstGroup string // e.g. host-immich
    Proto    string // tcp | udp
    Port     uint16
}

type Transport interface {
    ListPeers(ctx) ([]Peer, error)
    ApprovePeer(ctx, ref) error
    RemovePeer(ctx, ref) error

    EnsureGroup(ctx, name) error
    DeleteGroup(ctx, name) error

    // Single-use key that enrolls the peer into exactly these groups.
    MintEnrollmentKey(ctx, groups []string, ttl time.Duration) (secret string, ref json.RawMessage, err error)

    // Make the mesh allow exactly these rules among coordinator-managed
    // groups (prefixes caddy-, host-, coordinator). Driver computes the diff.
    SetDesiredPolicy(ctx, rules []Rule) error

    // True if the mesh currently allows traffic beyond the desired set
    // (NetBird: the default All->All policy exists; Tailscale: a wildcard grant).
    DefaultOpen(ctx) (bool, error)

    // What the enrollment snippet needs to know.
    InterfaceName() string            // wt0, tailscale0
    JoinCommand(key string) []string  // netbird up ..., tailscale up ...
}
```

Why declarative policy: NetBird exposes policies as individual objects, while Tailscale and Headscale keep one policy document that is read, edited and written back whole. `SetDesiredPolicy` lets each driver do what is natural for it — per-object create/delete for NetBird, document diff for the others — and gives the coordinator one simple contract: after a successful call, the mesh allows the given rules and nothing else among managed groups.

Known differences a second driver would absorb, none of which leak into the registry:

| Concern | NetBird | Tailscale / Headscale |
| --- | --- | --- |
| Group concept | groups | tags (`tag:host-immich`); tags need a declared owner in the policy before a key can carry them |
| Enrollment key | setup key with auto-groups | auth / pre-auth key with tags |
| Policy shape | objects via REST | one HuJSON document; Headscale has been moving between file and database modes |
| Default state | `All → All` policy present | wildcard grant present |
| Interface | `wt0` | `tailscale0` |
| Online status | management API, near real time | API, Headscale's lags more |

Drivers are compiled into the coordinator; there is no plugin protocol. If an out-of-tree driver is ever wanted, this interface can be wrapped in gRPC without changing callers.

## Health model

Two signals, combined per binding into one state that decides what Caddy serves.

| Peer present (NetBird) | Upstream healthy (Caddy / TCP check) | State | Caddy serves |
| --- | --- | --- | --- |
| yes | yes | healthy | the app |
| yes | no | degraded | `reverse_proxy` with health checks failing → 503 page via `handle_errors` |
| yes | unknown (UDP) | healthy | the app; no upstream probe exists for UDP |
| no | — | offline | static 503 responder, no upstream attempt |
| — | — | disabled / pending | no route; Caddy returns its default for an unknown host |

Peer presence is binary from NetBird's view, but it is a control-plane fact: it says the host holds a WireGuard session, not that port 2283 answers. Upstream health is the data-plane fact. Keeping both avoids two bad outcomes: swapping in a 503 responder on a brief management hiccup while the app is fine, and leaving a dead route up because the peer is technically connected.

Timing: Caddy's active health check runs every 15 s with a 5 s timeout, so `degraded` appears within about 20 s. Peer presence is polled every 30 s and absence triggers an immediate reconcile, so `offline` appears within about 40 s and the route is swapped within the next reload (NFR-21).

Flap damping: a binding must be present for two consecutive polls before `offline` is cleared, so a peer reconnecting repeatedly does not cause a reload storm. Reloads are also coalesced: at most one Caddy reload per node per 5 s.

## Identity and RBAC

The coordinator has no user table. Every request carries an OIDC token from the IdP; roles come from a group claim; the only local credential is a break-glass file.

**Humans** use authorization-code flow with PKCE in the browser. The coordinator is one OIDC client in the IdP. Sessions are short-lived cookies holding only the subject and roles; refresh happens through the IdP.

**Agents** (for example a provisioning agent and a monitoring agent) each get their own OIDC client in the IdP and use client-credentials flow. The resulting token's `sub` is the client id, so audit entries name the agent, not a shared account. Where the IdP cannot attach group claims to a client-credentials token, the coordinator's config maps `client_id → role` explicitly; this is the only identity-related configuration besides the claim name.

**Claim to role.** Configuration:

```
oidc:
  issuer: https://idp.example.net
  client_id: forecaddie
  groups_claim: groups
  roles:
    admin:    [fc-admin]
    operator: [fc-operator, agents]
    viewer:   [fc-viewer, trusted]
  clients:
    provisioning-agent: operator
    monitoring-agent:   viewer
```

| Capability | Admin | Operator | Viewer |
| --- | --- | --- | --- |
| View peers, apps, nodes, status, audit | yes | yes | yes |
| Register an app (→ pending) | yes | yes | — |
| Enable / disable an approved app | yes | yes | — |
| Mint a setup key | yes | yes | — |
| Approve an app or a peer | yes | — | — |
| Destroy an app, remove a peer | yes | — | — |
| Add or edit Caddy nodes, roles, config | yes | — | — |

**Break-glass.** A bearer token in `/etc/forecaddie/breakglass.token` (0600), honoured only over localhost, grants Admin. Its use is logged with `actor_kind = breakglass` and the UI shows a banner for 24 h afterwards. Delete the file to disable it.

**Why not reuse caddy-security in front of the coordinator UI?** It would work for humans, but client-credentials tokens for agents need to be validated by the application, and having one validation path for both is simpler and easier to audit. The coordinator can still sit behind Caddy for TLS.

## API surface

REST with JSON, served under `/api/v1`, described by an OpenAPI 3.1 document at `/api/v1/openapi.json`. The UI is a client of the same API, so nothing is UI-only (FR-80).

| Resource | Endpoints | Notes |
| --- | --- | --- |
| Peers | `GET /peers`, `GET /peers/{id}`, `POST /peers/{id}/approve`, `DELETE /peers/{id}` | Filter: `?site=`, `?present=`, `?unapproved=1` |
| Setup keys | `POST /setup-keys` (body: hostname, site, kind), `GET /setup-keys` | Response carries the key once. |
| Apps | `GET /apps`, `POST /apps`, `GET /apps/{id}`, `PATCH /apps/{id}`, `DELETE /apps/{id}` | `POST` requires `Idempotency-Key` header (FR-83). `PATCH` of identity fields resets state to pending. Filter: `?state=`, `?peer=`, `?node=`, `?health=` |
| App actions | `POST /apps/{id}/approve`, `/disable`, `/enable` | Approve is Admin-only and synchronous: it returns 200 only after policy + fragment + reload succeeded, 409 with the failing step otherwise. |
| Bindings | `GET /apps/{id}/bindings` | Per-node applied state, drift, last error. |
| Caddy nodes | `GET /nodes`, `POST /nodes`, `PATCH /nodes/{id}`, `POST /nodes/{id}/reconcile` | Admin-only writes. |
| Reconciler | `GET /nodes/{id}/desired` (reconciler pulls), `POST /nodes/{id}/report` (reconciler reports applied hash, errors, upstream health) | Authenticated with a per-node token issued when the node is registered. |
| Audit | `GET /audit` | Filter: `?actor=`, `?target=`, `?since=`; cursor pagination. |
| Health | `GET /healthz`, `GET /readyz`, `GET /metrics` | Unauthenticated on localhost; `metrics` is Prometheus text. |

Agent-friendly conventions: every list endpoint accepts `?limit=` and returns a `next` cursor; every error body has `code`, `message` and, for approval failures, `step` (`policy`, `render`, `validate`, `reload`). A `GET /summary` endpoint returns counts by state and health in one call for "what needs attention" prompts.

## Host enrollment

A new host needs three things: the NetBird client, a setup key, and a firewall rule. All three fit in cloud-init; the same script runs by hand on an existing LXC.

**cloud-init user-data (Debian/Ubuntu guest):**

```yaml
#cloud-config
apt:
  sources:
    netbird:
      source: "deb https://pkgs.netbird.io/debian stable main"
      keyid: <netbird key id>
packages: [netbird, nftables]
write_files:
  - path: /etc/nftables.d/forecaddie.nft
    content: |
      table inet forecaddie {
        chain input {
          type filter hook input priority 0; policy drop;
          ct state established,related accept
          iif lo accept
          iifname "wt0" accept
          tcp dport 22 accept
          ip protocol icmp accept
          ip6 nexthdr icmpv6 accept
        }
      }
runcmd:
  - [netbird, up, --management-url, "https://netbird.example.net", --setup-key, "${SETUP_KEY}", --hostname, "${HOSTNAME}"]
  - [systemctl, enable, --now, nftables]
```

`${SETUP_KEY}` is injected per host by whatever creates the guest (Proxmox `cicustom`, Terraform, or an agent through the API), after `POST /setup-keys`. The key is single-use, so leaking the rendered user-data after boot is harmless.

**Proxmox-level rule.** For LXCs, and as a backstop for VMs, the same policy is set on the guest's Proxmox firewall: default DROP inbound, accept SSH from the management network, accept nothing else. Proxmox cannot see `wt0` (it is inside the guest), so the guest rule above is still required for overlay traffic; the Proxmox rule guarantees the LAN side even if the guest's own firewall is misconfigured or bypassed by Docker.

**Docker hosts.** Published ports must bind to the overlay IP. The enrollment script writes `/etc/forecaddie/overlay-ip` after `netbird up` so compose files can reference it:

```yaml
services:
  app:
    ports:
      - "${OVERLAY_IP}:8080:8080"
```

with `OVERLAY_IP` loaded from that file via an `env_file`. Peers registered with `kind: docker_host` get a UI warning on every app reminding of this, and the reconciler's TCP check from the Caddy node confirms the port is reachable over `wt0`. The Proxmox-level rule is what actually stops a `0.0.0.0` publish from leaking to the LAN.

**Existing hosts.** The same steps as a shell script (`enroll.sh`) with the setup key as its argument. Nothing else is installed on the host; there is no coordinator agent.

## Failure modes

The data plane (WireGuard sessions plus Caddy's last valid config) survives every control-plane failure. The table says what stops and what keeps working.

| Failure | Keeps working | Stops | Recovery |
| --- | --- | --- | --- |
| Coordinator down | All existing routes and tunnels | Approvals, status, audit, new enrollments | Restart; reconcilers reconverge on next pull |
| NetBird management down | Established tunnels | New peers joining, peer status (cache goes stale), policy changes | Restore management; peers reconnect on their own |
| Caddy node down | Other Caddy nodes, if any | Everything published through that node | Site-local outage; bring node back, fragment is on disk |
| Reconciler down on a node | Caddy serves last fragment | Route changes and offline substitution on that node | Restart; coordinator shows `last_reconcile_at` ageing |
| Caddy validate fails | Live config untouched | The one app whose approval failed; others unaffected | Error on the binding; fix input, re-approve |
| Peer loses overlay (client restart, upgrade) | Nothing on that peer | That peer's apps, briefly | Automatic; damped so a short blip does not reload Caddy |
| IdP down | API with already-issued tokens until expiry; reconciler (per-node token, not OIDC) | New logins, agent token refresh | Break-glass over localhost if needed |
| Default `All → All` policy reappears | Nothing changes immediately | Approvals are refused with an explicit error | Delete the policy in NetBird |
| SQLite file lost | Data plane | Everything in the registry | Restore from PBS; reconcile is deterministic, so Caddy and NetBird state regenerate from the restored registry |
| Overlay IP of a peer changes | Nothing, once reconciled | That peer's apps until the next poll | Poll detects it, triggers immediate reconcile |

Cold start order (NFR-13): NetBird management, then Caddy nodes, then the coordinator, then app hosts. Any other order converges within one reconcile interval because nothing holds state that another component cannot rebuild.

## Technology choices

Go for both binaries, SQLite for state, server-rendered HTML with a little HTMX for the UI. The reasoning is that the project's whole value is reliability and low operational weight, and every choice below trades features for that.

| Choice | Picked | Considered | Why |
| --- | --- | --- | --- |
| Language | Go | Rust, Python | Same ecosystem as Caddy and NetBird; single static binary; the Caddy admin API and NetBird API clients are trivial; caddyfile parsing can reuse Caddy's own packages for validating snippet and policy names. |
| State | SQLite (WAL mode) | Postgres | One file, backed up with the host by PBS, no second service to keep alive. Tens of peers, hundreds of apps. Migrate to Postgres only if a second coordinator instance is ever wanted. |
| UI | Server-rendered templates + HTMX | SPA (React/Svelte), TUI | No build pipeline, works without JS for reads, same auth path as the API. A TUI can come later as an API client. |
| Reconciler placement | On each Caddy node, pulling | Central push over overlay | Node keeps working without the coordinator; admin API stays on localhost; no inbound requirement to the node. |
| Caddy config method | Caddyfile fragments via `import`, validate, reload | Admin API JSON patches | Your Caddyfile stays the source of truth and stays readable; JSON patching the live config fights the Caddyfile adapter on the next full load. |
| Policy enforcement | NetBird policies | Host nftables generated by an agent | Enforced on both ends by NetBird, central, visible in NetBird's UI, no agent needed. |
| Agent auth | OIDC client credentials | API keys issued by the coordinator | One identity system; revocation and rotation happen in the IdP; audit names the agent. |
| Health probes | Caddy active health checks + reconciler TCP connect | Separate prober service | Caddy already does it for HTTP; adding a third component for L4 is not worth it. |
| Metrics | Prometheus text endpoint | Push to a hosted service | Matches whatever monitoring the homelab already has or gets; zero dependencies. |

**License: Apache-2.0.** Chosen over AGPLv3 because there is no intent to build a business around the project and no wish to deter anyone from hosting or wrapping it; Apache's explicit patent grant and wide corporate acceptance make it the easiest license for contributors. Contributions are accepted under the same license with no CLA. The NetBird client the enrollment snippet installs is BSD-3-Clause and the Caddy plugins used are Apache-2.0, so nothing in the dependency set imposes copyleft obligations.

## Rollout plan

Four phases, each leaving the system usable, with a single Caddy node and one disposable app host as the proving ground.

1. **Foundation.** Bring NetBird management back up (DNS-only records, PBS backup job). Delete the default `All → All` policy. Rebuild Caddy with `caddy-l4`, add the two `import` lines and an empty fragment pair, confirm `caddy validate` and reload are clean with nothing generated. Exit criterion: Caddy behaves exactly as before.
2. **One app, by hand.** Enroll one test LXC with a setup key and the nftables rule. Create the `caddy-site-a` and `host-<name>` groups and one policy by hand in NetBird. Hand-write the HTTP fragment for one app. Exit criterion: the app is reachable through Caddy over `wt0`, unreachable from the LAN, and a `netbird down` on the peer produces the 503 page.
3. **Coordinator and reconciler.** Registry, API, OIDC login, approval transaction, reconciler on the first node. Re-create the phase-2 app through the UI and confirm the generated fragment and policy are identical to the hand-written ones. Add audit, metrics, the Docker-host warning, and cloud-init enrollment. Exit criterion: an agent can register an app through the API, an admin approves it in the UI, and it goes live without touching NetBird or Caddy by hand.
4. **Per-site nodes and L4.** Enroll a Caddy node at a second site (and a third when convenient), add `caddy-<site>` groups and per-node fragments, publish one app through two nodes. Add SNI and port-mode L4 for one TCP service and one UDP service. Migrate existing Caddy upstream entries one at a time, deleting each from the hand-maintained Caddyfile as its generated route takes over. Exit criterion: no `reverse_proxy` line in the Caddyfile points at a LAN address.

Migration of existing services is deliberately last and one-at-a-time: each move is a destroy-and-create of a binding, and the old LAN route can stay in place until the new one is confirmed healthy, so there is never a moment where a service is only reachable the new way before the new way has been seen working.

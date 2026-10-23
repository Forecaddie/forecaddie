# Contributing

Thanks for looking. Forecaddie is in the design phase, so the most useful contributions right now are critiques of the two documents in `docs/`.

## Design feedback

Open a [Discussion](../../discussions) rather than an issue. Useful shapes of feedback:

- "This requirement conflicts with that one, because…"
- "NetBird / Caddy / caddy-l4 doesn't actually behave the way the design assumes; here is what it does…"
- "This failure mode is missing from the table…"
- "I run a setup like this and here is what would stop me adopting it…"

Please read the **Scope decisions** section of the README first. Requests to support a different reverse proxy or mesh will be closed with a pointer to it; the transport driver interface is the one extension point, and a second driver is welcome once there is a first.

## Code (once there is code)

- Go, standard library first, dependencies justified in the PR.
- Every change to the fragment renderer or the approval transaction comes with a test that exercises the failure path, not only the happy path.
- Keep the data model free of transport-specific identifiers; they go in `transport_ref`.
- Commit messages explain why, not what.

## Licensing

By contributing you agree that your contribution is licensed under the Apache License 2.0, the same as the project. No CLA.

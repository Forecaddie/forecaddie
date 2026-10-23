# Contributing

Thanks for looking. Forecaddie is in the design phase, so the most useful contributions right now are critiques of the two documents in `docs/`.

## Design feedback

Open a [Discussion](../../discussions) rather than an issue. Useful shapes of feedback:

- "This requirement conflicts with that one, because…"
- "NetBird / Caddy / caddy-l4 doesn't actually behave the way the design assumes; here is what it does…"
- "This failure mode is missing from the table…"
- "I run a setup like this and here is what would stop me adopting it…"

Please read the **Scope decisions** section of the README first. Requests to support a different reverse proxy or mesh will be closed with a pointer to it; the transport driver interface is the one extension point, and a second driver is welcome once there is a first.

## Reporting bugs

Bug reports for this project tend to include exactly the things that should not be public. Before posting, redact:

- NetBird API tokens, setup keys and peer keys
- Caddy admin credentials and the reconciler's per-node token
- OIDC client secrets, and any bearer tokens or session cookies in request dumps
- The break-glass token, if you have one

Hostnames, overlay IPs and Caddyfile fragments are usually fine to share, but replace your real domain with `example.net` if you would rather not publish it. A setup key is single-use and expires, but treat a leaked one as live until you have confirmed it was used or revoked.

Security problems do not go in issues or Discussions. See [SECURITY.md](SECURITY.md).

## Code (once there is code)

- Go, standard library first, dependencies justified in the PR.
- Every change to the fragment renderer or the approval transaction comes with a test that exercises the failure path, not only the happy path.
- Keep the data model free of transport-specific identifiers; they go in `transport_ref`.
- Commit messages explain why, not what.

## AI-assisted contributions

Contributions written with the help of AI tools are welcome. This project was itself designed with AI assistance, and it is built to be driven by automation agents, so it would be odd to pretend otherwise. The rules are the same as for any other contribution, with three points made explicit:

- **A human is responsible.** Whoever opens the PR or Discussion owns its content, including anything a tool generated. Pull requests opened autonomously by an agent with no human accountable for them will be closed.
- **Understand what you submit.** You should be able to explain every change in review and answer questions about it without going back to the tool. "The model wrote it" is not an answer to "why does this do that?"
- **Say so when it is substantial.** If a significant part of a change was generated rather than written, mention it in the PR description. This is not a mark against the change; it tells reviewers where to look harder, particularly at tests that may pass without testing anything.

AI-generated design critiques follow the same rules: check claims about NetBird, Caddy or caddy-l4 behaviour against the real software before posting them.

## How the maintainers use AI tools

Maintainers may use AI tools when reviewing contributions, drafting documentation and writing code. This means the contents of public issues, Discussions and pull requests may be processed by third-party AI services as part of that work. Do not put anything in a contribution that you would not want handled that way; this is one more reason to redact the items listed under **Reporting bugs**.

## Licensing and sign-off

By contributing you agree that your contribution is licensed under the Apache License 2.0, the same as the project. There is no CLA.

Instead, every commit must carry a `Signed-off-by` line certifying the [Developer Certificate of Origin](https://developercertificate.org/): that you wrote the change or otherwise have the right to submit it under the project's license. Add it with:

```
git commit -s
```

The sign-off must use your real name and an email address you can be reached at. It applies to AI-assisted work too: by signing off, you are stating that you have the right to submit what the tool produced, so do not sign off on generated code that reproduces material under an incompatible license.

Pull requests with unsigned commits cannot be merged. If you forgot, run `git rebase --signoff main` and force-push the branch.

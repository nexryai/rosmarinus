# Latest Misskey federation test

The `Federation compatibility` GitHub Actions workflow checks out Misskey's
current `develop` branch independently of this repository, builds it, and uses
Misskey's official two-server federation fixture as the network topology.
Rosmarinus is attached as `https://rosmarinus.test` through the compose overlay.

The Go integration test performs this real federation sequence:

1. create an administrator and a direct-message recipient on `a.test`;
2. resolve that Actor from Rosmarinus with signed ActivityPub GET;
3. enqueue and deliver `Follow` from the Rosmarinus `relay` Actor and
   dereference its Rosmarinus Follow activity;
4. wait for Misskey's signed `Accept(Follow)` to change MongoDB state to
   `accepted`;
5. create a public Misskey note;
6. wait for the delivered `Create(Note)` to be verified and stored by
   Rosmarinus;
7. react to that Misskey note from Rosmarinus, verify Misskey applies the
   delivered Like, dereference its Rosmarinus Like activity, then deliver
   `Undo(Like)` and verify Misskey removes the reaction;
8. approve Misskey's inbound follow, dereference a public Rosmarinus
   `Create` activity, and deliver that note;
9. react to that note from Misskey, verify the reaction in Rosmarinus, and
   dereference its Like activity;
10. deliver a `specified` Rosmarinus note to the second account's individual
   inbox, verify that Misskey exposes it to that recipient, and verify that its
   private `Create` activity endpoint returns `404`.

The workflow runs on relevant pull requests and pushes, weekly against latest
Misskey, and manually with an optional branch, tag, or commit in `misskey_ref`.

To run it locally, first clone Misskey into `./misskey`, build it, and prepare
its federation certificates as described in
`misskey/packages/backend/test-federation/README.md`. Generate an additional
certificate for `rosmarinus.test`, build `rosmarinus:test`, then run:

```sh
docker compose \
  -f misskey/packages/backend/test-federation/compose.yml \
  -f misskey/packages/backend/test-federation/compose.override.yaml \
  -f docker/federation/misskey-rosmarinus.compose.yml \
  up -d --wait --scale tester=0

docker compose \
  -f misskey/packages/backend/test-federation/compose.yml \
  -f misskey/packages/backend/test-federation/compose.override.yaml \
  -f docker/federation/misskey-rosmarinus.compose.yml \
  --profile rosmarinus-test run --rm rosmarinus-federation-test
```

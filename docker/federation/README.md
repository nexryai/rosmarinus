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
5. upload an avatar, update the followed Misskey Actor's profile, and verify the
   signed `Update(Person)` refreshes its Rosmarinus Actor document;
6. verify the avatar is fetched into GridFS and served from Rosmarinus's stable
   media URL;
7. create a public Misskey note;
8. wait for the delivered `Create(Note)` to be verified and stored by
   Rosmarinus;
9. renote that public Misskey note and verify Rosmarinus stores the delivered
   `Announce` with its resolved target reference;
10. publish a Misskey `Question` and verify Rosmarinus stores its ordered poll
   choices, vote counts, multiplicity, and expiration;
11. react to that Misskey note from Rosmarinus, verify Misskey applies the
   delivered Like, dereference its Rosmarinus Like activity, then deliver
   `Undo(Like)` and verify Misskey removes the reaction;
12. deliver `Undo(Follow)` from Rosmarinus, verify its MongoDB relationship is
   soft-deleted, and verify Misskey removes the relay from its followers;
13. approve Misskey's inbound follow, dereference a public Rosmarinus
   `Create(Question)` activity, and deliver that poll;
14. vote on that poll from Misskey and verify Rosmarinus stores the inbound
   reply Note as a poll vote;
15. react to that note from Misskey, verify the reaction in Rosmarinus, and
   dereference its Like activity;
16. delete that Rosmarinus note and verify Misskey applies the delivered
   `Delete(Tombstone)`;
17. deliver a `specified` Rosmarinus note to the second account's individual
   inbox, verify that Misskey exposes it to that recipient, and verify that its
   private `Create` activity endpoint returns `404`.

Media downloads default to 20 MiB and a one-minute operation timeout through
`MEDIA_MAX_BYTES` and `MEDIA_FETCH_TIMEOUT`. Production deployments should
leave `MEDIA_ALLOWED_PRIVATE_NETWORKS` empty. The federation fixture sets it to
the RFC1918 Docker ranges so Rosmarinus can fetch test-only Misskey media from
the isolated compose network.

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

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
6. verify Rosmarinus preserves the validated direct avatar URL and that the
   frontend-facing source resolves without backend caching or image processing;
7. create a public Misskey note;
8. wait for the delivered `Create(Note)` to be verified and stored by
   Rosmarinus;
9. pin and unpin that Misskey note and verify Rosmarinus applies the delivered
   `Add`/`Remove` activities to the remote Actor's `featuredNoteIds`;
10. renote that public Misskey note and verify Rosmarinus stores the delivered
   `Announce` with its resolved target reference;
11. publish a Misskey `Question` and verify Rosmarinus stores its ordered poll
   choices, vote counts, multiplicity, and expiration;
12. react to that Misskey note from Rosmarinus, verify Misskey applies the
   delivered Like, dereference its Rosmarinus Like activity, then deliver
   `Undo(Like)` and verify Misskey removes the reaction;
13. deliver `Undo(Follow)` from Rosmarinus, verify its MongoDB relationship is
   soft-deleted, and verify Misskey removes the relay from its followers;
14. approve Misskey's inbound follow and verify Misskey applies the delivered
   `Accept`;
15. create an account-owned Rosmarinus Actor, approve a Misskey Follow, update
   its profile, and verify Misskey applies the delivered full `Update` including
   its name, cat flag, and bot Actor type;
16. deliver `Block` from Rosmarinus, verify Misskey removes its Follow and
   exposes the blocked relationship, then deliver `Undo(Block)` and prove the
   Misskey Actor can follow Rosmarinus again;
17. renote the stored Misskey Note from Rosmarinus, dereference and deliver its
   `Announce`, then delete it and verify Misskey applies `Undo(Announce)`;
18. dereference a public Rosmarinus `Create(Question)` with a local custom-emoji
   tag, verify its simple MFM is emitted as safe HTML without redundant Misskey
   source metadata, and deliver that poll;
19. publish advanced MFM containing bold and ruby syntax, verify its safe HTML
   and `_misskey_content`/`source` compatibility fields, and confirm latest
   Misskey stores the original MFM text;
20. vote on that poll from Misskey and verify Rosmarinus stores the inbound
   reply Note as a poll vote;
21. react to that note from Misskey, verify the reaction in Rosmarinus, and
   dereference its Like activity;
22. delete that Rosmarinus note, verify its Poll/votes/reactions are cleaned,
   and verify Misskey applies the delivered `Delete(Tombstone)`;
23. deliver a `specified` Rosmarinus note to the second account's individual
   inbox, verify that Misskey exposes it to that recipient, and verify that its
   private `Create` activity endpoint returns `404`.
24. verify Rosmarinus registers `a.test`, stores its latest NodeInfo software
   metadata, tracks authenticated receive/successful delivery
   timestamps and status, and keeps per-instance relationship counts current.

Rosmarinus stores validated remote media source URLs directly; it does not
download, transform, or proxy images. Instance NodeInfo, root HTML, and manifest
discovery defaults to a 30-second operation timeout. The federation fixture's
`MEDIA_ALLOWED_PRIVATE_NETWORKS` value is retained only as the private-network
allowlist used by that metadata fetcher. Production deployments should leave it
empty. Override the metadata timeout with `INSTANCE_METADATA_TIMEOUT` when
necessary.

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

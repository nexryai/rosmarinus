const databaseName = process.env.ROSMARINUS_MONGO_DATABASE || "rosmarinus_federation";
const rosmarinusUsername = process.env.ROSMARINUS_MONGO_USERNAME;
const rosmarinusPassword = process.env.ROSMARINUS_MONGO_PASSWORD;

if (!rosmarinusUsername || !rosmarinusPassword) {
  throw new Error("Rosmarinus MongoDB username and password are required");
}

const applicationDB = db.getSiblingDB(databaseName);

const rosmarinusCollections = [
  "accounts",
  "sessions",
  "webauthn_challenges",
  "ui_settings",
  "actor_settings",
  "actors",
  "actor_profiles",
  "actor_public_keys",
  "notes",
  "polls",
  "poll_votes",
  "reactions",
  "follows",
  "follow_requests",
  "blocks",
  "emojis",
  "media",
  "instances",
  "abuse_reports",
  "notifications",
  "api_idempotency_receipts",
];

const rosmarinusInternalCollections = [
  "inbox_activity_receipts",
  "media_fs.files",
  "media_fs.chunks",
];

const writeActions = [
  "find",
  "insert",
  "remove",
  "update",
  "createCollection",
  "createIndex",
  "dropIndex",
  "collMod",
];

applicationDB.createRole({
  role: "rosmarinusService",
  privileges: [
    ...rosmarinusCollections.map((collection) => ({
      resource: { db: databaseName, collection },
      actions: writeActions,
    })),
    ...rosmarinusInternalCollections.map((collection) => ({
      resource: { db: databaseName, collection },
      // GridFS checks existing indexes before the first upload in each process.
      actions: collection.startsWith("media_fs.") ? [...writeActions, "listIndexes"] : writeActions,
    })),
  ],
  roles: [],
});

applicationDB.createUser({
  user: rosmarinusUsername,
  pwd: rosmarinusPassword,
  roles: [{ role: "rosmarinusService", db: databaseName }],
});

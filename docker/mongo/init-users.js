const databaseName = process.env.ROSMARINUS_MONGO_DATABASE || "rosmarinus_federation";
const rosmarinusUsername = process.env.ROSMARINUS_MONGO_USERNAME;
const rosmarinusPassword = process.env.ROSMARINUS_MONGO_PASSWORD;
const salviaUsername = process.env.SALVIA_MONGO_USERNAME;
const salviaPassword = process.env.SALVIA_MONGO_PASSWORD;

if (!rosmarinusUsername || !rosmarinusPassword || !salviaUsername || !salviaPassword) {
  throw new Error("Rosmarinus and Salvia MongoDB usernames and passwords are required");
}

const applicationDB = db.getSiblingDB(databaseName);

const rosmarinusCollections = [
  "actors",
  "actor_profiles",
  "actor_public_keys",
  "notes",
  "polls",
  "reactions",
  "follows",
  "follow_requests",
  "blocks",
  "emojis",
  "media",
  "instances",
  "abuse_reports",
  "notifications",
  "connector_command_receipts",
];

const salviaCollections = [
  "salvia_accounts",
  "salvia_sessions",
  "salvia_ui_settings",
  "salvia_actor_settings",
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
    {
      resource: { db: databaseName, collection: "salvia_accounts" },
      actions: ["find"],
    },
  ],
  roles: [],
});

applicationDB.createRole({
  role: "salviaService",
  privileges: [
    ...salviaCollections.map((collection) => ({
      resource: { db: databaseName, collection },
      actions: writeActions,
    })),
    ...rosmarinusCollections.map((collection) => ({
      resource: { db: databaseName, collection },
      actions: ["find"],
    })),
  ],
  roles: [],
});

applicationDB.createUser({
  user: rosmarinusUsername,
  pwd: rosmarinusPassword,
  roles: [{ role: "rosmarinusService", db: databaseName }],
});

applicationDB.createUser({
  user: salviaUsername,
  pwd: salviaPassword,
  roles: [{ role: "salviaService", db: databaseName }],
});

import { z } from "zod";

const strings = z
    .array(z.string())
    .nullish()
    .transform((value) => value ?? []);

// Older Rosmarinus responses exposed Go field names for nonempty profile fields.
const profileFieldSchema = z.union([z.object({ name: z.string(), value: z.string() }), z.object({ Name: z.string(), Value: z.string() }).transform((field) => ({ name: field.Name, value: field.Value }))]);

export const actorSchema = z.object({
    id: z.string(),
    username: z.string(),
    name: z.string().default(""),
    summary: z.string().default(""),
    url: z.string().default(""),
    profile_fields: z
        .array(profileFieldSchema)
        .nullish()
        .transform((value) => value ?? []),
    birthday: z.string().default(""),
    location: z.string().default(""),
    avatar_url: z.string().default(""),
    banner_url: z.string().default(""),
    tags: strings,
    emoji_names: strings,
    is_bot: z.boolean().default(false),
    is_cat: z.boolean().default(false),
    is_locked: z.boolean().default(false),
    is_discoverable: z.boolean().default(false),
    type: z.string().default("Person"),
    uri: z.string(),
    moved_to_uri: z.string().default(""),
    is_suspended: z.boolean().default(false),
});

export type Actor = z.infer<typeof actorSchema>;

const noteReferenceSchema = z.object({
    id: z.string(),
    uri: z.string(),
    text: z.string(),
    content_warning: z.string().nullish(),
    sensitive: z.boolean(),
    visibility: z.string(),
    created_at: z.string(),
    author: actorSchema.optional(),
});

export const noteSchema = z.object({
    id: z.string(),
    uri: z.string(),
    text: z.string(),
    content_warning: z.string().nullish(),
    sensitive: z.boolean(),
    reply_id: z.string().optional(),
    quote_id: z.string().optional(),
    renote_id: z.string().optional(),
    visibility: z.string(),
    mention_uris: strings,
    hashtags: strings,
    emojis: z
        .array(z.object({ name: z.string(), url: z.string(), media_type: z.string().optional() }))
        .nullish()
        .transform((value) => value ?? []),
    attachments: z
        .array(z.object({ type: z.string().optional(), media_type: z.string().optional(), url: z.string(), name: z.string().optional(), width: z.number().optional(), height: z.number().optional(), sensitive: z.boolean() }))
        .nullish()
        .transform((value) => value ?? []),
    created_at: z.string(),
    published_at: z.string().nullish(),
    author: actorSchema.optional(),
    poll: z
        .object({
            choices: z.array(z.object({ index: z.number(), text: z.string(), votes: z.number(), voted: z.boolean() })),
            multiple: z.boolean(),
            expires_at: z.string().nullish(),
            expired: z.boolean(),
        })
        .optional(),
    reactions: z
        .array(z.object({ reaction: z.string(), count: z.number(), reacted: z.boolean() }))
        .nullish()
        .transform((value) => value ?? []),
    reply: noteReferenceSchema.optional(),
    quote: noteReferenceSchema.optional(),
    renote: noteReferenceSchema.optional(),
});

export type Note = z.infer<typeof noteSchema>;

export const sessionSchema = z.object({
    account_id: z.string(),
    csrf_token: z.string(),
    username: z.string().default(""),
    display_name: z.string().default(""),
});

export type Session = z.infer<typeof sessionSchema>;

export const accountSettingsSchema = z.object({
    theme: z.enum(["yellow", "light", "dark", "system"]),
    reduce_motion: z.boolean(),
    compact_mode: z.boolean(),
    selected_actor_id: z.string().optional(),
    updated_at: z.string().optional(),
});

export type AccountSettings = z.infer<typeof accountSettingsSchema>;

export const actorSettingsSchema = z.object({
    actor_id: z.string(),
    default_visibility: z.enum(["public", "home", "followers"]),
    show_content_warning: z.boolean(),
    display_order: z.number(),
    color: z.string().optional(),
    pinned: z.boolean(),
    updated_at: z.string().optional(),
});

export type ActorSettings = z.infer<typeof actorSettingsSchema>;

export const notificationSchema = z.object({
    id: z.string(),
    actor_id: z.string(),
    kind: z.string(),
    note_id: z.string().optional(),
    created_at: z.string(),
    is_read: z.boolean(),
    read_at: z.string().nullish(),
    source: actorSchema.optional(),
    note: noteSchema.optional(),
});

export type Notification = z.infer<typeof notificationSchema>;

export const connectionSchema = z.object({
    id: z.string(),
    status: z.string(),
    created_at: z.string(),
    accepted_at: z.string().nullish(),
    actor: actorSchema,
});

export type Connection = z.infer<typeof connectionSchema>;

export const emojiSchema = z.object({ name: z.string(), url: z.string(), media_type: z.string().optional() });
export type Emoji = z.infer<typeof emojiSchema>;

export const profileSchema = z.object({ actor: actorSchema, followers_count: z.number(), following_count: z.number(), follow_status: z.string().default(""), blocked_by_viewer: z.boolean().default(false) });
export type Profile = z.infer<typeof profileSchema>;

export const instanceSchema = z.object({ name: z.string(), host: z.string(), url: z.string(), version: z.string(), passkey_only: z.literal(true) });
export type Instance = z.infer<typeof instanceSchema>;

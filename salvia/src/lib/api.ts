import { z } from "zod";

import {
    type AccountSettings,
    type Actor,
    type ActorSettings,
    type Antenna,
    accountSettingsSchema,
    actorSchema,
    actorSettingsSchema,
    antennaSchema,
    type Connection,
    connectionSchema,
    type Emoji,
    emojiSchema,
    type Instance,
    instanceSchema,
    type ManagedEmoji,
    managedEmojiSchema,
    type Note,
    type Notification,
    noteSchema,
    notificationSchema,
    type Profile,
    profileSchema,
    type ReactionActor,
    reactionActorSchema,
    type Session,
    sessionSchema,
} from "./schema";

const API_BASE = (import.meta.env.VITE_API_BASE_URL || "/api/v1").replace(/\/$/, "");
const pendingMutationIntents = new Map<string, string>();

const envelope = <T extends z.ZodType>(schema: T) => z.object({ version: z.literal(1), data: schema });
const pageEnvelope = <T extends z.ZodType>(schema: T) => z.object({ version: z.literal(1), data: z.array(schema), next: z.string().default("") });

export class ApiError extends Error {
    readonly status: number;
    readonly code: string;

    constructor(status: number, code: string, message: string) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.code = code;
    }
}

type RequestOptions = Omit<RequestInit, "body"> & { body?: unknown; csrf?: string; idempotent?: boolean };

export type CreatePostInput = {
    text?: string;
    visibility: string;
    content_warning?: string;
    sensitive?: boolean;
    in_reply_to_uri?: string;
    quote_uri?: string;
    renote_id?: string;
    mention_uris?: string[];
    hashtags?: string[];
    emoji_names?: string[];
    media_ids?: string[];
    poll?: { choices: string[]; multiple?: boolean; expires_at?: string };
};

export type AntennaInput = Omit<Antenna, "id" | "created_at" | "updated_at">;

async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}): Promise<T> {
    const headers = new Headers(options.headers);
    const isForm = options.body instanceof FormData;
    if (options.body !== undefined && !isForm) headers.set("Content-Type", "application/json");
    if (options.csrf) headers.set("X-CSRF-Token", options.csrf);
    const intentSignature = options.idempotent ? `${options.method || "GET"}:${path}:${JSON.stringify(options.body)}` : "";
    if (intentSignature) {
        const key = pendingMutationIntents.get(intentSignature) || crypto.randomUUID();
        pendingMutationIntents.set(intentSignature, key);
        headers.set("Idempotency-Key", key);
    }
    const body: BodyInit | undefined = options.body === undefined ? undefined : isForm ? (options.body as FormData) : JSON.stringify(options.body);
    const response = await fetch(`${API_BASE}${path}`, {
        ...options,
        body,
        credentials: "same-origin",
        headers,
    });
    if (intentSignature) pendingMutationIntents.delete(intentSignature);
    const payload = response.status === 204 ? undefined : await response.json().catch(() => undefined);
    if (!response.ok) {
        const parsed = z.object({ error: z.object({ code: z.string(), message: z.string() }) }).safeParse(payload);
        if (response.status === 401) window.dispatchEvent(new Event("salvia:session-lost"));
        throw new ApiError(response.status, parsed.success ? parsed.data.error.code : "request_failed", parsed.success ? parsed.data.error.message : `Request failed (${response.status})`);
    }
    return schema.parse(payload);
}

const query = (values: Record<string, string | undefined>) => {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(values)) if (value) params.set(key, value);
    const encoded = params.toString();
    return encoded ? `?${encoded}` : "";
};

export const api = {
    eventURL: `${API_BASE}/events`,
    setupStatus: () => request("/auth/setup", envelope(z.object({ setup_required: z.boolean() }))),
    setupStart: (username: string, displayName: string) => request("/auth/setup/start", envelope(z.object({ ceremony_id: z.string(), public_key: z.unknown() })), { method: "POST", body: { username, display_name: displayName } }),
    setupFinish: (ceremonyID: string, credential: unknown) => request("/auth/setup/finish", envelope(z.object({ csrf_token: z.string() })), { method: "POST", body: credential, headers: { "X-WebAuthn-Ceremony-ID": ceremonyID } }),
    loginStart: () => request("/auth/login/start", envelope(z.object({ ceremony_id: z.string(), public_key: z.unknown() })), { method: "POST" }),
    loginFinish: (ceremonyID: string, credential: unknown) => request("/auth/login/finish", envelope(z.object({ csrf_token: z.string() })), { method: "POST", body: credential, headers: { "X-WebAuthn-Ceremony-ID": ceremonyID } }),
    logout: (csrf: string) => request("/auth/logout", z.undefined(), { method: "POST", csrf }),
    session: async (): Promise<Session> => (await request("/session", envelope(sessionSchema))).data,
    actors: async (): Promise<Actor[]> => (await request("/actors?limit=100", pageEnvelope(actorSchema))).data,
    createActor: async (csrf: string, username: string, name: string): Promise<void> => {
        await request("/actors", envelope(z.unknown()), { method: "POST", body: { username, name, type: "Person" }, csrf, idempotent: true });
    },
    updateActor: async (csrf: string, actorID: string, patch: Record<string, unknown>): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}`, envelope(z.unknown()), { method: "PATCH", body: patch, csrf, idempotent: true });
    },
    deleteActor: async (csrf: string, actorID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}`, envelope(z.unknown()), { method: "DELETE", csrf, idempotent: true });
    },
    timeline: async (kind: "home" | "public", actorID: string, after = "", signal?: AbortSignal): Promise<{ data: Note[]; next: string }> => {
        const result = await request(`/timelines/${kind}${query({ actor_id: actorID, after, limit: "30" })}`, pageEnvelope(noteSchema), { signal });
        return { data: result.data, next: result.next };
    },
    createPost: async (csrf: string, actorID: string, input: CreatePostInput, intentKey: string = crypto.randomUUID()): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/posts`, envelope(z.unknown()), { method: "POST", body: input, csrf, headers: { "Idempotency-Key": intentKey } });
    },
    prepareMediaUpload: async (csrf: string, actorID: string, input: { name: string; content_type: string; size: number; sha256: string; width: number; height: number }, intentKey: string) => {
        const result = await request(
            `/actors/${encodeURIComponent(actorID)}/media`,
            envelope(
                z.object({
                    id: z.string(),
                    url: z.string(),
                    state: z.string(),
                    upload_url: z.string(),
                    upload_headers: z.record(z.string(), z.string()),
                    expires_at: z.string(),
                }),
            ),
            { method: "POST", body: input, csrf, headers: { "Idempotency-Key": intentKey } },
        );
        return result.data;
    },
    completeMediaUpload: async (csrf: string, actorID: string, mediaID: string): Promise<{ id: string; url: string }> => {
        const result = await request(`/actors/${encodeURIComponent(actorID)}/media/${encodeURIComponent(mediaID)}/complete`, envelope(z.object({ id: z.string(), url: z.string() }).passthrough()), { method: "POST", csrf });
        return result.data;
    },
    deleteMedia: async (csrf: string, actorID: string, mediaID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/media/${encodeURIComponent(mediaID)}`, z.undefined(), { method: "DELETE", csrf });
    },
    deletePost: async (csrf: string, actorID: string, noteID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/posts/${encodeURIComponent(noteID)}`, envelope(z.unknown()), { method: "DELETE", csrf, idempotent: true });
    },
    react: async (csrf: string, actorID: string, noteID: string, reaction: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/reactions/${encodeURIComponent(noteID)}`, envelope(z.unknown()), { method: "PUT", body: { reaction }, csrf, idempotent: true });
    },
    unreact: async (csrf: string, actorID: string, noteID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/reactions/${encodeURIComponent(noteID)}`, envelope(z.unknown()), { method: "DELETE", csrf, idempotent: true });
    },
    vote: async (csrf: string, actorID: string, noteID: string, choice: number): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/poll-votes`, envelope(z.unknown()), { method: "POST", body: { note_id: noteID, choice }, csrf, idempotent: true });
    },
    notifications: async (actorID: string, signal?: AbortSignal): Promise<Notification[]> => (await request(`/actors/${encodeURIComponent(actorID)}/notifications?limit=50`, pageEnvelope(notificationSchema), { signal })).data,
    accountNotifications: async (signal?: AbortSignal): Promise<Notification[]> => (await request("/notifications?limit=50", pageEnvelope(notificationSchema), { signal })).data,
    notificationUnreadCount: async (actorID: string, signal?: AbortSignal): Promise<number> => (await request(`/actors/${encodeURIComponent(actorID)}/notifications/unread-count`, envelope(z.object({ count: z.number().int().nonnegative() })), { signal })).data.count,
    markNotificationRead: async (csrf: string, actorID: string, notificationID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/notifications/${encodeURIComponent(notificationID)}`, envelope(z.unknown()), { method: "PATCH", body: { is_read: true }, csrf, idempotent: true });
    },
    markAllNotificationsRead: async (csrf: string, actorID: string): Promise<number> => (await request(`/actors/${encodeURIComponent(actorID)}/notifications`, envelope(z.object({ count: z.number().int().nonnegative() })), { method: "PATCH", body: { is_read: true }, csrf, idempotent: true })).data.count,
    followRequests: async (actorID: string): Promise<Connection[]> => (await request(`/actors/${encodeURIComponent(actorID)}/follow-requests?limit=50`, pageEnvelope(connectionSchema))).data,
    sentFollowRequests: async (actorID: string): Promise<Connection[]> => (await request(`/actors/${encodeURIComponent(actorID)}/follow-requests/sent?limit=50`, pageEnvelope(connectionSchema))).data,
    following: async (actorID: string): Promise<Connection[]> => (await request(`/actors/${encodeURIComponent(actorID)}/following?limit=100`, pageEnvelope(connectionSchema))).data,
    decideFollowRequest: async (csrf: string, actorID: string, followerID: string, status: "accepted" | "rejected" | "rejected_and_blocked"): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follow-requests/${encodeURIComponent(followerID)}`, envelope(z.unknown()), { method: "PATCH", body: { status }, csrf, idempotent: true });
    },
    profile: async (viewerID: string, actorID: string, signal?: AbortSignal): Promise<Profile> => (await request(`/profiles/${encodeURIComponent(actorID)}${query({ actor_id: viewerID })}`, envelope(profileSchema), { signal })).data,
    profileNotes: async (viewerID: string, actorID: string, after = "", signal?: AbortSignal): Promise<{ data: Note[]; next: string }> => {
        const result = await request(`/profiles/${encodeURIComponent(actorID)}/notes${query({ actor_id: viewerID, after, limit: "30" })}`, pageEnvelope(noteSchema), { signal });
        return { data: result.data, next: result.next };
    },
    resolveProfile: async (csrf: string, actorID: string, target: string): Promise<Profile> => (await request(`/actors/${encodeURIComponent(actorID)}/profiles/resolve`, envelope(profileSchema), { method: "POST", body: { target }, csrf })).data,
    profileConnections: async (viewerID: string, actorID: string, kind: "followers" | "following"): Promise<Connection[]> => (await request(`/profiles/${encodeURIComponent(actorID)}/${kind}${query({ actor_id: viewerID, limit: "100" })}`, pageEnvelope(connectionSchema))).data,
    follow: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follows`, envelope(z.unknown()), { method: "POST", body: { target }, csrf, idempotent: true });
    },
    unfollow: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follows`, envelope(z.unknown()), { method: "DELETE", body: { target }, csrf, idempotent: true });
    },
    antennas: async (actorID: string): Promise<Antenna[]> => (await request(`/actors/${encodeURIComponent(actorID)}/antennas`, envelope(z.array(antennaSchema)))).data,
    antennaNotes: async (actorID: string, antennaID: string, after = "", signal?: AbortSignal): Promise<{ data: Note[]; next: string }> => {
        const result = await request(`/actors/${encodeURIComponent(actorID)}/antennas/${encodeURIComponent(antennaID)}/notes${query({ after, limit: "30" })}`, pageEnvelope(noteSchema), { signal });
        return { data: result.data, next: result.next };
    },
    createAntenna: async (csrf: string, actorID: string, input: AntennaInput): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/antennas`, envelope(z.unknown()), { method: "POST", body: input, csrf, idempotent: true });
    },
    updateAntenna: async (csrf: string, actorID: string, antennaID: string, input: AntennaInput): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/antennas/${encodeURIComponent(antennaID)}`, envelope(z.unknown()), { method: "PATCH", body: input, csrf, idempotent: true });
    },
    deleteAntenna: async (csrf: string, actorID: string, antennaID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/antennas/${encodeURIComponent(antennaID)}`, envelope(z.unknown()), { method: "DELETE", csrf, idempotent: true });
    },
    block: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/blocks`, envelope(z.unknown()), { method: "POST", body: { target }, csrf, idempotent: true });
    },
    unblock: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/blocks`, envelope(z.unknown()), { method: "DELETE", body: { target }, csrf, idempotent: true });
    },
    mute: async (csrf: string, actorID: string, target: string, expiresAt: string | null): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/mutes`, envelope(z.unknown()), { method: "POST", body: { target, expires_at: expiresAt }, csrf, idempotent: true });
    },
    unmute: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/mutes`, envelope(z.unknown()), { method: "DELETE", body: { target }, csrf, idempotent: true });
    },
    note: async (actorID: string, noteID: string, signal?: AbortSignal): Promise<Note> => (await request(`/notes/${encodeURIComponent(noteID)}${query({ actor_id: actorID })}`, envelope(noteSchema), { signal })).data,
    thread: async (actorID: string, noteID: string, signal?: AbortSignal, limit = 100): Promise<Note[]> => (await request(`/notes/${encodeURIComponent(noteID)}/thread${query({ actor_id: actorID, limit: String(limit) })}`, pageEnvelope(noteSchema), { signal })).data,
    noteReactions: async (actorID: string, noteID: string, reaction: string, signal?: AbortSignal): Promise<ReactionActor[]> => {
        const result = await request(`/notes/${encodeURIComponent(noteID)}/reactions${query({ actor_id: actorID, reaction, limit: "10" })}`, pageEnvelope(reactionActorSchema), { signal });
        return result.data;
    },
    emojiCatalog: async (scope: "local" | "remote", filters: { after?: string; query?: string; host?: string } = {}): Promise<{ data: ManagedEmoji[]; next: string }> => {
        const result = await request(`/emojis${query({ scope, after: filters.after, query: filters.query, host: filters.host, limit: "30" })}`, pageEnvelope(managedEmojiSchema));
        return { data: result.data, next: result.next };
    },
    emojis: async (): Promise<Emoji[]> => (await request("/emojis?scope=local&limit=100", pageEnvelope(emojiSchema))).data,
    createEmoji: async (csrf: string, actorID: string, name: string, mediaID: string): Promise<ManagedEmoji> => (await request("/emojis", envelope(managedEmojiSchema), { method: "POST", body: { actor_id: actorID, name, media_id: mediaID }, csrf })).data,
    updateEmoji: async (csrf: string, actorID: string, id: string, name: string, mediaID = ""): Promise<ManagedEmoji> => (await request(`/emojis/${encodeURIComponent(id)}`, envelope(managedEmojiSchema), { method: "PATCH", body: { actor_id: actorID, name, ...(mediaID ? { media_id: mediaID } : {}) }, csrf })).data,
    deleteEmoji: async (csrf: string, id: string): Promise<void> => {
        await request(`/emojis/${encodeURIComponent(id)}`, envelope(z.object({ id: z.string() })), { method: "DELETE", csrf });
    },
    importEmoji: async (csrf: string, actorID: string, sourceID: string, name: string): Promise<ManagedEmoji> => (await request("/emojis/import", envelope(managedEmojiSchema), { method: "POST", body: { actor_id: actorID, source_id: sourceID, name }, csrf })).data,
    instance: async (): Promise<Instance> => (await request("/instance", envelope(instanceSchema))).data,
    accountSettings: async (): Promise<AccountSettings> => (await request("/settings", envelope(accountSettingsSchema))).data,
    updateAccountSettings: async (csrf: string, patch: Partial<AccountSettings>): Promise<AccountSettings> => (await request("/settings", envelope(accountSettingsSchema), { method: "PATCH", body: patch, csrf })).data,
    actorSettings: async (actorID: string): Promise<ActorSettings> => (await request(`/actors/${encodeURIComponent(actorID)}/settings`, envelope(actorSettingsSchema))).data,
    updateActorSettings: async (csrf: string, actorID: string, patch: Partial<ActorSettings>): Promise<ActorSettings> => (await request(`/actors/${encodeURIComponent(actorID)}/settings`, envelope(actorSettingsSchema), { method: "PATCH", body: patch, csrf })).data,
};

export type Page = "home" | "emojis" | "antennas" | "notifications" | "follow-requests" | "settings" | "profile" | "note";

import { z } from "zod";

import {
    type AccountSettings,
    type Actor,
    type ActorSettings,
    accountSettingsSchema,
    actorSchema,
    actorSettingsSchema,
    type Connection,
    connectionSchema,
    type Emoji,
    emojiSchema,
    type Instance,
    instanceSchema,
    type Note,
    type Notification,
    noteSchema,
    notificationSchema,
    type Profile,
    profileSchema,
    type Session,
    sessionSchema,
} from "./schema";

const API_BASE = (import.meta.env.VITE_API_BASE_URL || "/api/v1").replace(/\/$/, "");

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

async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}): Promise<T> {
    const headers = new Headers(options.headers);
    if (options.body !== undefined) headers.set("Content-Type", "application/json");
    if (options.csrf) headers.set("X-CSRF-Token", options.csrf);
    if (options.idempotent) headers.set("Idempotency-Key", crypto.randomUUID());
    const response = await fetch(`${API_BASE}${path}`, {
        ...options,
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        credentials: "same-origin",
        headers,
    });
    const payload = response.status === 204 ? undefined : await response.json().catch(() => undefined);
    if (!response.ok) {
        const parsed = z.object({ error: z.object({ code: z.string(), message: z.string() }) }).safeParse(payload);
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
    timeline: async (kind: "home" | "public", actorID: string, after = ""): Promise<{ data: Note[]; next: string }> => {
        const result = await request(`/timelines/${kind}${query({ actor_id: actorID, after, limit: "30" })}`, pageEnvelope(noteSchema));
        return { data: result.data, next: result.next };
    },
    createPost: async (csrf: string, actorID: string, text: string, visibility: string, contentWarning?: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/posts`, envelope(z.unknown()), { method: "POST", body: { note_id: crypto.randomUUID(), text, visibility, ...(contentWarning ? { content_warning: contentWarning } : {}) }, csrf, idempotent: true });
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
    notifications: async (actorID: string): Promise<Notification[]> => (await request(`/actors/${encodeURIComponent(actorID)}/notifications?limit=50`, pageEnvelope(notificationSchema))).data,
    markNotificationRead: async (csrf: string, actorID: string, notificationID: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/notifications/${encodeURIComponent(notificationID)}`, envelope(z.unknown()), { method: "PATCH", body: { is_read: true }, csrf, idempotent: true });
    },
    followRequests: async (actorID: string): Promise<Connection[]> => (await request(`/actors/${encodeURIComponent(actorID)}/follow-requests?limit=50`, pageEnvelope(connectionSchema))).data,
    following: async (actorID: string): Promise<Connection[]> => (await request(`/actors/${encodeURIComponent(actorID)}/following?limit=100`, pageEnvelope(connectionSchema))).data,
    decideFollowRequest: async (csrf: string, actorID: string, followerID: string, status: "accepted" | "rejected"): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follow-requests/${encodeURIComponent(followerID)}`, envelope(z.unknown()), { method: "PATCH", body: { status }, csrf, idempotent: true });
    },
    profile: async (viewerID: string, actorID: string): Promise<Profile> => (await request(`/profiles/${encodeURIComponent(actorID)}${query({ actor_id: viewerID })}`, envelope(profileSchema))).data,
    follow: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follows`, envelope(z.unknown()), { method: "POST", body: { target }, csrf, idempotent: true });
    },
    unfollow: async (csrf: string, actorID: string, target: string): Promise<void> => {
        await request(`/actors/${encodeURIComponent(actorID)}/follows`, envelope(z.unknown()), { method: "DELETE", body: { target }, csrf, idempotent: true });
    },
    emojis: async (): Promise<Emoji[]> => (await request("/emojis?limit=100", pageEnvelope(emojiSchema))).data,
    instance: async (): Promise<Instance> => (await request("/instance", envelope(instanceSchema))).data,
    accountSettings: async (): Promise<AccountSettings> => (await request("/settings", envelope(accountSettingsSchema))).data,
    updateAccountSettings: async (csrf: string, patch: Partial<AccountSettings>): Promise<AccountSettings> => (await request("/settings", envelope(accountSettingsSchema), { method: "PATCH", body: patch, csrf })).data,
    actorSettings: async (actorID: string): Promise<ActorSettings> => (await request(`/actors/${encodeURIComponent(actorID)}/settings`, envelope(actorSettingsSchema))).data,
    updateActorSettings: async (csrf: string, actorID: string, patch: Partial<ActorSettings>): Promise<ActorSettings> => (await request(`/actors/${encodeURIComponent(actorID)}/settings`, envelope(actorSettingsSchema), { method: "PATCH", body: patch, csrf })).data,
};

export type Page = "home" | "public" | "notifications" | "follow-requests" | "settings" | "profile";

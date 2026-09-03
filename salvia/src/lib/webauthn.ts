const decodeBase64URL = (value: string): ArrayBuffer => {
    const padding = "=".repeat((4 - (value.length % 4)) % 4);
    const binary = atob(value.replace(/-/g, "+").replace(/_/g, "/") + padding);
    return Uint8Array.from(binary, (character) => character.charCodeAt(0)).buffer;
};

const encodeBase64URL = (value: ArrayBuffer | null): string | null => {
    if (!value) return null;
    const bytes = new Uint8Array(value);
    let binary = "";
    for (const byte of bytes) binary += String.fromCharCode(byte);
    return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};

const publicKeyOptions = (value: unknown): Record<string, unknown> => {
    if (!value || typeof value !== "object") throw new Error("パスキーのオプションが不正です");
    const record = value as Record<string, unknown>;
    const nested = record.publicKey;
    return nested && typeof nested === "object" ? (nested as Record<string, unknown>) : record;
};

export const createPasskey = async (value: unknown): Promise<unknown> => {
    if (!window.PublicKeyCredential) throw new Error("このブラウザはパスキーに対応していません");
    const raw = publicKeyOptions(value);
    const user = raw.user as Record<string, unknown>;
    const options: PublicKeyCredentialCreationOptions = {
        ...raw,
        challenge: decodeBase64URL(String(raw.challenge)),
        user: { ...user, id: decodeBase64URL(String(user.id)) } as PublicKeyCredentialUserEntity,
        excludeCredentials: Array.isArray(raw.excludeCredentials) ? raw.excludeCredentials.map((item) => ({ ...(item as PublicKeyCredentialDescriptor), id: decodeBase64URL(String((item as Record<string, unknown>).id)) })) : undefined,
    } as PublicKeyCredentialCreationOptions;
    const credential = await navigator.credentials.create({ publicKey: options });
    if (!(credential instanceof PublicKeyCredential)) throw new Error("パスキーの登録がキャンセルされました");
    const response = credential.response as AuthenticatorAttestationResponse;
    return {
        id: credential.id,
        rawId: encodeBase64URL(credential.rawId),
        type: credential.type,
        authenticatorAttachment: credential.authenticatorAttachment,
        response: {
            clientDataJSON: encodeBase64URL(response.clientDataJSON),
            attestationObject: encodeBase64URL(response.attestationObject),
            transports: response.getTransports?.() ?? [],
        },
        clientExtensionResults: credential.getClientExtensionResults(),
    };
};

export const getPasskey = async (value: unknown): Promise<unknown> => {
    if (!window.PublicKeyCredential) throw new Error("このブラウザはパスキーに対応していません");
    const raw = publicKeyOptions(value);
    const options: PublicKeyCredentialRequestOptions = {
        ...raw,
        challenge: decodeBase64URL(String(raw.challenge)),
        allowCredentials: Array.isArray(raw.allowCredentials) ? raw.allowCredentials.map((item) => ({ ...(item as PublicKeyCredentialDescriptor), id: decodeBase64URL(String((item as Record<string, unknown>).id)) })) : undefined,
    } as PublicKeyCredentialRequestOptions;
    const credential = await navigator.credentials.get({ publicKey: options });
    if (!(credential instanceof PublicKeyCredential)) throw new Error("パスキー認証がキャンセルされました");
    const response = credential.response as AuthenticatorAssertionResponse;
    return {
        id: credential.id,
        rawId: encodeBase64URL(credential.rawId),
        type: credential.type,
        authenticatorAttachment: credential.authenticatorAttachment,
        response: {
            clientDataJSON: encodeBase64URL(response.clientDataJSON),
            authenticatorData: encodeBase64URL(response.authenticatorData),
            signature: encodeBase64URL(response.signature),
            userHandle: encodeBase64URL(response.userHandle),
        },
        clientExtensionResults: credential.getClientExtensionResults(),
    };
};

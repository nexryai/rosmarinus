import type { CSSProperties } from "react";

import { IconUserSearch } from "@tabler/icons-react";

import { RemoteUserSearch } from "../components/RemoteUserSearch";
import { PageHeader } from "../components/ui";

const styles = {
    headerIcon: {
        width: "1.5rem",
        height: "1.5rem",
    },
} satisfies Record<string, CSSProperties>;

export function UserSearchPage({ actorID, csrf, onOpenProfile }: { actorID: string; csrf: string; onOpenProfile: (actorID: string) => void }) {
    return (
        <>
            <PageHeader eyebrow="連合ネットワーク" title="ユーザー検索" trailing={<IconUserSearch style={styles.headerIcon} />} />
            <RemoteUserSearch actorID={actorID} csrf={csrf} onOpenProfile={onOpenProfile} />
        </>
    );
}

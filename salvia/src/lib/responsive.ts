import { useEffect, useState } from "react";

export const mobileMediaQuery = "(width < 64rem)";

const matchesMobile = () => window.matchMedia?.(mobileMediaQuery).matches ?? false;

export function useIsMobile() {
    const [isMobile, setIsMobile] = useState(matchesMobile);

    useEffect(() => {
        const media = window.matchMedia?.(mobileMediaQuery);
        if (!media) return;
        const update = () => setIsMobile(media.matches);
        update();
        if (media.addEventListener) {
            media.addEventListener("change", update);
            return () => media.removeEventListener("change", update);
        }
        media.addListener(update);
        return () => media.removeListener(update);
    }, []);

    return isMobile;
}

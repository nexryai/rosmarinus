import { StrictMode } from "react";

import { createRoot } from "react-dom/client";

import App from "./App.tsx";
import { ErrorBoundary } from "./components/ui.tsx";
import { GlobalStyles } from "./globalStyles";
import { registerServiceWorker } from "./lib/pwa";

const root = document.getElementById("root");
if (!root) throw new Error("Rosemary root element was not found");

createRoot(root).render(
    <StrictMode>
        <GlobalStyles />
        <ErrorBoundary>
            <App />
        </ErrorBoundary>
    </StrictMode>,
);

registerServiceWorker();

import { StrictMode } from "react";

import { createRoot } from "react-dom/client";

import App from "./App.tsx";
import { ErrorBoundary } from "./components/ui.tsx";
import { SalviaStyles } from "./styles.tsx";

const root = document.getElementById("root");
if (!root) throw new Error("Salvia root element was not found");

createRoot(root).render(
    <StrictMode>
        <SalviaStyles />
        <ErrorBoundary>
            <App />
        </ErrorBoundary>
    </StrictMode>,
);

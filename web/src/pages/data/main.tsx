import { createRoot } from "react-dom/client";
import { AppLayout } from "../../components/AppLayout";
import { DataPage } from "./Page";

const root = document.getElementById("root");
if (!root) throw new Error("missing React root");

createRoot(root).render(<AppLayout><DataPage /></AppLayout>);

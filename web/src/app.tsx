import { AdminRoute } from "./routes/AdminRoute";
import { currentRouteIsSetup } from "./lib/paths";
import { SetupRoute } from "./routes/SetupRoute";

export function App() {
  if (currentRouteIsSetup()) {
    return <SetupRoute />;
  }
  return <AdminRoute />;
}

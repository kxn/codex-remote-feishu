import { AdminRoute } from "./routes/AdminRoute";
import { currentRouteIsSetup, currentRouteIsSetupMock } from "./lib/paths";
import { SetupMockRoute } from "./routes/SetupMockRoute";
import { SetupRoute } from "./routes/SetupRoute";

export function App() {
  if (currentRouteIsSetupMock()) {
    return <SetupMockRoute />;
  }
  if (currentRouteIsSetup()) {
    return <SetupRoute />;
  }
  return <AdminRoute />;
}

import { AppShell } from "./components/layout";
import { useDashboardData } from "./hooks/useDashboardData";

export function App() {
  useDashboardData();
  return <AppShell />;
}

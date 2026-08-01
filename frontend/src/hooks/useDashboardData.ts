import { useEffect } from "react";

import { initDashboardStore, teardownDashboardStore } from "../store/dashboard-store";

export function useDashboardData() {
  useEffect(() => {
    initDashboardStore();
    return () => {
      teardownDashboardStore();
    };
  }, []);
}

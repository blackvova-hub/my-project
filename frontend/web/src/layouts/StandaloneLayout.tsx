import { Outlet } from "react-router-dom";
import Header from "../shared/ui/Header";

/** Shared branding on authentication and administration pages. */
export function StandaloneLayout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Header />
      <div className="standalone-content">
        <Outlet />
      </div>
    </div>
  );
}

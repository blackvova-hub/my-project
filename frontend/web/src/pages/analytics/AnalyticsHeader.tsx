import Header from "../../shared/ui/Header";

/** Reuse the site's navigation so every existing section stays accessible. */
export function AnalyticsHeader({ onProfile }: { onProfile: () => void }) {
  return <Header onProfile={onProfile} />;
}

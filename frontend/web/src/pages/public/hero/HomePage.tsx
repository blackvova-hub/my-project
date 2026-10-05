import { HeroSection } from "./sections/HeroSection.tsx"
import { AboutSection } from "./sections/AboutSection.tsx"
import { BenefitsSection } from "./sections/BenefitsSection.tsx"
import { TeamSection } from "./sections/TeamSection.tsx"
import { BetaSection } from "./sections/BetaSection.tsx"
import { SubscriptionsSection } from "./sections/SubscriptionsSection.tsx"

export default function HomePage() {
  return (
    <div className="text-foreground">
      <HeroSection />
      <AboutSection />
      <BenefitsSection />
      <TeamSection />
      <BetaSection />
      <SubscriptionsSection />
    </div>
  )
}

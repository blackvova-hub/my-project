import { useEffect } from "react";
import { useLocation } from "react-router-dom";

const TARGET_SELECTOR = "[data-reveal], [data-reveal-scope] > *";

export function ScrollReveal() {
  const { pathname } = useLocation();

  useEffect(() => {
    const preference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const targets = new Set<HTMLElement>();
    let frame = 0;
    const observer = new IntersectionObserver((entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("is-visible");
        observer.unobserve(entry.target);
      }
    }, { rootMargin: "0px 0px -32px 0px", threshold: 0 });

    const collect = () => {
      frame = 0;
      document.querySelectorAll<HTMLElement>(TARGET_SELECTOR).forEach((target) => {
        // Reveal individual blocks, never a parent and its children together.
        if (targets.has(target) || target.closest("[data-reveal-skip]") ||
          target.querySelector("[data-reveal], [data-reveal-scope]")) return;
        targets.add(target);
        const siblings = target.parentElement?.querySelectorAll(
          target.parentElement.hasAttribute("data-reveal-scope") ? ":scope > *" : ":scope > [data-reveal]",
        );
        const index = siblings ? Array.from(siblings).indexOf(target) : 0;
        target.style.setProperty("--reveal-delay", `${Math.max(0, index % 4) * 65}ms`);
        target.classList.add("reveal-on-scroll");
        observer.observe(target);
        if (preference.matches) target.classList.add("is-visible");
      });
    };

    collect();
    // Lazy routes and asynchronously loaded widgets can mount after this effect.
    const mutations = new MutationObserver((records) => {
      const hasTargets = records.some((record) => Array.from(record.addedNodes).some((node) =>
        node instanceof HTMLElement && (node.matches(TARGET_SELECTOR) || node.querySelector(TARGET_SELECTOR)),
      ));
      if (hasTargets && !frame) frame = window.requestAnimationFrame(collect);
    });
    mutations.observe(document.body, { childList: true, subtree: true });

    const showAll = () => {
      if (!preference.matches) return;
      observer.disconnect();
      targets.forEach((target) => target.classList.add("is-visible"));
    };
    preference.addEventListener("change", showAll);

    return () => {
      window.cancelAnimationFrame(frame);
      observer.disconnect();
      mutations.disconnect();
      preference.removeEventListener("change", showAll);
      targets.forEach((target) => {
        target.classList.remove("reveal-on-scroll", "is-visible");
        target.style.removeProperty("--reveal-delay");
      });
    };
  }, [pathname]);

  return null;
}

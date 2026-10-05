# Frontend work rules

Scope: all files under `frontend/`. Follow the user's current task scope before making visual changes.

## Reuse and consistency

- Reuse existing UI components and use shadcn/ui where it fits the existing architecture. Add only the components a task needs.
- Keep one coherent visual language across pages. Do not invent a new visual style for each page.
- Make the interface clean, restrained, compact, and product-focused. Prefer simple composition.
- Build hierarchy through typography, spacing, alignment, grouping, and contrast.
- Use semantic theme tokens for interface colors. Preserve meaningful status colors, chart colors, and image content.
- Light, Dark, and Green must use the same components, layout, dimensions, spacing, and radius. Change color tokens only between themes.

## Visual changes requiring a separate user request

Do not introduce any of the following unless the user specifically asks for it:

- gradients, glow, or glassmorphism;
- huge hero sections or oversized headings;
- excessive border radius or shadows;
- turning every content block into a card;
- decorative badges, pills, or meaningless icons;
- arbitrary colors, font sizes, or spacing values.

## Implementation

- Preserve page structure, copy, behavior, and business logic unless the task explicitly requires changes.
- Inspect the existing component and token patterns before adding a new one.
- For visual work, check the affected page in Light, Dark, and Green, including hover and focus states, and run the frontend build.

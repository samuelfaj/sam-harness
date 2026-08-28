# Design consistency and human finish

Start with the product's existing design system. Locate components, tokens, typography, spacing, layout, motion, and interaction rules before proposing UI.

For every affected flow, verify:

- loading, empty, error, success, unavailable, and destructive states;
- keyboard access, visible focus, contrast, responsive behavior, and reduced motion where relevant;
- localized visible copy and accessible names;
- human names instead of database IDs, UUIDs, enum codes, or internal status values;
- dates, money, permissions, and state in the user's locale and context;
- confirmation for destructive actions and a recovery path when recovery is possible;
- browser or device evidence at the affected widths and interaction states.

If the source of truth is missing, do not invent a parallel visual language. Ask for the design owner or document the unresolved decision. Do not claim completion from source code alone when the acceptance criterion is visual or interactive.

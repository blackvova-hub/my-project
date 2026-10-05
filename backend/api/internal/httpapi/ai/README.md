# AI assistant knowledge system

The assistant uses a compact local knowledge base compiled into the Go binary. It does not send every site document on every request and does not make a second model call to classify the question.

## Request flow

1. The frontend sends the current pathname as `Page: /...` and at most the last 6 chat messages; each message is capped at 1600 characters by the backend.
2. `assistant_behavior.go` blocks only prompt/secret extraction and access-bypass requests locally. All normal product questions go through OpenAI; the file supplies short routing hints, not canned answers.
3. `knowledge.go` validates the pathname and scores knowledge modules by route, typo-tolerant keywords and the latest user message.
4. At most 3 relevant modules are appended to `knowledge/core.md`.
5. The backend adds only server-verified user context: plan, primary exchange, email verification, 2FA and admin-role state.
6. OpenAI Responses API receives the result in `instructions`, while chat turns remain separate `user`/`assistant` input messages. Blocked injection turns are excluded from later model history.

Current response controls are `reasoning.effort: low`, `text.verbosity: low`, `max_output_tokens: 900` and `store: false`. Every model request includes a stable hashed `safety_identifier`; raw user identifiers are not sent in that field.

## Updating site knowledge

- Keep universal behavior and safety rules only in `knowledge/core.md`.
- Put page facts in the matching small file under `knowledge/`.
- Register new routes and search keywords in `knowledgeCatalog`.
- Describe only behavior present in the active frontend route and components. If marketing copy conflicts with the active route, state the active behavior explicitly.
- Add or update a selector test in `knowledge_test.go`.

The Timeweb variables in the repository's root environment belong to the separate news collector. The site AI assistant reads only `OPENAI_*` configuration.

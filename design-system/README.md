# Design tokens

The web application uses the `jj-*` token namespace. The source is [design-tokens.json](tokens/design-tokens.json); [tokens.css](tokens/tokens.css) contains CSS variables.

From the repository root, run `npm run tokens:build` after changing the JSON, then `npm run tokens:check`. The stylesheet guard rejects direct color literals outside the token layer.

The original project adapted its token structure from an internal Zhiliao design specification. That archive and its planning documents are excluded from the public edition. This directory describes the implemented web tokens, not verified React Native or Flutter components.

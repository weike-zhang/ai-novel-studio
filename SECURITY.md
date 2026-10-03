# Security reports

Do not post credentials, account recovery codes, private stories or exploit details in a public issue.

Use GitHub's private vulnerability reporting when it is enabled for this repository. If that option is unavailable, open an issue asking the maintainer for a private contact channel without including sensitive details.

Development mode includes synthetic identities and verification helpers. It is not suitable for an internet-facing deployment. Use independent authentication keys and your own infrastructure configuration, and review the [deployment boundary](docs/development.md) before exposing a service.

If a credential was committed, rotate it. Removing its latest occurrence does not revoke the value or remove earlier copies.

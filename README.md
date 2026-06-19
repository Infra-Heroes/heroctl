# heroctl

The command-line interface for the InfraHeroes platform.

## Usage
Standard build:
```bash
make build
./heroctl --help
```

## Development
Use the `Makefile` for tasks:
- `make build`: Build the binary
- `make test`: Run tests
- `make fmt`: Format code
- `make tidy`: Tidy dependencies

## Authentication

heroctl uses short-lived tokens from Zitadel for regular user login (via `heroctl login`).
For CI/CD and automation, use a Personal Access Token (PAT).
You can create a PAT via `heroctl tokens create --scope deploy`.

**Why custom PATs instead of Zitadel tokens?**
We use our own PAT system in the API server instead of Zitadel's built-in PATs because:
1. We need strict, custom scopes (like `deploy` only) which are easier to manage in-house.
2. It decouples CLI automation from complex OIDC flows.
3. We can track "last used" timestamps and instantly revoke tokens directly in our database.

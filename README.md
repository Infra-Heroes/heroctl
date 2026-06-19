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

## Authentication & CI/CD

`heroctl` supports two authentication methods: interactive browser-based login and token-based login.

### 1. Interactive Login
To log in interactively on your local machine:
```bash
./heroctl login
```
This starts the OAuth2 Device Authorization flow. It will print a URL and a user code. Open the URL in your browser, log in, and authorize `heroctl`. Once authorized, `heroctl` saves the token configuration locally at `~/.config/heroctl/token.json`.

### 2. Token Authentication (CI/CD / Headless)
For CI/CD pipelines (e.g., Woodpecker, GitHub Actions) or headless servers, you can bypass the interactive browser flow by providing a token directly.

#### How to get the token:
1. Log in interactively on your local machine using `./heroctl login`.
2. Open the local token config file:
   ```bash
   cat ~/.config/heroctl/token.json
   ```
3. Extract the `access_token` string. This is your personal access token (JWT issued by Zitadel).

#### Using the token:
You can pass the token to any `heroctl` command using the global `--token` flag:
```bash
./heroctl deploy --project webpage --token <YOUR_ACCESS_TOKEN>
```
Alternatively, you can log in non-interactively on a machine using the token:
```bash
./heroctl login --token <YOUR_ACCESS_TOKEN>
```
This saves the token to the local config path so subsequent commands run authenticated without needing the `--token` flag.


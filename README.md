<div align="center">

# 🚀 heroctl

**The official command-line interface for the Infra-Heroes platform.**


[![status-badge](https://woodpecker.infra-heroes.de/api/badges/InfraHeroes/heroctl/status.svg)](https://woodpecker.infra-heroes.de/repos/InfraHeroes/heroctl)

Manage your MicroVM deployments, volumes, secrets, and configurations effortlessly directly from your terminal.

[Explore Infra-Heroes](https://www.infra-heroes.de) • [Documentation](https://www.infra-heroes.de/docs) • [Showcase Examples](https://github.com/Infra-Heroes/hero-showcase)

</div>

---

## 📑 Table of Contents

- [Installation](#-installation)
- [Getting Started](#-getting-started)
- [Platform Features](#-platform-features)
- [Developer Guide](#-developer-guide)

---

## 📦 Installation

Get started with `heroctl` in seconds using Homebrew:

```bash
curl -sL https://infra-heroes.de/install.sh | bash
```

*(For other platforms, you can download the binaries directly from the [GitHub Releases](https://github.com/Infra-Heroes/heroctl/releases) page).*

---

## 🚀 Getting Started

Deploying your application to the Infra-Heroes cloud is effortless.

1. **Login to your account**:
   ```bash
   heroctl login
   ```

2. **Validate your configuration**:
   Ensure your `hero.toml` syntax is correct:
   ```bash
   heroctl validate
   ```

3. **Deploy your project**:
   Navigate to your project directory containing your `hero.toml` and run:
   ```bash
   heroctl deploy --project my-project
   ```

---

## 📖 Platform Features

### 1. Scaling Up/Down

Infra-Heroes supports both **horizontal scaling** (number of VM replicas) and **vertical scaling** (vCPU and Memory limits).

**Horizontal Scaling** (Defined in `hero.toml`)
To specify how many isolated MicroVM instances of your app should run, use the `min_replicas` and `max_replicas` parameters:

```toml
[deploy]
min_replicas = 2
max_replicas = 5
```

**Vertical Scaling** (Executed via CLI)
To adjust CPU cores and Memory limits in-place for an active deployment, run:
```bash
heroctl deployments update my-app --cpu 2 --memory 1024 --project my-project
```

### 2. Getting Logs

Stream standard output and error logs directly from your isolated MicroVM running instance:

```bash
# Retrieve current logs snapshot
heroctl logs my-app --project my-project

# Stream live log outputs (Follow)
heroctl logs my-app --project my-project -f
```

### 3. Custom Domain (CNAME Setup)

Run workloads under your own domain name by completing these two steps:

1. **Configure your Domain DNS Registry:** Add a CNAME record pointing your domain or subdomain to the edge load balancer:
   ```txt
   Type: CNAME
   Name: app (or @)
   Target: ingress.infra-heroes.de
   ```

2. **Map the Domain in `hero.toml`:** Add the hostname list inside the `[app]` block:
   ```toml
   [app]
   name = "my-app"
   custom_domains = ["app.example.com"]
   ```

#### How it works under the hood

When you deploy with `custom_domains`, hero-api appends the domain to the Traefik router rule registered with Consul:

```
Host(`my-app.heroapp.run`) || Host(`www.my-app.heroapp.run`) || Host(`app.example.com`)
```

**TLS certificates** are handled by the platform automatically:

- All `*.heroapp.run` subdomains are covered by a wildcard certificate issued by Let's Encrypt via Certbot (DNS-01 challenge against the Hetzner zone). Renewed automatically every 90 days.
- On each renewal, the certificate is written to `/etc/traefik/certs/wildcard.{crt,key}` on the gateway node and backed up to Consul KV at `traefik/certs/{cert,key}` for recovery across gateway restarts.
- Traefik loads the certificate from the local file and serves it for all `*.heroapp.run` traffic.

> **Note:** Custom domains outside `*.heroapp.run` (e.g. `app.example.com`) require a valid TLS certificate that covers that domain. The platform currently does not auto-issue per-custom-domain certificates — ensure your DNS CNAME is in place before deploying so the routing resolves correctly. Per-custom-domain certificate issuance is on the roadmap.

#### DNS propagation

The CNAME must resolve before you deploy. Traefik adds the `Host()` rule immediately on deploy but there is no retry — if DNS does not resolve to the platform IP at that point, HTTP health checks targeting the custom domain will fail and the deployment will be marked unhealthy.

### 4. Volumes (Ceph Block Storage)

Attach high-availability persistent storage volumes to your MicroVM.

1. **Create the Volume:** Create a storage block device inside the project:
   ```bash
   heroctl volumes create my-volume-name --size 10 --project my-project
   ```

2. **Mount the Volume:** Map the volume to a mount path inside the guest OS using `hero.toml`:
   ```toml
   [[volumes]]
   name = "my-volume-name"
   mount = "/var/lib/db-data"
   ```

### 5. Secrets

Sensitive configurations (passwords, private keys, database connections) are encrypted at rest using HashiCorp Vault.

```bash
# Set a secret (value is read securely from stdin to avoid leakages in bash history)
heroctl secrets set DB_PASSWORD --project my-project

# List secret keys (values are never exposed)
heroctl secrets list --project my-project

# Delete a secret
heroctl secrets delete DB_PASSWORD --project my-project
```
Secrets are automatically injected as environment variables inside your MicroVM on boot using the `secret:KEY` syntax in your `hero.toml`.

### 6. Environment Variables

Define non-sensitive public environment variables directly in your `hero.toml` configurations:

```toml
[env]
NODE_ENV = "production"
LOG_LEVEL = "info"
API_ENDPOINT = "https://api.external.com"
```
Env variables are loaded and made available to your application process on start.

### 7. Database Access

Access managed project databases over the isolated virtual network (VXLAN overlay) namespace.

By configuring your application connection string, you bypass public ports entirely. Connect internally via:
```txt
postgres://username:password@db.my-project.internal:5432/database_name
```
Internal DNS resolvable names (`.internal`) are bound to private VXLAN IPs, ensuring secure tenant isolation.

---

## 💻 Developer Guide

This section is for contributors and developers working on the `heroctl` CLI codebase.

### Building from Source

To build the standard binary locally:
```bash
make build
./heroctl --help
```

### Development Tasks

We use a `Makefile` for all common development tasks:
- `make build`: Build the binary into the root directory.
- `make test`: Run unit and integration tests.
- `make fmt`: Format Go code.
- `make tidy`: Tidy go module dependencies.
- `make lint`: Run `golangci-lint` (installs automatically if missing).
- `make security`: Run `gosec` and `govulncheck` to scan for vulnerabilities.

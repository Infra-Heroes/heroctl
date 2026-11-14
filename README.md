<div align="center">

# 🚀 heroctl

**The official command-line interface for the Infra-Heroes platform.**


[![status-badge](https://ci.infra-heroes.de/api/badges/InfraHeroes/heroctl/status.svg)](https://ci.infra-heroes.de/repos/InfraHeroes/heroctl)

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

Install the latest release with the install script:

```bash
curl -sL https://infra-heroes.de/install.sh | bash
```

*(For other platforms, or to pick a version, download the binary directly from the [Releases](https://codeberg.org/InfraHeroes/heroctl/releases) page on Codeberg).*

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

**Autoscaling.** With `max_replicas` above `min_replicas` the platform watches the average CPU of the replicas every 30 seconds: above 80 % it adds one, below 20 % it removes one, always within your minimum and maximum. A new deploy, resize or restart starts with the count the autoscaler has reached, not with the minimum.

**Vertical Scaling** (Executed via CLI)
To adjust CPU cores and Memory limits of an active deployment, run:
```bash
heroctl deployments update my-app --cpu 2 --memory 1024 --project my-project
```
A deployment with several replicas is updated one replica after another, without downtime.

### 1a. Scale to zero

An app that is not used does not need to run. Set `scale_to_zero = true` and the platform stops it after 5 minutes without requests; the next request starts it again in about two seconds and is answered normally.

```toml
[deploy]
scale_to_zero = true
```

Scale to zero needs a single replica and no volume. Only such apps are woken by a request: an app you stopped yourself (`heroctl deployments stop`) stays stopped until you start it.

### 1b. Scheduled stop and start

Stop an app at night and on weekends, start it again in the morning:

```bash
heroctl deployments schedule my-app --project my-project \
  --stop "0 20 * * 1-5" --start "0 7 * * 1-5" --timezone Europe/Berlin

heroctl deployments schedule my-app --project my-project            # show it, with the next times
heroctl deployments schedule my-app --project my-project --off      # pause, keep the times
heroctl deployments schedule my-app --project my-project --on       # resume
heroctl deployments schedule my-app --project my-project --remove   # delete it
```

Times are cron expressions (minute hour day-of-month month day-of-week). Changing one side keeps the other.

### 1c. Going back to an earlier version (rollback)

Every deploy, resize, environment change and rollback leaves a revision behind. List them and put one back:

```bash
heroctl deployments revisions my-app --project my-project            # newest first, * = the image running now
heroctl deployments rollback my-app --project my-project             # undo the last change (asks first)
heroctl deployments rollback my-app 8f3c1a2b --project my-project    # go back to a given revision
```

A revision is named by the last characters of its id, as the list shows them (at least four, as long as they are unique). The rollback redeploys the earlier image with the CPU, memory, environment and replica count it had, and is a revision of its own, so it can be undone the same way. A deployment with a volume is stopped and started again, which takes it offline for a short while. `--force` skips the question.

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

#### Optional: prove you control the domain (TXT record)

By default a custom domain is routed without a proof of ownership, as before. To
have the platform check that your organisation controls it, set the option in
`hero.toml`:

```toml
[app]
name = "my-app"
custom_domains = ["shop.example.com"]
verify_domains = true
```

A deploy then needs a TXT record for every custom domain:

```bash
heroctl domains challenge shop.example.com --project my-project   # shows the record
# add it at your DNS provider:  _hero-challenge.shop.example.com  TXT  hero-verification=<token>
heroctl domains verify shop.example.com --project my-project      # checks it now
heroctl deploy --project my-project
```

If a record is missing the deploy stops and lists the records still to add. Apex and
`www` are separate domains with their own record. Once verified, a domain stays
verified for your organisation. Needs a hero-api with domain verification; without
`verify_domains` nothing changes.

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

3. **Recovery points (snapshots):** Take, list, remove and restore point-in-time copies, or let the platform take one every day:
   ```bash
   heroctl volumes snapshots create my-volume-name --project my-project      # a manual one; the deployment must be stopped
   heroctl volumes snapshots list my-volume-name --project my-project
   heroctl volumes snapshots restore my-volume-name <snapshot> --project my-project   # deployment must be stopped
   heroctl volumes snapshots delete my-volume-name <snapshot> --project my-project

   # Automatic: every day at 03:00 UTC, keep the newest 10 (1-14)
   heroctl volumes snapshots schedule my-volume-name --project my-project --at 3 --keep 10
   heroctl volumes snapshots schedule my-volume-name --project my-project          # show it
   heroctl volumes snapshots schedule my-volume-name --project my-project --off
   ```
   A running app holds its volume exclusively, so a snapshot can only be taken, and restored, while the deployment is **stopped** (`heroctl deployments stop`); the daily schedule skips a volume that is in use and says so in `heroctl volumes snapshots schedule`. Snapshots are crash-consistent. Restoring discards everything written since. Snapshots you take without a name are called `manual-<UTC time>`; the schedule only ever removes automatic ones (`auto-<UTC time>`), yours stay until you remove them. For a database that must not stop, back it up from inside the app too.

### 5. Secrets

Sensitive configurations (passwords, private keys, database connections) are encrypted at rest using HashiCorp Vault.

```bash
# Set a secret (the value is asked for at a hidden prompt, so it never lands in your shell history)
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
postgres://username:password@db:5432/database_name
```
Internal service names resolve via the platform DNS facade (`<app>.svc.platform`, with `search svc.platform` preconfigured in the VM — so the short app name works). Names are bound to private VXLAN IPs, ensuring secure tenant isolation.

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

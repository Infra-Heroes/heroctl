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

*(For other platforms, or to pick a version, download the binary directly from [infra-heroes.de/downloads/heroctl](https://infra-heroes.de/downloads/heroctl/)).*

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

   No Docker on this machine, or the image comes from CI or a public registry? Skip the build and deploy the image as it is. `hero.toml` still decides size, port, environment and volumes:
   ```bash
   heroctl deploy --project my-project --image docker.io/library/nginx:1.31
   ```
   The image must be one the platform can pull without credentials: a public image, or one in the hero registry.

   **From CI** (GitHub Actions): create a deploy token with `heroctl tokens create --name ci --scope deploy`, store it as the secret `HERO_TOKEN`, then:
   ```yaml
   - uses: actions/checkout@v4
   - uses: infra-heroes/deploy-action@v1
     with:
       token: ${{ secrets.HERO_TOKEN }}
       project: my-project
   ```
   The action ([infra-heroes/deploy-action](https://github.com/Infra-Heroes/deploy-action)) builds the image from your Dockerfile and deploys it; `image:` deploys an existing one instead. On other CI systems install heroctl with `curl -fsSL https://infra-heroes.de/install.sh | bash` and run `heroctl deploy --project my-project --yes --token "$HERO_TOKEN"`.
   `heroctl logs`, `deployments rollback` and the other commands take `--token` as well.
   Keys in `hero.toml` that heroctl does not know (for example `port` under `[app]` instead of `[deploy]`) are reported as warnings.

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

**Tuning the autoscaler per app.** The thresholds above are the platform's. An app sets its own, and can also scale on memory, with labels in `hero.toml`:

```toml
[deploy]
min_replicas = 1
max_replicas = 5

[labels]
"hero-scale-cpu-up"    = "70"   # add a replica above this average CPU (percent)
"hero-scale-cpu-down"  = "30"   # remove one below this
"hero-scale-memory-up" = "80"   # also add one above this share of the VM's memory (percent)
```

- All three are optional and take a number of percent: `cpu-up` and `memory-up` from 1 to 100, `cpu-down` from 0 to 100, and `cpu-down` must be below `cpu-up`. A value that is not valid is refused when you deploy.
- Memory is only watched when `hero-scale-memory-up` is set. It is the memory the app holds in its VM, not what the system keeps as cache, so a file cache that fills up does not count as pressure.
- A replica is removed only when CPU is below `cpu-down` **and**, if memory is watched, memory is below half of its line, so that removing one does not push the rest straight back over it. If memory is watched but cannot be read at that moment, nothing is removed.
- One replica is added or removed per check, every 30 seconds, always within `min_replicas` and `max_replicas`.

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

### 1d. Watching a deployment: metrics, events, alerts, audit

```bash
heroctl deployments metrics my-app --project my-project --range 24h   # CPU, memory, network: now, average, max, course
heroctl deployments events my-app --project my-project                # starts, stops, restarts, resizes, deploys
heroctl audit                                                          # who did what in your organisation
```

Ranges are `1h`, `6h`, `24h`, `7d` and `30d`. The audit log leaves out the per-request entries (`api.request`) that say nothing about what changed; `--all` shows them too.

**Alerts** notify you when a deployment stays under strain or has no healthy instance:

```bash
heroctl deployments alerts set my-app --project my-project --metric cpu_percent --threshold 80 --for 300
heroctl deployments alerts set my-app --project my-project --metric memory_bytes --threshold 400   # megabytes
heroctl deployments alerts set my-app --project my-project --metric unavailable                    # no healthy instance
heroctl deployments alerts list my-app --project my-project
heroctl deployments alerts delete my-app <id> --project my-project   # the id from the list, or its last characters
```

### 2. Getting Logs

Stream standard output and error logs directly from your isolated MicroVM running instance:

```bash
# Retrieve current logs snapshot
heroctl logs my-app --project my-project

# Stream live log outputs (Follow)
heroctl logs my-app --project my-project -f

# Only the last lines, or only lines that match a regular expression
heroctl logs my-app --project my-project --tail 50
heroctl logs my-app --project my-project --grep "error|panic"
heroctl logs my-app --project my-project -f --grep timeout

# History: survives restarts, crashes and deletes (kept 30 days)
heroctl logs my-app --project my-project --since 2h
heroctl logs my-app --project my-project --since 2d --until 1d --grep "panic"
```

Without `--since`/`--until` you get the running instance's output. With either you get the history of all runs, oldest line first, at most 5000 lines (`--tail N` keeps the newest N). Times are RFC 3339 timestamps or a time ago (`90m`, `6h`, `2d`). `--follow` only works on the live output.

### Deploy from GitHub with one command

```bash
cd my-app                                   # the directory with hero.toml
heroctl ci github --project my-project --set-secret
git add .github && git commit -m "Deploy to Infra-Heroes" && git push
```

`heroctl ci github` writes `.github/workflows/deploy.yml` (deploys on every push to `main`, or `--branch`), and with `--preview` also `.github/workflows/preview.yml` (a preview for every pull request, removed when it closes). `--image` deploys an existing image instead of building one. With `--set-secret` it creates a deploy token and stores it as the repository secret `HERO_TOKEN` through the GitHub CLI (`gh`); the token is never printed or written to a file. Without it, the command only writes the files and prints the two commands for the secret. Existing workflow files are not overwritten without `--force`.

### Preview deployments

Test a pull request on its own address before it is merged. A preview is your app deployed under another name, `<app>-<preview>`, next to the real one:

```bash
heroctl deploy --project my-project --preview pr-12     # deploys my-app-pr-12
heroctl preview delete pr-12 --project my-project       # removes it and the databases it created
```

A preview never touches the real app: it gets no custom domains and no volumes (a volume belongs to one deployment), its `[[databases]]` are its own, empty ones, and it scales to zero so it only runs while somebody opens it. `preview delete` is safe to run twice. It removes the preview app first; a deploy-scoped token (what CI uses) may delete the preview app but not its databases, so with `[[databases]]` it deletes the app, says which database is left and exits non-zero until a token with scope `all` runs `heroctl db destroy`. The GitHub Action does both for you with `preview: true`, see [deploy-action](https://github.com/Infra-Heroes/deploy-action).

### Workers (background processes)

A queue consumer, a bot or a long-running job has nothing to answer on a port. Mark it as a worker:

```toml
[app]
name = "queue-worker"

[deploy]
worker = true
memory_mb = 256

[env]
QUEUE_URL = "secret:QUEUE_URL"
```

```bash
heroctl deploy --project my-project --yes
heroctl logs queue-worker --project my-project --follow
```

A worker has no address, no health check and no `port` or `health_path`; it counts as started once it has been running for a few seconds. It is never public and cannot `scale_to_zero` (nothing would ever start it again), and `heroctl validate` refuses `custom_domains` and the health settings on it. If the process exits or crashes, the platform restarts it.

### Scheduled jobs

Run a command of your app on a schedule: a nightly cleanup, a report every 15 minutes. A job is your app's image started with another command; it costs nothing between runs and is billed for the time a run takes.

```toml
[[jobs]]
name = "cleanup"
schedule = "0 3 * * *"                  # minute hour day month weekday, in UTC
command = "python cleanup.py"
timezone = "Europe/Berlin"              # optional, default UTC
timeout = "30m"                         # optional, default 1h, at most 24h
memory_mb = 1024                        # optional, default: the app's
```

```bash
heroctl deploy --project my-project               # creates the jobs next to the app
heroctl jobs list --project my-project
heroctl jobs run shop-cleanup --project my-project    # a run now, outside the schedule
heroctl jobs runs shop-cleanup --project my-project   # newest runs: start, duration, exit code
heroctl logs shop-cleanup --project my-project --since 2h
```

Each job is a deployment of its own, named `<app>-<job>`. The command goes through `sh -c`, so `cd x && ./run | tee log` works; the job gets the app's environment and secrets. Runs never overlap (a tick is skipped while the last run is still going), a failed run is not retried, and a run that exceeds its `timeout` is stopped. A job removed from `hero.toml` is removed by the next deploy. A preview (`--preview`) does not run the app's jobs.

### 3. Bring your own domain

Run an app under your own domain in three steps. You never enter an IP address.

1. **CNAME at your DNS provider.** Use a subdomain such as `www.example.org`;
   a bare `example.org` (apex) cannot be a CNAME, and an A record would pin the
   load balancer's IP, which can change.

   ```txt
   www.example.org   CNAME   ingress.infra-heroes.de.
   ```

   Mind the dot at the end. Without it many providers (Hetzner among them)
   append the zone, and the record becomes
   `ingress.infra-heroes.de.example.org`. `heroctl domains verify` spots that.

2. **List it in `hero.toml`**, and ask for the ownership proof:

   ```toml
   [app]
   name = "my-app"
   custom_domains = ["www.example.org"]
   verify_domains = true
   ```

3. **TXT record for the proof.** With `verify_domains = true` a deploy waits
   until every domain carries its record:

   ```bash
   heroctl domains challenge www.example.org --project my-project   # shows CNAME and TXT
   # _hero-challenge.www.example.org   TXT   hero-verification=<token>
   heroctl domains verify www.example.org --project my-project      # checks TXT and CNAME now
   heroctl deploy --project my-project
   ```

   If a record is missing, the deploy stops and lists the records still to add.
   Once verified, a domain stays verified for your organisation. Without
   `verify_domains` a domain is routed without proof, as before.

The same works in hero-platform: in the order form under *Advanced options*,
*Custom domains* shows the CNAME and the TXT record for each domain and checks
them before the deploy.

#### Certificates

- Every custom domain gets its own Let's Encrypt certificate, issued and renewed
  by the platform's Traefik. Let's Encrypt validates over port 443 (TLS-ALPN-01),
  so the CNAME is all it needs; port 80 plays no part.
- The TXT record only proves ownership to the platform; Let's Encrypt does not
  read it.
- The certificate is requested once the domain is deployed. The first request
  can take a few seconds or fail once while it is issued; a retry then succeeds.
- `*.heroapp.run` is covered by a wildcard certificate (DNS-01 against the
  Hetzner zone).
- Wildcards such as `*.example.com` are not supported; list each hostname.

#### Under the hood

hero-api appends the domain to the app's Traefik router rule in Consul:

```
Host(`my-app.heroapp.run`) || Host(`www.my-app.heroapp.run`) || Host(`www.example.org`)
```

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

3. **Grow it:** A volume can be made larger, never smaller:
   ```bash
   heroctl volumes resize my-volume-name --size 20 --project my-project
   ```

4. **Recovery points (snapshots):** Take, list, remove and restore point-in-time copies, or let the platform take one every day:
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

To change them on a running deployment without a new build, which redeploys it with the new environment:

```bash
heroctl deployments env list my-app --project my-project
heroctl deployments env set my-app LOG_LEVEL=debug --project my-project
heroctl deployments env set my-app DB_PASSWORD=secret:DB_PASSWORD --project my-project   # a project secret, never the value
heroctl deployments env unset my-app LOG_LEVEL --project my-project
```

Values are shown as they were set, so a secret appears as its reference.

### 7. Databases

A microVM runs one image, so a database is not a sidecar inside your app's VM. It is a private deployment of its own in the same project, with a volume and a generated password, and your app gets the connection URL as an environment variable. Declare it next to the app in `hero.toml`:

```toml
[[databases]]
name    = "db"          # becomes the service name <app>-db
engine  = "postgres"    # postgres, mariadb (MySQL compatible) or redis
version = "17"          # optional; postgres 17, mariadb 11.4, redis 8 by default
size_gb = 5             # optional; size of the data volume

[[databases]]
name   = "cache"
engine = "redis"
```

On the next `heroctl deploy` the database is created, started and waited for, and your app receives `DATABASE_URL` (`REDIS_URL` for redis; `env = "NAME"` picks another name, which you need when two databases would share one). A database that already exists is left alone, so deploying the app never restarts it. A stopped one is started again.

```bash
heroctl db list --project my-project                 # the databases of a project
heroctl db destroy my-app-db --project my-project    # deletes it with its data, volume and secrets
```

Good to know:

- The password is generated and kept as a project secret; you never handle it. Creating the secrets needs a token with scope `all`. A deploy-scope token (CI) can use databases that exist, not create them: create them once from your machine.
- Reach the database only from apps of the same project, by its short name (`my-app-db`). It has no public address.
- `mysql` is accepted as `engine` and means MariaDB: the official MySQL image does not start in a microVM.
- Redis has no password; it is reachable only inside the project's isolated network. Treat it as a cache: it saves snapshots to its volume (after 60 s with 10000 changes, 5 min with 100, an hour with 1), so a restart can lose the changes since the last one. If you need durable data, use postgres or mariadb.
- Backups: a running volume cannot be snapshotted, so dump the data from your app (`pg_dump`, `mysqldump`) and store it outside.

The platform's internal DNS (`<app>.svc.platform`, with `search svc.platform` set in every VM) resolves service names to private VXLAN IPs, which keeps tenants apart.

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

### Release housekeeping

Release binaries and container images are published to Forgejo. The website copies the four newest releases to https://infra-heroes.de/downloads/heroctl/<tag>/ at its deploy, which is where install.sh and the GitHub deploy action download from (Codeberg no longer resolves).

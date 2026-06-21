# heroctl 🚀

The command-line interface for the InfraHeroes (NanoStack) platform.

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
- `make lint`: Run golangci-lint (installs automatically if missing)
- `make security`: Run gosec and govulncheck

---

## Features & Documentation Examples

### 1. Scaling Up/Down

InfraHeroes supports both **horizontal scaling** (number of VM replicas) and **vertical scaling** (vCPU and Memory limits).

#### Horizontal Scaling (Defined in `hero.toml`)
To specify how many isolated MicroVM instances of your app should run, use the `min_replicas` and `max_replicas` parameters in `hero.toml`:

```toml
[deploy]
min_replicas = 2
max_replicas = 5
```
Apply the configuration by redeploying:
```bash
heroctl deploy --project my-project
```

#### Vertical Scaling (Executed via CLI)
To adjust CPU cores and Memory limits in-place for an active deployment, run:
```bash
heroctl deployments update my-app --cpu 2 --memory 1024 --project my-project
```

---

### 2. Getting Logs

Stream standard output and error logs from your isolated MicroVM running instance:

```bash
# Retrieve current logs snapshot
heroctl logs my-app --project my-project

# Stream live log outputs (Follow)
heroctl logs my-app --project my-project -f
```

---

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
   Submit the update:
   ```bash
   heroctl deploy --project my-project
   ```

---

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
   Redeploy the application:
   ```bash
   heroctl deploy --project my-project
   ```

---

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
Secrets are automatically injected as environment variables inside your MicroVM on boot.

---

### 6. Interactive Shell (SSH Access)

Establish a secure, interactive shell session directly into the active MicroVM allocation instance. This is supported in both direct deployment environments and via the self-service App Store.

#### Direct Environment Access (API Host)
```bash
# Connect to an app's shell in a project
heroctl ssh my-app --project my-project

# Connect and run a specific shell command
heroctl ssh my-app --project my-project --cmd "ls -la /var/log"
```

#### App Store Instance Access (WebSocket Tunnel)
```bash
# Connect to a store instance shell using the WebSocket tunnel proxy
heroctl ssh <instance_id> --store

# Specify a custom store API backend URL
heroctl ssh <instance_id> --store --store-url https://store.example.com
```

---

### 7. Environment Variables

Define non-sensitive public environment variables directly in the `hero.toml` configurations:

```toml
[env]
NODE_ENV = "production"
LOG_LEVEL = "info"
API_ENDPOINT = "https://api.external.com"
```
Env variables are loaded and made available to your application process on start.

---

### 8. Database Access

Access managed project databases over the isolated virtual network (VXLAN overlay) namespace.

By configuring your application connection string, you bypass public ports entirely. Connect internally via:
```txt
postgres://username:password@db.my-project.internal:5432/database_name
```
Internal DNS resolvable names (`.internal`) are bound to private VXLAN IPs, ensuring tenant isolation.


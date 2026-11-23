# Changelog

Merged pull requests, grouped by week and kind. Written once a week by the changelog job
(repository ci-images); edit by hand only to add what a title does not say.

## 2026-W47 (2026-11-16 to 2026-11-22)

### Breaking changes

- Heroctl init creates a new app repository ([#71](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/71))
- Members invite says whether the invitation was mailed ([#73](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/73))
- Teams, Projekt-Tokens, Token-Ablauf und Besitzerwechsel ([#79](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/79))

### Features

- Logs --since/--until read the log history ([#59](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/59))
- Deploy --preview and preview delete ([#61](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/61))
- [deploy] worker = true ([#62](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/62))
- [[jobs]] and heroctl jobs (list, run, runs) ([#63](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/63))
- Heroctl ci github ([#68](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/68))
- Workspaces and preview --ttl ([#75](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/75))
- Staged deploys (deploy --stage, promote, discard) ([#76](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/76))
- Heroctl notifications und --channel ([#78](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/78))
- Heroctl exec and console ([#80](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/80))
- Heroctl sandbox commands ([#82](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/82))

### Fixes

- Check the error of the history hints (errcheck) ([#60](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/60))
- Check the errors of the jobs messages (errcheck) ([#64](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/64))
- Preview delete works with a deploy token ([#67](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/67))
- Secrets set reads the value from a pipe ([#70](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/70))

### CI and build

- Prune old Codeberg releases and container tags before each release ([#66](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/66))
- Build Windows for x86-64 only ([#65](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/65))
- Lint, gosec and govulncheck from the shared ci-go image ([#72](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/72))
- Drop the Codeberg release, image and prune steps ([#77](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/77))

### Dependencies

- Update alpine Docker tag to v3.24 ([#69](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/69))

### Other

- Guide custom domains: CNAME, no apex, TXT proof ([#81](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/81))
- Show the remote's stderr in console sessions ([#83](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/83))

## 2026-W46 (2026-11-09 to 2026-11-15)

### Features

- volumes: Heroctl volumes snapshots ([#44](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/44))
- domains: Verify_domains in hero.toml und heroctl domains challenge/verify ([#45](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/45))
- deployments: Heroctl deployments schedule und README zu Skalierung ([#46](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/46))
- deployments: Heroctl deployments revisions und rollback ([#50](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/50))
- CLI fuer Metriken, Events, Alarme, Env, Audit, Volume-Resize, deploy --image, Log-Filter ([#51](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/51))
- db: Verwaltete Datenbanken per [[databases]] in hero.toml ([#55](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/55))

### Fixes

- volumes: Restore-Entscheidung dem Server ueberlassen ([#48](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/48))
- deploy: Warnung bei ignorierten hero.toml-Schluesseln, klarer Timeout, Actions entfernt, README ([#53](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/53))

### Performance

- deploy: Status jede Sekunde abfragen ([#49](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/49))

### Tests

- Heroctl db, waitHealthy, VersionString ([#57](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/57))
- Auth, client und Deploy mit Datenbanken ([#58](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/58))

### Documentation

- Snapshots brauchen eine gestoppte Deployment ([#47](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/47))
- Autoscale-Labels ([#52](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/52))
- GitHub Action infra-heroes/deploy-action ([#54](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/54))
- Redis ist ein Cache ([#56](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/56))

### Dependencies

- Update all non-major dependencies ([#43](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/43))

## 2026-W45 (2026-11-02 to 2026-11-08)

### Dependencies

- Update module github.com/spf13/pflag to v1.1.0 ([#42](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/42))

## 2026-W43 (2026-10-19 to 2026-10-25)

### Dependencies

- Update dependency securego/gosec to v2.30.0 ([#41](https://git.infra-heroes.de/InfraHeroes/heroctl/pulls/41))

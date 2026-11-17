# Changelog

Merged pull requests, grouped by week and kind. Written once a week by the changelog job
(repository ci-images); edit by hand only to add what a title does not say.

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

# CLAUDE.md — Arbeitsregeln für capulus-operator

Go-Operator (controller-runtime) für das Capulus-Homelab. Sprache im Repo ist **Deutsch** (Docs, Commit-Beschreibungen, PR-Texte). Regeln wie in capulus-core.

## Harte Regeln

- **Keine Kommentare im Code.** Einzige Ausnahmen: `// +kubebuilder:`-Marker und der Versions-Kommentar `# vX.Y.Z` hinter der Commit-SHA in `uses:`-Zeilen der Workflows.
- **Doku liegt in [pkr-lab-docs](https://github.com/pkr-lab/pkr-lab-docs)**, nicht hier. Jede Codeänderung bekommt dort einen PR.
- **Claude committet und pusht nicht.** Commits, Pushes und PRs macht der Nutzer.
- Conventional Commits, Scope = Bereich, Beschreibung auf Deutsch: `feat(backup): …`, `fix(controller): …`.
- Keine Secrets im Repo. Der Operator legt Secrets nie im Klartext in Git ab.

## Aufbau

| Pfad | Inhalt |
|---|---|
| `api/v1alpha1/` | CRD-Typen mit kubebuilder-Markern |
| `internal/backup/` | Aufbau des CronJobs und Status-Berechnung (reine Funktionen, Unit-Tests) |
| `internal/controller/` | Reconciler, Metriken, RBAC-Marker |
| `cmd/manager/` | Einstiegspunkt, Flags |
| `config/crd/bases/`, `config/rbac/` | **generiert** (`make generate`), nie von Hand ändern |
| `config/samples/` | Beispielressourcen |

## Prüfen vor dem Commit

```bash
make generate
gofmt -l api cmd internal
make vet test
```

Go ist lokal oft nicht installiert; dann in `golang:1.26-alpine` per Docker ausführen.

## Fallstricke

- CEL-Regeln in Markern: Leerstrings nicht mit `''` prüfen (controller-gen macht daraus ungültiges YAML), stattdessen `has(self.feld)`.
- Shell-Skripte der Backup-Container bekommen alle Werte als Positionsargumente (`sh -c SKRIPT name arg…`), nie per String-Interpolation, damit nichts gequotet werden muss.
- Statusänderungen per `Status().Patch` mit `MergeFrom`, nicht `Update` (sonst Konfliktfehler bei parallelen CronJob-Events).

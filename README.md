# capulus-operator

Kubernetes-Operator für das Capulus-Homelab. Er löst die kopierten CronJobs, Skripte und manuellen Abläufe in [capulus-core](https://github.com/pkr-lab/capulus-core) durch deklarative Ressourcen ab. Pro Cluster (TECH, PROD, ENTW) läuft eine eigene Instanz.

## Stand

| Bereich | CRD | Controller |
|---|---|---|
| Backups (restic, SQLite, Postgres) | `Backup` | fertig |
| Datenbanken | `Database` | geplant |
| Zertifikate / PKI | `ManagedCertificate` | geplant |
| Token- und Secret-Rotation | `SecretRotation` | geplant |

Die CRDs der geplanten Bereiche sind Entwürfe. Das Schema kann sich noch ändern, solange kein Controller sie verarbeitet.

## Backup

Ein `Backup` erzeugt einen CronJob (Owner-Referenz, wird bei Änderungen am Spec nachgezogen) und spiegelt dessen Zustand in den Status.

Ablauf eines Laufs:

1. Pro SQLite- oder Postgres-Quelle ein InitContainer: `sqlite3 .backup` mit optionalem `integrity_check`, bzw. `pg_dump -F c`, Ergebnis nach `/stage`.
2. Ein restic-Container sichert `/stage` und die PVC-Quellen (nur lesend gemountet), dann `forget --prune` nach `retention` (gruppiert nach Host und Tag, nicht nach Pfad) und `check --read-data-subset`.

Quellentypen:

| `type` | Pflichtfelder | Gesichert |
|---|---|---|
| `Files` | `pvcName` | gesamte PVC unter `/sources/<name>` |
| `SQLite` | `pvcName`, `sqlite.file` | konsistente Kopie in `/stage/<name>/`, PVC ohne die DB-Datei |
| `Postgres` | `postgres.*` | Dump nach `/stage/<name>/<database>.dump` |

Beispiele: [config/samples/](config/samples/). Das Repository ist eine bestehende PVC (`repository.pvcName`), das Passwort kommt aus einem Secret. Der Operator legt weder PVCs noch Secrets an.

Status (`kubectl get backup`): `Pending`, `Running`, `Succeeded`, `Failed`, `Suspended`; dazu die Conditions `Ready` (CronJob abgeglichen) und `Healthy` (letzter Lauf).

Metriken (Port 8080, für VictoriaMetrics):

| Metrik | Bedeutung |
|---|---|
| `capulus_backup_last_success_timestamp_seconds` | Zeitpunkt des letzten erfolgreichen Laufs |
| `capulus_backup_last_schedule_timestamp_seconds` | Zeitpunkt des letzten gestarteten Laufs |
| `capulus_backup_failed` | 1, wenn der letzte Lauf fehlgeschlagen ist |
| `capulus_backup_suspended` | 1, wenn das Backup pausiert ist |

## Entwicklung

```bash
make generate
make vet test
make build
```

`make generate` erzeugt DeepCopy, CRDs (`config/crd/bases/`) und RBAC (`config/rbac/role.yaml`). Die erzeugten Dateien werden eingecheckt, die CI prüft das mit `make check-generated`.

Der Controller-Test nutzt einen Fake-Client. Gegen einen echten Cluster wurde der Backup-Controller mit k3s geprüft (CronJob anlegen, Lauf, Restore, Fehlerfall, Suspend).

## Deployment

Flags des Managers:

| Flag | Standard |
|---|---|
| `--restic-image` | `restic/restic:0.18.1` |
| `--sqlite-image` | `alpine:3.24` |
| `--postgres-image` | `postgres:17-alpine` |
| `--leader-elect` | aus |
| `--metrics-bind-address` | `:8080` |
| `--health-probe-bind-address` | `:8081` |

Das Image heißt `ghcr.io/pkr-lab/capulus-operator:sha-<commit>`. Das Helm-Chart für ArgoCD gehört nach `capulus-core/argocd/apps/<cluster>/capulus-operator/`; CRDs und ClusterRole stammen aus `config/`.

`pg_dump` muss mindestens die Major-Version des Servers haben. Bei abweichender Version `spec.images.postgres` pro Backup setzen.

## Lizenz

MIT, siehe [LICENSE](LICENSE).

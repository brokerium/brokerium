# Brokerium server

Brokerium is a control plane and web dashboard for Apache Kafka® KRaft clusters: one place to see
every cluster, add and remove controllers, rebalance partitions, replace brokers and run rolling
upgrades, with every step planned, confirmed and checked against the health of the cluster.

This image runs `brokerium-server`: the REST API, the dashboard and the operations engine. It
stores its state in PostgreSQL.

- Website: https://brokerium.kaufen
- Documentation: https://brokerium.kaufen/docs/
- Source code and issues: https://github.com/brokerium/brokerium

## Quick start

```bash
docker network create brokerium
docker run -d --name brokerium-db --network brokerium -e POSTGRES_USER=brokerium \
  -e POSTGRES_PASSWORD=brokerium -e POSTGRES_DB=brokerium postgres:18-alpine
docker run -d --name brokerium --network brokerium -p 8080:8080 -p 8443:8443 \
  -v brokerium-data:/var/lib/brokerium -e BROKERIUM_ADMIN_PASSWORD=change-me \
  -e BROKERIUM_DATABASE_URL='postgres://brokerium:brokerium@brokerium-db:5432/brokerium?sslmode=disable' \
  bvaliev/brokerium-server:latest
```

Open http://localhost:8080, sign in as `admin` and add a cluster by its bootstrap servers.
Port 8080 serves the dashboard and the API, port 8443 the gRPC endpoint for agents. The data
directory `/var/lib/brokerium` holds the key that seals stored credentials; keep it on a volume
and back it up together with the database.

For Kubernetes, use the Helm chart in the repository; see the
[installation guide](https://brokerium.kaufen/install/).

## Tags

| Tag | Meaning |
| --- | --- |
| `latest`, `0.2`, `0.2.1` | The latest release, the latest patch of a minor version, an exact release |
| `edge` | The current `main` branch, for testing |
| `sha-<commit>` | The build of one commit |

Images are built for `linux/amd64` and `linux/arm64`, run as the non-root user 65532 and carry
the license in `/licenses`.

## License

Source-available under the PolyForm Shield License 1.0.0: free to use, including in production.
See [licensing](https://brokerium.kaufen/licensing/).

Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of the Apache
Software Foundation in the United States and/or other countries. Brokerium is not affiliated with,
endorsed by, or sponsored by the Apache Software Foundation.

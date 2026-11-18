# Brokerium Enterprise server

Brokerium is a control plane and web dashboard for Apache Kafka® KRaft clusters: one place to see
every cluster, add and remove controllers, rebalance partitions, replace brokers and run rolling
upgrades, with every step planned, confirmed and checked against the health of the cluster.

This image runs `brokerium-server` of Brokerium Enterprise: everything in the Community image
`bvaliev/brokerium-server`, plus the Enterprise features, starting with message tools (deleting
records, emptying topics, searching, exporting and copying messages). The image is public; a
license key unlocks the Enterprise features. Without a key it works like the Community image.

- Website: https://brokerium.kaufen
- Brokerium Enterprise: https://brokerium.kaufen/enterprise/
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
  bvaliev/brokerium-server-enterprise:latest
```

Open http://localhost:8080, sign in as `admin`, open **License** and paste the license key. Port
8080 serves the dashboard and the API, port 8443 the gRPC endpoint for agents. The data directory
`/var/lib/brokerium` holds the key that seals stored credentials; keep it on a volume and back it
up together with the database.

For Kubernetes, use the Helm chart with `edition: enterprise`; see the
[installation guide](https://brokerium.kaufen/install/).

## License key

- Add the key on the **License** page (administrators), or set `BROKERIUM_LICENSE` to the key or
  `BROKERIUM_LICENSE_FILE` to a file that contains it, for example a mounted Secret. The variables
  take precedence over the key stored through the dashboard.
- The key is verified offline against public keys built into the image. The server never reports
  usage and works in air-gapped networks.
- After the expiry date the Enterprise features keep working for the grace period of the key,
  and the dashboard warns. Then they stop; everything in Community keeps working.

## Tags

| Tag | Meaning |
| --- | --- |
| `latest`, `0.3`, `0.3.0` | The latest release, the latest patch of a minor version, an exact release |
| `edge` | A nightly build of the `main` branch, for testing |

The tags match those of `bvaliev/brokerium-server`. Images are built for `linux/amd64` and
`linux/arm64`, run as the non-root user 65532 and carry the licenses in `/licenses`.

## License

The Enterprise features are licensed under the Brokerium Commercial License: production use needs a
subscription and a license key; evaluation and testing do not. Everything else is source-available
under the PolyForm Shield License 1.0.0. See [licensing](https://brokerium.kaufen/licensing/).

Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of the Apache
Software Foundation in the United States and/or other countries. Brokerium is not affiliated with,
endorsed by, or sponsored by the Apache Software Foundation.

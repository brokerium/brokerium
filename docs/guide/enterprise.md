# Editions and license keys

Brokerium comes in two editions, both built from this repository
([ADR 0016](../adr/0016-editions-and-license-keys.md)):

- **Community** is everything outside `ee/` and `web/src/ee/`: free under the
  [PolyForm Shield License](../../LICENSE), including in production and for any number of
  clusters.
- **Enterprise** is the Community edition plus paid features. Their code lives in `ee/` and
  `web/src/ee/` under the [Brokerium Commercial License](../../ee/LICENSE): production use needs a
  subscription and a license key, evaluation and testing do not.

The Community binaries and images contain none of the Enterprise code, and features that were free
in a published version never move to Enterprise.

## Enterprise features

| Feature | What it adds |
| --- | --- |
| Message tools | Delete records and empty topics, search, export and copy messages. |

The **License** page of the dashboard lists these features in both editions and shows which ones
the license covers.

## Install Brokerium Enterprise

Enterprise has its own image and binaries and takes the same configuration as Community. Both
editions use the same database, so moving from Community to Enterprise only replaces the image or
the binary.

- **Docker:** `bvaliev/brokerium-server-enterprise`, with the tags of `bvaliev/brokerium-server`.
- **Binary:** the release assets `brokerium-server-enterprise-<os>-<arch>`, listed in `SHA256SUMS`.
- **Helm:** `edition: enterprise`, see [below](#add-the-license-key).
- **From source:** `make build EDITION=enterprise` builds `bin/brokerium-server-enterprise` with the
  Enterprise dashboard, `make images EDITION=enterprise` the image.

## Add the license key

The server uses the first key it finds:

1. `BROKERIUM_LICENSE`: the key itself;
2. `BROKERIUM_LICENSE_FILE`: a file that contains the key, for example a mounted Secret;
3. the key stored in the database through the **License** page.

On the **License** page administrators paste the key and save it. The server verifies the key
first and refuses keys that do not verify, expired or are not valid yet. A key from the environment
or a file takes precedence and cannot be replaced or removed in the dashboard: change the variable
or the file and restart the server. The audit log records `license.update` and `license.delete`,
never the key itself. The API offers the same through `GET`, `PUT` and `DELETE /api/v1/license`
(see the [REST API](../../api/openapi.yaml)).

With Docker, pass `-e BROKERIUM_LICENSE="$(cat license.key)"`, or mount the file and set
`BROKERIUM_LICENSE_FILE`. With the Helm chart, keep the key in a Secret:

```bash
kubectl -n brokerium create secret generic brokerium-license --from-file=license=./license.key
helm upgrade --install brokerium brokerium/brokerium -n brokerium \
  --set edition=enterprise --set license.existingSecret=brokerium-license
```

`license.key` stores the key in the chart's Secret instead. After changing an existing Secret,
restart the server with `kubectl -n brokerium rollout restart deployment/brokerium`.

## States and the grace period

| State | Meaning | Enterprise features |
| --- | --- | --- |
| none | There is no license key. | Locked |
| valid | Between the start and the expiry of the key. | Work |
| grace | For the grace days of the key after its expiry; the dashboard warns. | Work |
| expired | After the grace period. | Stopped |
| invalid | The key does not verify, belongs to another edition or is not valid yet. | Locked |

The server evaluates the state at startup, when a key is stored and every hour, and logs every
change. The sidebar of the dashboard marks the **License** page during the grace period and when
the license expired or is invalid.

## What stops after expiry

After the grace period the routes of Enterprise features answer 403 `license_required`, and
Enterprise operations can no longer be planned or started. Nothing in Community changes, and
Enterprise operations that already run can still finish or be cancelled. A renewed key unlocks the
features as soon as it is stored, or after a restart when it comes from the environment or a file.

## Offline verification and no telemetry

A license key is the text `brk-lic-1.<payload>.<signature>`: the payload holds the signed claims
(license id, customer, features, start, expiry and grace days) as base64url JSON, the signature is
an Ed25519 signature over the prefix and the payload. The server verifies the key with public keys
built into the Enterprise binary, so it works in air-gapped networks. Brokerium never reports usage
and contacts no one to check a key.

## The commercial license

The [Brokerium Commercial License](../../ee/LICENSE) allows reading, evaluating and testing the
Enterprise code. Production use needs a subscription, and removing or bypassing the license checks
is not allowed. For subscriptions see [brokerium.kaufen/enterprise](https://brokerium.kaufen/enterprise/)
or write to [licensing@brokerium.kaufen](mailto:licensing@brokerium.kaufen); the plain-language
summary is in [LICENSING.md](../../LICENSING.md).

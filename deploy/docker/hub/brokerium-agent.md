# Brokerium agent

`brokerium-agent` runs on Apache Kafka® hosts managed by [Brokerium](https://brokerium.kaufen),
the control plane for Apache Kafka KRaft clusters. It connects out to the Brokerium server over
mutual TLS and runs a fixed set of typed commands: install Kafka releases, write configuration,
format storage, and start, stop and upgrade the node.

On virtual machines the agent usually runs as a systemd service, installed by the script the
server serves; see the [installation guide](https://brokerium.kaufen/install/). This image
carries only the static binary, for container platforms and for building your own images:

```dockerfile
COPY --from=bvaliev/brokerium-agent:latest /usr/local/bin/brokerium-agent /usr/local/bin/
```

- Documentation: https://brokerium.kaufen/docs/
- Source code and issues: https://github.com/brokerium/brokerium

## Tags

| Tag | Meaning |
| --- | --- |
| `latest`, `0.2`, `0.2.1` | The latest release, the latest patch of a minor version, an exact release |
| `edge` | The current `main` branch, for testing |
| `sha-<commit>` | The build of one commit |
| `0.2.1-lab`, `lab` | Lab node image of a release and of `main`: Apache Kafka with the agent as the main process, for the Docker provider of the server |

Images are built for `linux/amd64` and `linux/arm64` on a distroless base, run as the non-root
user 65532 and carry the license in `/licenses`. The lab node images are built on the official
`apache/kafka` image instead.

## License

Source-available under the PolyForm Shield License 1.0.0: free to use, including in production.
See [licensing](https://brokerium.kaufen/licensing/).

Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of the Apache
Software Foundation in the United States and/or other countries. Brokerium is not affiliated with,
endorsed by, or sponsored by the Apache Software Foundation.

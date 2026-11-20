# ADR 0026: Listeners, ports and security of clusters Brokerium runs

- Status: accepted
- Date: 2026-11-20

## Context

Clusters that Brokerium creates listen on PLAINTEXT `0.0.0.0:9092` for clients and brokers and on
PLAINTEXT `0.0.0.0:9093` for the controllers, and `User:ANONYMOUS` is a super user, so ACLs do not
restrict anyone. Ports, bind addresses, TLS and SASL cannot be chosen, and running clusters cannot
be moved to other ports or to authenticated listeners.

## Decision

- **Listener model.** A cluster has named listeners, each with a port, a protocol (`PLAINTEXT`,
  `SSL`, `SASL_PLAINTEXT`, `SASL_SSL`), its SASL mechanisms (`PLAIN`, `SCRAM-SHA-256`,
  `SCRAM-SHA-512`) and for TLS whether clients must present certificates. One listener is the
  inter-broker listener; the controller listener is separate. Each node binds to `0.0.0.0` or to
  one of the addresses its agent reports, and advertises its advertised host
  ([ADR 0018](0018-explicit-node-assignment.md)). Defaults are those of Apache Kafka: clients and
  brokers on 9092, controllers on 9093, PLAINTEXT, which keeps existing clusters as they are.
- **Controller listener.** PLAINTEXT, PLAIN over SASL, or TLS with client certificates. SCRAM is
  not offered there: controllers cannot authenticate each other with SCRAM while the credentials
  live in the metadata log they have not loaded yet (KAFKA-15513).
- **TLS.** Brokerium keeps a Kafka CA per installation, apart from the agent CA, and issues a
  certificate per node with the advertised host, the host name and the addresses of the node.
  Certificates are PEM files; Brokerium renews them 30 days before expiry and reloads them without
  a restart by setting the same keystore location again through IncrementalAlterConfigs
  (KAFKA-7429). Operators can instead upload their own CA certificate and key, from which
  Brokerium issues, or a certificate and key per node. The dashboard offers the CA certificate for
  clients.
- **SASL.** SCRAM users for the admin of Brokerium and for the brokers are created when the
  storage is formatted (`kafka-storage format --add-scram`, KIP-900); later SCRAM users through the
  Security tab. PLAIN users are kept by Brokerium, sealed, and rendered into the JAAS configuration
  of the listener; changing them restarts the nodes one by one.
- **Authorization.** With authentication on a listener, `allow.everyone.if.no.acl.found` is
  `false` unless the operator keeps it on, and `super.users` lists the admin of Brokerium and the
  principal of the brokers (and the certificate principals of mTLS). Without authentication the
  current behaviour stays, and the problems report it.
- **Secrets.** Passwords, keys and JAAS settings are sealed in the database and reach the agent
  only encrypted for that node (ADR 0027), so the insecure agent transport
  ([ADR 0017](0017-agent-transport-behind-proxies.md)) does not reveal them. The agent writes them
  readable for the `kafka` group only; `server.properties` becomes `0640`.
- **Registration.** After a creation or a change, the connection profile of the cluster in
  Brokerium moves to a listener it can authenticate on, with the admin credentials and the CA.
- **Running clusters.** `change-listeners` moves a managed cluster in phases, each a rolling
  restart with health gates: add the new listeners; switch the inter-broker and controller
  listeners; switch the registration of Brokerium; remove the old listeners. Changing the
  controller listener of a dynamic quorum also updates the voter endpoints (UpdateRaftVoter,
  KIP-853), verified in the lab before it is offered. Clients outside Brokerium have to move to
  the new listeners between the phases; the plan says so.
- **Edition.** Listeners, ports, bind addresses, TLS, SASL and `change-listeners` are the
  Enterprise feature `security`.

## Consequences

- Clusters can be created with authentication and encryption from the start and moved to them
  later without downtime.
- The Kafka CA is a secret of the installation; losing the database means issuing new
  certificates.
- Plain-text clusters keep working unchanged.

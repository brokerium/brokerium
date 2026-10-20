# ADR 0017: Agent transport behind TLS-terminating proxies

- Status: accepted
- Date: 2026-10-20

## Context

Agents connect out to the agent gateway with gRPC over TLS 1.3. Enrollment pins the server CA
with the fingerprint in the join token `brk1.<secret>.<SHA-256 of the CA certificate>`, and every
session uses mutual TLS with a client certificate of the internal CA (ADR 0009, ADR 0014).

Many networks put a reverse proxy in front of every server and let it terminate TLS: OpenResty
or nginx with `grpc_pass`, ingress controllers, corporate proxies that inspect TLS. Such a proxy
presents its own certificate, so the fingerprint check fails, and it keeps the client
certificate, so mutual TLS cannot authenticate the agent. Without another option these users
have to open the gateway port past the proxy or cannot manage hosts with agents at all.

## Decision

An optional insecure transport moves authenticity from the connection into the messages. It
trusts the transport for nothing; TLS between agent and proxy only adds confidentiality.

### Listener and options

- `BROKERIUM_AGENT_INSECURE_ADDR` (empty by default, `:8444` in the Helm chart) starts a plaintext
  gRPC listener, HTTP/2 with prior knowledge as `grpc_pass grpc://` speaks it, next to the mutual
  TLS gateway and with the same keepalive settings. It serves only the insecure flavor of
  `Enroll` and `Connect`. `RenewCertificate`, which relies on client certificates, is not
  implemented there, and the mutual TLS gateway refuses enrollment proofs.
- The agent takes `BROKERIUM_AGENT_TRANSPORT=mtls|insecure` (default `mtls`) and, for `insecure`,
  `BROKERIUM_AGENT_TLS=system|skip-verify|none`: TLS verified against the system CAs plus
  `BROKERIUM_AGENT_CA_FILE`, TLS without verification, or plaintext. It keeps the default gRPC
  dialer, so `HTTPS_PROXY` keeps working.
- The server offers the transport in the install command with `BROKERIUM_AGENT_TRANSPORT`,
  `BROKERIUM_AGENT_INSECURE_ENDPOINT` (the address of the proxy) and `BROKERIUM_AGENT_INSECURE_TLS`.

### Encoding of MAC and signature inputs

Every MAC and signature covers a byte string made of a domain and fields, each written as a
4-byte big-endian length followed by its bytes; numbers are 8-byte big-endian fields. Different
field lists therefore never encode the same, and the domain names the purpose and version of
the input. MACs are HMAC-SHA256, signatures ECDSA P-256 with SHA-256 in ASN.1 form by the key of
the internal CA, which agents pin through the join token. The CA key also signs certificates;
their signatures cover a DER `TBSCertificate`, which starts with the byte `0x30`, while every
protocol input starts with the length of its domain, whose first byte is 0.

### Enrollment

The agent never sends the token secret. Its key is `K = auth.HashToken(secret)`, the lowercase
hex SHA-256 of the secret as text, which is what the server stores for the token.

1. The agent creates an X25519 key pair and sends `EnrollRequest{hostname, agent_version,
   proof}` with `proof = {token_lookup = SHA-256(K), public_key, nonce (16 random bytes), mac}`
   and `mac = HMAC(K, "brokerium-enroll-v1", token_lookup, public_key, nonce, hostname,
   agent_version)`.
2. The server finds the unused, unexpired join token whose stored hash has the lookup as SHA-256,
   verifies the MAC with that hash and only then consumes the token. It creates or updates the
   node like the mutual TLS enrollment, generates a random 32-byte node secret, stores it sealed
   with the server key and sets the transport of the node to `insecure`, which drops its
   certificates.
3. The response carries `node_id`, `node_name`, `ca_cert_der`, `node_secret = {public_key (an
   ephemeral X25519 key), nonce, ciphertext}` and `signature`. The ciphertext is the node secret
   under AES-256-GCM with the node id as additional data and the key `HKDF-SHA256(X25519 shared
   secret, no salt, info "brokerium-node-secret-v1")`. The signature covers
   `"brokerium-enroll-response-v1", request mac, node_id, node_name, ca_cert_der,
   node_secret.public_key, node_secret.nonce, node_secret.ciphertext`.
4. The agent checks the CA certificate against the fingerprint of the join token, verifies the
   signature with its key, decrypts the node secret and stores it in `node.secret` (mode 0600,
   atomic write) next to `node.json` and `ca.crt`.

### Session

1. The agent opens `Connect` with `SessionHello{node_id, agent_nonce (16 random bytes),
   timestamp_ms, mac}`, `mac = HMAC(secret, "brokerium-hello-v1", node_id, agent_nonce,
   timestamp_ms)`.
2. The server rejects unknown nodes, nodes whose transport is not `insecure`, wrong MACs and
   timestamps more than five minutes off. It answers `SessionAccept{server_nonce (16 random
   bytes), signature}` over `"brokerium-session-v1", node_id, agent_nonce, server_nonce`, which
   the agent verifies.
3. Every later message is a `SignedMessage{seq, signed_body, signature | mac}`. `seq` counts the
   messages of each direction from 1; `signed_body` is the deterministic protobuf encoding of the
   message it carries.
   - Server to agent: a `ServerMessage` with a command, signed over `"brokerium-command-v1",
     node_id, agent_nonce, server_nonce, seq, signed_body`.
   - Agent to server: an `AgentMessage` with the hello, a result or a status, with
     `mac = HMAC(secret, "brokerium-agent-v1", node_id, agent_nonce, server_nonce, seq,
     signed_body)`.
4. The receiver verifies the signature or MAC, then requires `seq` to be one more than the last
   one, and only then decodes `signed_body`; the agent executes only commands that pass. Any
   failure closes the session and is logged without secrets.
5. The first agent message must be its hello. Only that authenticated message registers the
   session, replacing an earlier session of the node, so a replayed session hello opens nothing.
   Streams that do not get this far within 30 seconds are closed.

## Threat analysis

A man in the middle is the proxy itself or anyone on a segment without TLS.

He can:

- read every message on a segment without TLS or inside a compromised proxy: commands with the
  Kafka configuration, results and status. Confidentiality depends entirely on the transport.
- drop, delay or block messages and connections, or forward an enrollment request before the
  agent's own copy arrives and so use up the join token. These are denials of service; the
  operator creates a new token.
- see which token and node a request is for, and when nodes connect.

He cannot:

- learn the join token secret or the stored hash: only SHA-256 of the hash travels.
- redeem the token for a key of his own: the MAC covers the X25519 key of the agent, and a proof
  that does not match leaves the token valid.
- read or replace the node secret: it is encrypted to the ephemeral key of the agent and signed
  together with the MAC of the request.
- impersonate the server: commands and session answers need the CA key, and the agent knows the
  CA certificate from the fingerprint in the join token.
- impersonate the agent: hello, results and status need the node secret.
- replay messages of an earlier session, or move them to another node: both nonces and the node
  id are part of every input.
- replay, reorder or drop messages within a session unnoticed: `seq` must grow by one.

## Why mutual TLS stays the default

- Mutual TLS authenticates both sides and encrypts end to end; no proxy is trusted for anything.
  With the insecure transport, whoever terminates TLS reads all commands and results.
- The plaintext listener belongs on trusted segments only: between the proxy and the server, in
  the same Kubernetes cluster or data center network.
- Certificates are renewed after two thirds of their lifetime (ADR 0014) and revoked by a new
  enrollment. The node secret has no lifetime and is not rotated yet.
- Setups that keep mutual TLS should be preferred: TLS passthrough (Gateway API `TLSRoute`, an
  nginx `stream` block), a `LoadBalancer` service for the agent port (for example with Cilium
  BGP), or an HTTP CONNECT proxy, which the agent supports with `HTTPS_PROXY`.

## Consequences

- The `nodes` table records the transport and the sealed node secret (migration 0004). An
  enrollment with the other transport switches the node: the new credentials replace the old
  ones, which stop authenticating.
- Certificate renewal does not apply to nodes of the insecure transport. Rotating the node secret
  is out of scope for now: to replace it, enroll the node again with a new join token.
- Agents behind a TLS-terminating proxy need the insecure transport; the install command on the
  **Nodes** page offers it when the server is configured for it, also through the Helm values
  `agents.insecure.*`.
- Messages are authenticated but not encrypted by the protocol itself; TLS to the proxy and a
  trusted segment behind it remain necessary for confidentiality.

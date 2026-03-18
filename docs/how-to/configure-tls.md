# Configure TLS

TLS is required for any non-local deployment. Without it, agent tokens and task output are transmitted in plaintext.

## Using a CA-Issued Certificate

If your certificate is signed by a CA your agents already trust (a public CA or one in your system trust store), agent config requires no additional TLS configuration:

```hcl
# server.hcl
server {
  tls {
    cert_file = "/etc/fluke/tls/server.crt"
    key_file  = "/etc/fluke/tls/server.key"
  }
}
```

```hcl
# agent.hcl
agent {
  server_url = "grpcs://fluke.internal:7070"
  # No tls block needed if the CA is in the system trust store
}
```

## Using a Self-Signed Certificate

Generate a CA and server certificate:

```bash
# CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt -subj "/CN=fluke-ca"

# Server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr -subj "/CN=fluke-server"
openssl x509 -req -days 3650 -in server.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt
```

Distribute `ca.crt` to all agent hosts, then configure:

```hcl
# server.hcl
server {
  tls {
    cert_file = "/etc/fluke/tls/server.crt"
    key_file  = "/etc/fluke/tls/server.key"
  }
}
```

```hcl
# agent.hcl
agent {
  server_url = "grpcs://fluke.internal:7070"

  tls {
    ca_file = "/etc/fluke/tls/ca.crt"
  }
}
```

## Disabling TLS

Only for local development. Never in production.

```hcl
server {
  tls {
    enabled = false
  }
}
```

```hcl
agent {
  server_url = "grpc://localhost:7070"   # grpc://, not grpcs://
}
```

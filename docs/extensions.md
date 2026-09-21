# External extensions

ProxySieve Extension API v1 runs trusted local programs out of process. A crash,
timeout or malformed response fails the call without crashing the gateway process.
Process isolation is not a security sandbox: install and run only code reviewed by
an administrator.

## Build and call an example

Each example uses only the public `pkg/extension` package. Build the provider next
to its manifest, then make one explicit call:

```sh
go build -o examples/extensions/provider/extension ./examples/extensions/provider
go build -o bin/proxysieve ./cmd/proxysieve
./bin/proxysieve extension call \
  --root ./examples/extensions/provider \
  --manifest manifest.json \
  --capability proxy-provider \
  --method fetch \
  --input '{}'
```

PowerShell uses the same manifest; the runner resolves the native `.exe` suffix:

```powershell
go build -o examples\extensions\provider\extension.exe .\examples\extensions\provider
go build -o bin\proxysieve.exe .\cmd\proxysieve
.\bin\proxysieve.exe extension call --root .\examples\extensions\provider --manifest manifest.json --capability proxy-provider --method fetch --input '{}'
```

The selector supports `selector/select` with a bounded `candidates` array and
returns `proxy_id`. The alert sink supports `alert-sink/deliver` and acknowledges
a bounded event type. Their manifests are under `examples/extensions/selector`
and `examples/extensions/alert-sink`.

## Manifest

Manifests are bounded JSON files. The manifest and executable must resolve inside
the supplied extension root, including after symlink resolution.

```json
{
  "name": "example-provider",
  "version": "0.1.0",
  "api_version": 1,
  "command": "./extension",
  "capabilities": ["proxy-provider"],
  "permissions": []
}
```

Supported capabilities are `proxy-provider`, `selector` and `alert-sink`.
`permissions` declares review-relevant needs but does not create an OS sandbox.
Extension installation, discovery and downloading are never automatic.

## Protocol v1

The host and child exchange newline-delimited JSON over standard input/output:

```text
host      -> {"type":"hello","api_version":1}
extension -> {"type":"hello","name":"...","version":"...","api_version":1,"capabilities":["..."]}
host      -> {"type":"call","id":"1","capability":"...","method":"...","input":{...}}
extension -> {"type":"result","id":"1","output":{...}}
```

Failures use `{"type":"error","id":"1","error":{"code":"invalid_input"}}`.
Codes are stable machine-readable tokens; messages are optional and bounded.

One process handles one call. Frames are limited to 1 MiB, manifests to 64 KiB,
timeouts to 30 seconds and restarts to three attempts. Raw stderr is discarded
because it may contain secrets; only its byte count and a 64 KiB truncation flag
are reported. The child receives only a
small OS/runtime environment plus `PROXYSIEVE_EXTENSION_API=1`; ProxySieve does not
provide raw stored secrets. Cancellation kills the owned child process.

Use `extension.Serve` to implement the handshake and framing. Return JSON output
or a structured `extension.ProtocolError`; do not write logs to stdout.

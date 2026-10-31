# runner-core

runner-core is the backend-agnostic front of an agyn runner, extracted from the
[k8s-runner](https://github.com/agynio/k8s-runner) so that every runner
implementation shares it: enrollment with the Gateway and the service token,
catalog loading and reporting, the Runner gRPC API server, workload state
reporting, and the configuration and logging around them. A runner implements
the backend interface for whatever hosts its workloads — Pods, microVMs — and
hands it to this module, which does the rest.

Architecture: [Runner](https://github.com/agynio/architecture/blob/main/architecture/runner.md)

## Layout

| Package | Holds |
|---|---|
| `runnercore` (root) | `Run`: serves the Runner API on TCP and, when enrolled, on the runner's OpenZiti service; enrols with the Gateway, reports the catalog, watches the identity, runs state reporting |
| `backend` | The seam a runner implements: `Backend` (workloads, exec, logs, events, state source) and the optional `Hibernator` |
| `server` | The RunnerService gRPC server over a `Backend`: exec session protocol, log streaming, `Ready`, `Unimplemented` for hibernation unless the backend can |
| `reporter` | Best-effort `ReportWorkloadState` to the Gateway with change suppression; implements `backend.Observer` |
| `catalog` | The platform-facing catalog: loading, validation, capability merging and the `ReportRunnerCatalog` request |
| `config`, `logging` | The configuration and logger every runner shares |
| `gen` | Bindings for `buf.build/agynio/api` (`runner/v1`, `runners/v1`, `gateway/v1` and what they import), committed so consumers need no generator |

A runner advertises `hibernate` exactly when its backend implements
`backend.Hibernator`; a declared catalog entry alone never does.

## Regenerating the bindings

```bash
buf generate --include-imports
```

`buf.gen.yaml` takes the module from the BSR at its latest `main`; the commit
the committed output was generated from is named in the commit that updated it.

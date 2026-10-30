# runner-core

runner-core is the backend-agnostic front of an agyn runner, extracted from the
[k8s-runner](https://github.com/agynio/k8s-runner) so that every runner
implementation shares it: enrollment with the Gateway and the service token,
catalog loading and reporting, the Runner gRPC API server, workload state
reporting, and the configuration and logging around them. A runner implements
the backend interface for whatever hosts its workloads — Pods, microVMs — and
hands it to this module, which does the rest.

Architecture: [Runner](https://github.com/agynio/architecture/blob/main/architecture/runner.md)

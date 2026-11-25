# Preserve running limits during a webhook handoff

Capture the running CPU and memory limits before disabling the previous optimizer. A shared OptimizationPolicy can hold a separate capture for each workload UID. The capture command reads the controlling workload and its Ready Pods, checks their owner UIDs and compares limits across replicas. By default the CLI requires agreement across replicas. For workloads with different running resources, choose the capture mode that matches their controller.

```sh
optipodctl capture-limits --kubeconfig "$KUBECONFIG" --context "$CONTEXT" \
  --namespace "$NAMESPACE" --kind Deployment --name "$WORKLOAD" \
  --workload-uid "$WORKLOAD_UID" > limits.json &&
kubectl --context "$CONTEXT" --namespace "$POLICY_NAMESPACE" annotate optimizationpolicy "$POLICY" \
  "optipod.io/capture-${WORKLOAD_UID}=$(cat limits.json)" --overwrite
```

For a shared Auto policy, set `spec.workloadSettings.requireActivation` and stage the member with `byUID[WORKLOAD_UID].active` set to false. A dedicated policy can be staged in Recommend. Select the workload explicitly, set the update strategy to webhook, enable updateRequestsOnly, and use Preserve for the CPU and memory limit modes. For deferred workloads select onNextRestart and disable in-place resize and recreation. Save the original running requests separately. Verify the previous optimizer has relinquished the workload before activating the member or switching a dedicated policy to Auto.

On Pod creation the webhook applies the recorded limits to eligible containers in the workload UID named in the policy annotation. An empty limits map means both limits were absent. Other resource types are preserved. Native and injected sidecars follow the policy's inclusion settings, exclusions and horizontal-scaler restrictions. One-shot init containers keep their admission defaults. Changing a declared container limit in the workload template retires the capture for that resource. Remove the policy annotation to retire the whole capture.

Once the workload is enrolled in webhook admission, a fixed capture can preserve limits when a request recommendation expires. Constraint checks still apply. A request above a captured limit, for example, can block admission. Verify a server-side dry-run owned by the actual ReplicaSet or workload before a rollout. With webhook failurePolicy set to Ignore, Kubernetes can admit template defaults when the webhook is unavailable.

When OptiPod calculates a container request above a finite preserved limit, it uses the limit as the effective request if the request still satisfies its safety minimums. It evaluates CPU and memory independently for each container. A deferred policy can renew this plan while the existing Pods continue at their running requests. Check scheduler capacity for the effective request before the next replacement.

In the console, inspect the workload's container recommendations to see the raw and effective requests when a cap applies. Decision history also records the original quantity, effective quantity, and reason in the recommendation explanation. For example, a raw memory request of 9Gi under an 8Gi preserved limit yields an effective request of 8Gi.

## Preserve horizontal scaling on replacement Pods

Add `--include-hpa-requests` to the capture command to record the CPU or memory requests used by utilization-based HPA or KEDA targets. Resource targets apply to all eligible containers. ContainerResource targets apply only to the named container. Verify the running replicas agree before capture. Use an Auto, request-only webhook policy with `onNextRestart` and enroll the workload in the webhook, including workloads with every dimension reserved.

At admission the webhook checks the workload UID, current target and declared request. A matching capture restores the running denominator even when there is no fresh vertical recommendation. Preserve-mode limits can also be restored for that dimension. Changing the declared request, removing the utilization target, pausing the workload or returning the policy to Recommend releases the captured request. Captures created without the flag retain their original limit-only behavior.

For larger fleets, set `OPTIPOD_POLICY_WORKERS` for Recommend and other background policies, and `OPTIPOD_ADMISSION_WORKERS` for Auto webhook policies. The defaults are 1 and 4. Deferred request-only members share the admission worker budget across policies. Immediate rollouts within a policy run sequentially. Set either from 1 through 32 using the controller chart's `extraEnv`. Measure plan expiry margins, controller resource use and Prometheus latency when increasing concurrency.

### DaemonSets with different resources on different nodes

Use `optipodctl capture-limits --kind DaemonSet --node-handoff-duration 15m` with the usual kubeconfig, namespace, name and workload UID flags. Capture after every desired node has one updated, Ready Pod. The CLI records the Node UID, CPU and memory requests and limits for each running container, including absent keys. Check the capture fits in the policy annotation before withdrawing the previous optimizer.

With a request-only Auto webhook policy using `onNextRestart`, replacement Pods on the same node retain their captured requests until the UTC deadline in the snapshot. After the deadline, normal request recommendations resume. Captured limits retain the usual template-edit and policy ownership checks. A new node, including one recreated under an old name, uses the normal policy. Replacing the DaemonSet invalidates the whole capture. The node capture is independent of percentage-based DaemonSet sizing and requires permission to read Nodes.

### StatefulSets with different resources per ordinal

Use `--kind StatefulSet --ordinal-handoff-duration 4h` with the capture command. Each existing ordinal retains its own per-container request and limit baseline. The webhook verifies the StatefulSet UID and the controller-assigned Pod name. The hold applies to requests until the recorded deadline. Captured limits and HPA-owned requests follow the template and ownership checks described above. A new ordinal uses the normal policy. A recreated StatefulSet needs a new capture.

### Deployments with different resources across replicas

For a healthy Deployment with interchangeable replicas, `--replica-maximum-handoff-duration 4h` captures the highest running value independently for each container and resource. A missing limit on any replica makes that dimension unbounded. Inspect the captured maxima and cluster quota before activation.

### Operator-owned workloads and whole-Pod budgets

A policy writer can explicitly activate an operator-owned native workload by UID for deferred, request-only webhook updates. Check that the operator preserves the workload annotations that carry plans, pause state and recovery history. If the operator overwrites them, use Recommend and keep the operator-declared budget.

For a Pod with native CPU or memory requests under `spec.resources`, deferred request-only admission updates a Pod-level dimension when all contributing containers have eligible recommendations. Container floors, init containers and Pod limits constrain the result. The webhook preserves container resources and dimensions reserved for horizontal scaling.

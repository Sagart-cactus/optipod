# Impact Report (before Auto)

OptiPod is safe by default in **Recommend mode**. When you’re ready to opt in to `mode: Auto`, it helps to understand the blast radius first: *what will change, and by how much?*

This repository includes an **impact report** script that reads OptiPod’s per-workload recommendation annotations and generates a sortable HTML summary.

## What the report shows

- Only workloads that have OptiPod recommendation annotations.
- Current vs recommended **CPU and memory requests**.
- **Replica-weighted totals** (the delta multiplied by the number of pods), so you can estimate cluster-level impact before enabling Auto.
- Warnings (for example: when `updateRequestsOnly=true` but recommended requests would exceed existing limits).

![OptiPod Impact Report HTML](../scripts/report-html.png)

## Prerequisites

- `kubectl` configured to access your cluster
- `jq` installed locally

## Generate the report

From a clone of this repo:

```bash
./optipod-recommendation-report.sh -o html -f optipod-impact.html
```

Or run the script directly from GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/Sagart-cactus/optipod/main/optipod-recommendation-report.sh -o optipod-recommendation-report.sh
chmod +x optipod-recommendation-report.sh
./optipod-recommendation-report.sh -o html -f optipod-impact.html
```

Open the file in a browser:

```bash
open optipod-impact.html
```

## How to use it for Auto adoption

1. Start with `mode: Recommend` and let OptiPod produce recommendations.
2. Generate the impact report and review:
   - Total CPU/memory request changes (replica-weighted)
   - Any warnings
3. Adjust your policy bounds and update strategy if needed.
4. Switch the policy to `mode: Auto` only after the report looks safe.

## How to read the HTML report

**Cards**

- **Workloads optimized**: `workloads with recommendations / workloads scanned` and the percent. “Container rows” is the number of containers with recommendations.
- **Difference (Requests)**: replica-weighted total delta across pods (Σ (recommended − current) × pods). `▼` means a decrease, `▲` means an increase.
- **Requests summary**: replica-weighted totals for Recommended vs Current, plus `Δ` (same sign convention).

**Table columns**

- **Workload / Namespace / Type / Pods / Container / Policy**: identifies what would be impacted and how many pods the change would affect.
- **CPU req / Mem req**: current → recommended values (shown in `cores` and `GiB` for scanability). Hover to see raw values (`m`, `Mi`) from the workload spec/annotations.
- **CPU Δ / Mem Δ**: replica-weighted total delta across pods for that row.
- **Warnings**: count badge; hover for details. Common warnings include requests that would exceed existing limits when `updateRequestsOnly=true`.
- **Last rec**: when OptiPod last wrote recommendations to the workload.

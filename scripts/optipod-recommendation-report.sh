#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/optipod-recommendation-report.sh [--output json|html] [--file PATH] [--namespace NS]

Generates a report comparing current CPU/memory requests/limits vs OptiPod recommended values (from annotations).

Notes:
- Requires: kubectl, jq
- OptiPod recommendations are read from workload annotations like:
  optipod.io/recommendation.<container>.cpu-request, memory-request, cpu-limit, memory-limit
- Policy information is read from optipod.io/policy and optipod.io/policy-uid annotations
- Checks for potential issues when updateRequestsOnly=true but recommendations exceed current limits

Options:
  --output, -o     Output format: json (default), html
  --file, -f       Write output to file instead of stdout
  --namespace, -n  Restrict to a namespace (default: all namespaces)
  --kinds          Comma-separated kinds to scan (default: deploy,sts,ds)
  --help, -h       Show this help
EOF
}

OUTPUT="json"
OUT_FILE=""
NAMESPACE=""
KINDS="deploy,sts,ds"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -o|--output) OUTPUT="${2:-}"; shift 2;;
    -f|--file) OUT_FILE="${2:-}"; shift 2;;
    -n|--namespace) NAMESPACE="${2:-}"; shift 2;;
    --kinds) KINDS="${2:-}"; shift 2;;
    -h|--help) usage; exit 0;;
    *) echo "Unknown arg: $1" >&2; usage; exit 2;;
  esac
done

if [[ "$OUTPUT" != "json" && "$OUTPUT" != "html" ]]; then
  echo "Invalid --output: $OUTPUT (expected json|html)" >&2
  exit 2
fi

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "Missing required command: $1" >&2; exit 2; }
}

need kubectl
need jq

now_utc() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

b64_decode() {
  # GNU coreutils: base64 --decode
  base64 --decode 2>/dev/null || \
  # macOS/BSD: base64 -D
  base64 -D 2>/dev/null || \
  # Fallback (macOS typically has openssl)
  openssl base64 -d 2>/dev/null
}

html_escape() {
  jq -nr --arg v "${1:-}" '$v|@html'
}

cpu_to_milli() {
  local q="${1:-}"
  [[ -z "$q" ]] && { echo ""; return 0; }
  if [[ "$q" =~ ^[0-9]+m$ ]]; then
    echo "${q%m}"
    return 0
  fi
  if [[ "$q" =~ ^[0-9]+$ ]]; then
    awk -v v="$q" 'BEGIN{printf "%.0f", v*1000}'
    return 0
  fi
  if [[ "$q" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
    awk -v v="$q" 'BEGIN{printf "%.0f", v*1000}'
    return 0
  fi
  echo ""
}

mem_to_bytes() {
  local q="${1:-}"
  [[ -z "$q" ]] && { echo ""; return 0; }

  # Binary SI (Ki, Mi, Gi, Ti, Pi, Ei)
  if [[ "$q" =~ ^([0-9]+(\.[0-9]+)?)(Ki|Mi|Gi|Ti|Pi|Ei)$ ]]; then
    local num="${BASH_REMATCH[1]}"
    local unit="${BASH_REMATCH[3]}"
    local pow=0
    case "$unit" in
      Ki) pow=10;;
      Mi) pow=20;;
      Gi) pow=30;;
      Ti) pow=40;;
      Pi) pow=50;;
      Ei) pow=60;;
    esac
    awk -v n="$num" -v p="$pow" 'BEGIN{printf "%.0f", n*(2^p)}'
    return 0
  fi

  # Decimal SI (K, M, G, T, P, E)
  if [[ "$q" =~ ^([0-9]+(\.[0-9]+)?)(K|M|G|T|P|E)$ ]]; then
    local num="${BASH_REMATCH[1]}"
    local unit="${BASH_REMATCH[3]}"
    local mul=1
    case "$unit" in
      K) mul=1000;;
      M) mul=1000000;;
      G) mul=1000000000;;
      T) mul=1000000000000;;
      P) mul=1000000000000000;;
      E) mul=1000000000000000000;;
    esac
    awk -v n="$num" -v m="$mul" 'BEGIN{printf "%.0f", n*m}'
    return 0
  fi

  # Raw bytes
  if [[ "$q" =~ ^[0-9]+$ ]]; then
    echo "$q"
    return 0
  fi

  echo ""
}

fmt_cpu() {
  local milli="${1:-}"
  [[ -z "$milli" ]] && { echo "—"; return 0; }
  echo "${milli}m"
}

fmt_cpu_cores() {
  local milli="${1:-}"
  [[ -z "$milli" ]] && { echo "—"; return 0; }
  awk -v m="$milli" 'BEGIN{printf "%.2f", m/1000.0}'
}

abs_number() {
  local v="${1:-}"
  [[ -z "$v" ]] && { echo ""; return 0; }
  awk -v x="$v" 'BEGIN{ if (x < 0) x = -x; printf "%.0f", x }'
}

arrow_for_dir() {
  case "${1:-}" in
    decrease) printf '▼';;
    increase) printf '▲';;
    no-change) printf '—';;
    *) printf '—';;
  esac
}

arrow_for_delta() {
  local delta="${1:-}"
  [[ -z "$delta" || "$delta" == "null" ]] && { printf '—'; return 0; }
  if awk -v d="$delta" 'BEGIN{exit !(d<0)}'; then printf '▼'; return 0; fi
  if awk -v d="$delta" 'BEGIN{exit !(d>0)}'; then printf '▲'; return 0; fi
  printf '—'
}

fmt_mem_mi() {
  local bytes="${1:-}"
  [[ -z "$bytes" ]] && { echo "—"; return 0; }
  awk -v b="$bytes" 'BEGIN{printf "%.2fMi", b/1048576}'
}

fmt_mem_gi() {
  local bytes="${1:-}"
  [[ -z "$bytes" ]] && { echo "—"; return 0; }
  awk -v b="$bytes" 'BEGIN{printf "%.2fGi", b/1073741824}'
}

fmt_mem_display() {
  local v="${1:-}"
  [[ -z "$v" || "$v" == "—" ]] && { echo "—"; return 0; }
  if [[ "$v" =~ ^[0-9]+$ ]]; then
    fmt_mem_mi "$v"
    return 0
  fi
  echo "$v"
}

direction() {
  local delta="${1:-}"
  [[ -z "$delta" ]] && { echo "n/a"; return 0; }
  if awk -v d="$delta" 'BEGIN{exit !(d>0)}'; then echo "increase"; return 0; fi
  if awk -v d="$delta" 'BEGIN{exit !(d<0)}'; then echo "decrease"; return 0; fi
  echo "no-change"
}

percent_change() {
  local cur="${1:-}"
  local rec="${2:-}"
  [[ -z "$cur" || -z "$rec" ]] && { echo ""; return 0; }
  if awk -v c="$cur" 'BEGIN{exit !(c==0)}'; then echo ""; return 0; fi
  awk -v c="$cur" -v r="$rec" 'BEGIN{printf "%.2f", ((r-c)/c)*100.0}'
}

CTX="$(kubectl config current-context 2>/dev/null || true)"
GEN_AT="$(now_utc)"

KUBECTL_ARGS=(get "$KINDS")
if [[ -n "$NAMESPACE" ]]; then
  KUBECTL_ARGS=(get "$KINDS" -n "$NAMESPACE")
else
  KUBECTL_ARGS=(get "$KINDS" -A)
fi

WORKLOADS_JSON="$(mktemp)"
RECORDS_NDJSON="$(mktemp)"
trap 'rm -f "$WORKLOADS_JSON" "$RECORDS_NDJSON"' EXIT

kubectl "${KUBECTL_ARGS[@]}" -o json >"$WORKLOADS_JSON"
SCANNED_WORKLOADS_COUNT="$(jq -r '.items | length' "$WORKLOADS_JSON")"
SCANNED_NAMESPACES_COUNT="$(jq -r '[.items[].metadata.namespace] | unique | length' "$WORKLOADS_JSON")"

# Totals (only count where both current and recommended exist)
count_items=0

while IFS= read -r row_b64; do
  row_json="$(printf '%s' "$row_b64" | b64_decode)"

  kind="$(jq -r '.kind' <<<"$row_json")"
  ns="$(jq -r '.namespace' <<<"$row_json")"
  wl="$(jq -r '.workload' <<<"$row_json")"
  pods="$(jq -r '.pods' <<<"$row_json")"
  policy="$(jq -r '.policy' <<<"$row_json")"
  policyUID="$(jq -r '.policyUID' <<<"$row_json")"
  lastRec="$(jq -r '.lastRecommendation' <<<"$row_json")"
  container="$(jq -r '.container' <<<"$row_json")"

  curCpuReq="$(jq -r '.curCpuReq' <<<"$row_json")"
  curMemReq="$(jq -r '.curMemReq' <<<"$row_json")"
  curCpuLim="$(jq -r '.curCpuLim' <<<"$row_json")"
  curMemLim="$(jq -r '.curMemLim' <<<"$row_json")"

  recCpuReq="$(jq -r '.recCpuReq' <<<"$row_json")"
  recMemReq="$(jq -r '.recMemReq' <<<"$row_json")"
  recCpuLim="$(jq -r '.recCpuLim' <<<"$row_json")"
  recMemLim="$(jq -r '.recMemLim' <<<"$row_json")"

  curCpuReq_m="$(cpu_to_milli "$curCpuReq")"
  recCpuReq_m="$(cpu_to_milli "$recCpuReq")"
  curCpuLim_m="$(cpu_to_milli "$curCpuLim")"
  recCpuLim_m="$(cpu_to_milli "$recCpuLim")"

  curMemReq_b="$(mem_to_bytes "$curMemReq")"
  recMemReq_b="$(mem_to_bytes "$recMemReq")"
  curMemLim_b="$(mem_to_bytes "$curMemLim")"
  recMemLim_b="$(mem_to_bytes "$recMemLim")"

  hasRec="false"
  if [[ -n "$recCpuReq" || -n "$recMemReq" || -n "$recCpuLim" || -n "$recMemLim" ]]; then
    hasRec="true"
  fi

  dCpuReq=""
  dCpuLim=""
  dMemReq=""
  dMemLim=""

  if [[ -n "$curCpuReq_m" && -n "$recCpuReq_m" ]]; then dCpuReq=$((recCpuReq_m - curCpuReq_m)); fi
  if [[ -n "$curCpuLim_m" && -n "$recCpuLim_m" ]]; then dCpuLim=$((recCpuLim_m - curCpuLim_m)); fi
  if [[ -n "$curMemReq_b" && -n "$recMemReq_b" ]]; then dMemReq=$((recMemReq_b - curMemReq_b)); fi
  if [[ -n "$curMemLim_b" && -n "$recMemLim_b" ]]; then dMemLim=$((recMemLim_b - curMemLim_b)); fi

  impactCpuReq=""
  impactMemReq=""
  impactCpuLim=""
  impactMemLim=""
  if [[ -n "$pods" && "$pods" != "null" ]] && awk -v p="$pods" 'BEGIN{exit !(p>0)}'; then
    if [[ -n "$dCpuReq" ]]; then impactCpuReq="$(awk -v d="$dCpuReq" -v p="$pods" 'BEGIN{printf "%.0f", d*p}')"; fi
    if [[ -n "$dMemReq" ]]; then impactMemReq="$(awk -v d="$dMemReq" -v p="$pods" 'BEGIN{printf "%.0f", d*p}')"; fi
    if [[ -n "$dCpuLim" ]]; then impactCpuLim="$(awk -v d="$dCpuLim" -v p="$pods" 'BEGIN{printf "%.0f", d*p}')"; fi
    if [[ -n "$dMemLim" ]]; then impactMemLim="$(awk -v d="$dMemLim" -v p="$pods" 'BEGIN{printf "%.0f", d*p}')"; fi
  fi

  pCpuReq="$(percent_change "${curCpuReq_m:-}" "${recCpuReq_m:-}")"
  pCpuLim="$(percent_change "${curCpuLim_m:-}" "${recCpuLim_m:-}")"
  pMemReq="$(percent_change "${curMemReq_b:-}" "${recMemReq_b:-}")"
  pMemLim="$(percent_change "${curMemLim_b:-}" "${recMemLim_b:-}")"

  # Check for potential updateRequestsOnly issues
  warnings=()

  # Check if recommended CPU request exceeds current CPU limit (when limits exist)
  if [[ -n "$recCpuReq_m" && -n "$curCpuLim_m" && "$recCpuReq_m" -gt "$curCpuLim_m" ]]; then
    warnings+=("CPU_REQ_EXCEEDS_LIMIT")
  fi

  # Check if recommended memory request exceeds current memory limit (when limits exist)
  if [[ -n "$recMemReq_b" && -n "$curMemLim_b" && "$recMemReq_b" -gt "$curMemLim_b" ]]; then
    warnings+=("MEM_REQ_EXCEEDS_LIMIT")
  fi

  # Convert warnings array to JSON array
  if [[ ${#warnings[@]} -eq 0 ]]; then
    warnings_json="[]"
  else
    warnings_json="$(printf '%s\n' "${warnings[@]}" | jq -R . | jq -s .)"
  fi

  count_items=$((count_items + 1))

  jq -n \
    --arg kind "$kind" \
    --arg namespace "$ns" \
    --arg workload "$wl" \
    --argjson pods "${pods:-0}" \
    --arg container "$container" \
    --arg policy "$policy" \
    --arg policyUID "$policyUID" \
    --arg lastRecommendation "$lastRec" \
    --argjson hasRecommendation "$hasRec" \
    --argjson warnings "$warnings_json" \
    --arg curCpuReq "$curCpuReq" \
    --arg curMemReq "$curMemReq" \
    --arg curCpuLim "$curCpuLim" \
    --arg curMemLim "$curMemLim" \
    --arg recCpuReq "$recCpuReq" \
    --arg recMemReq "$recMemReq" \
    --arg recCpuLim "$recCpuLim" \
    --arg recMemLim "$recMemLim" \
    --argjson curCpuReqMilli "${curCpuReq_m:-null}" \
    --argjson recCpuReqMilli "${recCpuReq_m:-null}" \
    --argjson curCpuLimMilli "${curCpuLim_m:-null}" \
    --argjson recCpuLimMilli "${recCpuLim_m:-null}" \
    --argjson curMemReqBytes "${curMemReq_b:-null}" \
    --argjson recMemReqBytes "${recMemReq_b:-null}" \
    --argjson curMemLimBytes "${curMemLim_b:-null}" \
    --argjson recMemLimBytes "${recMemLim_b:-null}" \
    --argjson deltaCpuReqMilli "${dCpuReq:-null}" \
    --argjson deltaCpuLimMilli "${dCpuLim:-null}" \
    --argjson deltaMemReqBytes "${dMemReq:-null}" \
    --argjson deltaMemLimBytes "${dMemLim:-null}" \
    --argjson impactCpuReqMilli "${impactCpuReq:-null}" \
    --argjson impactCpuLimMilli "${impactCpuLim:-null}" \
    --argjson impactMemReqBytes "${impactMemReq:-null}" \
    --argjson impactMemLimBytes "${impactMemLim:-null}" \
    --arg cpuReqDirection "$(direction "${dCpuReq:-}")" \
    --arg cpuLimDirection "$(direction "${dCpuLim:-}")" \
    --arg memReqDirection "$(direction "${dMemReq:-}")" \
    --arg memLimDirection "$(direction "${dMemLim:-}")" \
    --argjson cpuReqPercent "${pCpuReq:-null}" \
    --argjson cpuLimPercent "${pCpuLim:-null}" \
    --argjson memReqPercent "${pMemReq:-null}" \
    --argjson memLimPercent "${pMemLim:-null}" \
    '{
      kind: $kind,
      namespace: $namespace,
      workload: $workload,
      pods: $pods,
      container: $container,
      policy: ($policy | select(length>0) // null),
      policyUID: ($policyUID | select(length>0) // null),
      lastRecommendation: ($lastRecommendation | select(length>0) // null),
      hasRecommendation: $hasRecommendation,
      warnings: $warnings,
      current: {
        requests: { cpu: ($curCpuReq | select(length>0) // null), memory: ($curMemReq | select(length>0) // null) },
        limits:   { cpu: ($curCpuLim | select(length>0) // null), memory: ($curMemLim | select(length>0) // null) }
      },
      recommended: {
        requests: { cpu: ($recCpuReq | select(length>0) // null), memory: ($recMemReq | select(length>0) // null) },
        limits:   { cpu: ($recCpuLim | select(length>0) // null), memory: ($recMemLim | select(length>0) // null) }
      },
      currentNumeric: {
        cpuReqMilli: $curCpuReqMilli,
        cpuLimMilli: $curCpuLimMilli,
        memReqBytes: $curMemReqBytes,
        memLimBytes: $curMemLimBytes
      },
      recommendedNumeric: {
        cpuReqMilli: $recCpuReqMilli,
        cpuLimMilli: $recCpuLimMilli,
        memReqBytes: $recMemReqBytes,
        memLimBytes: $recMemLimBytes
      },
      delta: {
        cpuReqMilli: $deltaCpuReqMilli,
        cpuLimMilli: $deltaCpuLimMilli,
        memReqBytes: $deltaMemReqBytes,
        memLimBytes: $deltaMemLimBytes,
        impactCpuReqMilli: $impactCpuReqMilli,
        impactCpuLimMilli: $impactCpuLimMilli,
        impactMemReqBytes: $impactMemReqBytes,
        impactMemLimBytes: $impactMemLimBytes,
        cpuReqDirection: $cpuReqDirection,
        cpuLimDirection: $cpuLimDirection,
        memReqDirection: $memReqDirection,
        memLimDirection: $memLimDirection,
        cpuReqPercent: $cpuReqPercent,
        cpuLimPercent: $cpuLimPercent,
        memReqPercent: $memReqPercent,
        memLimPercent: $memLimPercent
      }
    }' >>"$RECORDS_NDJSON"
done < <(
  jq -r '
    .items[]? as $w
    | ($w.spec.template.spec.containers // [])[]
    | {
        kind: $w.kind,
        namespace: $w.metadata.namespace,
        workload: $w.metadata.name,
        pods: (
          if $w.kind == "Deployment" then ($w.status.replicas // 0)
          elif $w.kind == "StatefulSet" then ($w.status.replicas // 0)
          elif $w.kind == "DaemonSet" then ($w.status.numberReady // ($w.status.desiredNumberScheduled // 0))
          else 0 end
        ),
        policy: ($w.metadata.annotations["optipod.io/policy"] // ""),
        policyUID: ($w.metadata.annotations["optipod.io/policy-uid"] // ""),
        lastRecommendation: ($w.metadata.annotations["optipod.io/last-recommendation"] // ""),
        container: .name,
        curCpuReq: (.resources.requests.cpu // ""),
        curMemReq: (.resources.requests.memory // ""),
        curCpuLim: (.resources.limits.cpu // ""),
        curMemLim: (.resources.limits.memory // ""),
        recCpuReq: ($w.metadata.annotations[("optipod.io/recommendation." + .name + ".cpu-request")] // ""),
        recMemReq: ($w.metadata.annotations[("optipod.io/recommendation." + .name + ".memory-request")] // ""),
        recCpuLim: ($w.metadata.annotations[("optipod.io/recommendation." + .name + ".cpu-limit")] // ""),
        recMemLim: ($w.metadata.annotations[("optipod.io/recommendation." + .name + ".memory-limit")] // "")
      }
    | @base64
  ' "$WORKLOADS_JSON"
)

# Build final records list and compute totals in jq to avoid shell/subshell pitfalls.
RECORDS_JSON="$(jq -s '.' "$RECORDS_NDJSON")"
FILTERED_RECORDS_JSON="$(jq '[.[] | select(.hasRecommendation == true)]' <<<"$RECORDS_JSON")"

# Fetch policy information to check for updateRequestsOnly settings
POLICIES_JSON="$(mktemp)"
trap 'rm -f "$WORKLOADS_JSON" "$RECORDS_NDJSON" "$POLICIES_JSON"' EXIT

# Get all OptimizationPolicy resources
kubectl get optimizationpolicies -A -o json >"$POLICIES_JSON" 2>/dev/null || echo '{"items":[]}' >"$POLICIES_JSON"

# Enhance records with policy information and additional warnings
ENHANCED_RECORDS_JSON="$(jq -n \
  --argjson records "$FILTERED_RECORDS_JSON" \
  --argjson policies "$(cat "$POLICIES_JSON")" '
  # Create a lookup map of policies by name and UID
  ($policies.items // []) as $policyList
  | (reduce $policyList[] as $p ({};
      .[$p.metadata.name] = $p |
      .[$p.metadata.uid] = $p
    )) as $policyMap
  |
  $records | map(
    . as $record
    | ($policyMap[.policy] // $policyMap[.policyUID] // {}) as $policyObj
    | ($policyObj.spec.updateStrategy.updateRequestsOnly // false) as $updateRequestsOnly
    |
    # Add additional warnings for updateRequestsOnly scenarios
    if $updateRequestsOnly and ((.warnings // []) | map(select(. == "CPU_REQ_EXCEEDS_LIMIT" or . == "MEM_REQ_EXCEEDS_LIMIT")) | length > 0) then
      .warnings = (.warnings // []) + ["UPDATE_REQUESTS_ONLY_CONFLICT"]
    else
      .
    end
    |
    # Add policy information
    .policyInfo = {
      updateRequestsOnly: $updateRequestsOnly,
      mode: ($policyObj.spec.mode // null),
      weight: ($policyObj.spec.weight // null)
    }
  )
')"

totals_json="$(jq -n --argjson records "$ENHANCED_RECORDS_JSON" '
  def sum_field($cur; $rec):
    [ $records[]
      | select((getpath($cur) != null) and (getpath($rec) != null))
      | {c: getpath($cur), r: getpath($rec)}
    ] as $xs
    | {
        current: (($xs | map(.c) | add) // 0),
        recommended: (($xs | map(.r) | add) // 0)
      }
    | . + { delta: (.recommended - .current) };

  def sum_field_impact($cur; $rec):
    [ $records[]
      | select((getpath($cur) != null) and (getpath($rec) != null) and (.pods != null) and (.pods > 0))
      | {c: (getpath($cur) * .pods), r: (getpath($rec) * .pods)}
    ] as $xs
    | {
        current: (($xs | map(.c) | add) // 0),
        recommended: (($xs | map(.r) | add) // 0)
      }
    | . + { delta: (.recommended - .current) };

  {
    cpuRequests: (sum_field(["currentNumeric","cpuReqMilli"]; ["recommendedNumeric","cpuReqMilli"]) | {currentMilli: .current, recommendedMilli: .recommended, deltaMilli: .delta}),
    memoryRequests: (sum_field(["currentNumeric","memReqBytes"]; ["recommendedNumeric","memReqBytes"]) | {currentBytes: .current, recommendedBytes: .recommended, deltaBytes: .delta}),
    cpuLimits: (sum_field(["currentNumeric","cpuLimMilli"]; ["recommendedNumeric","cpuLimMilli"]) | {currentMilli: .current, recommendedMilli: .recommended, deltaMilli: .delta}),
    memoryLimits: (sum_field(["currentNumeric","memLimBytes"]; ["recommendedNumeric","memLimBytes"]) | {currentBytes: .current, recommendedBytes: .recommended, deltaBytes: .delta}),
    impact: {
      cpuRequests: (sum_field_impact(["currentNumeric","cpuReqMilli"]; ["recommendedNumeric","cpuReqMilli"]) | {currentMilli: .current, recommendedMilli: .recommended, deltaMilli: .delta}),
      memoryRequests: (sum_field_impact(["currentNumeric","memReqBytes"]; ["recommendedNumeric","memReqBytes"]) | {currentBytes: .current, recommendedBytes: .recommended, deltaBytes: .delta}),
      cpuLimits: (sum_field_impact(["currentNumeric","cpuLimMilli"]; ["recommendedNumeric","cpuLimMilli"]) | {currentMilli: .current, recommendedMilli: .recommended, deltaMilli: .delta}),
      memoryLimits: (sum_field_impact(["currentNumeric","memLimBytes"]; ["recommendedNumeric","memLimBytes"]) | {currentBytes: .current, recommendedBytes: .recommended, deltaBytes: .delta})
    }
  }
')"

REPORT_JSON="$(jq -n \
  --arg generatedAt "$GEN_AT" \
  --arg context "$CTX" \
  --arg namespace "$NAMESPACE" \
  --arg kinds "$KINDS" \
  --argjson scannedWorkloads "$SCANNED_WORKLOADS_COUNT" \
  --argjson scannedNamespaces "$SCANNED_NAMESPACES_COUNT" \
  --argjson totals "$totals_json" \
  --argjson records "$ENHANCED_RECORDS_JSON" \
  '{
    generatedAt: $generatedAt,
    context: ($context | select(length>0) // null),
    namespace: ($namespace | select(length>0) // null),
    kinds: ($kinds | split(",")),
    totals: $totals,
    records: $records,
    counts: {
      scannedWorkloads: $scannedWorkloads,
      scannedNamespaces: $scannedNamespaces,
      workloadsWithRecommendations: ($records | map(.namespace + "/" + .workload + " (" + .kind + ")") | unique | length),
      recordsWithRecommendations: ($records | length),
      warningRecords: ($records | map(select((.warnings // []) | length > 0)) | length)
    }
  }'
)"

render_html() {
  local json="$1"
  local title="OptiPod Recommendation Report"
  local generated; generated="$(jq -r '.generatedAt' <<<"$json")"
  local ctx; ctx="$(jq -r '.context // ""' <<<"$json")"
  local ns; ns="$(jq -r '.namespace // ""' <<<"$json")"

  local scanned_workloads; scanned_workloads="$(jq -r '.counts.scannedWorkloads' <<<"$json")"
  local rec_workloads; rec_workloads="$(jq -r '.counts.workloadsWithRecommendations' <<<"$json")"
  local rec_records; rec_records="$(jq -r '.counts.recordsWithRecommendations' <<<"$json")"
  local warn_records; warn_records="$(jq -r '.counts.warningRecords' <<<"$json")"

  local pct="0.00"
  if [[ -n "$scanned_workloads" && "$scanned_workloads" != "0" ]]; then
    pct="$(awk -v a="$rec_workloads" -v b="$scanned_workloads" 'BEGIN{printf "%.2f", (a/b)*100.0}')"
  fi
  local donut_deg; donut_deg="$(awk -v p="$pct" 'BEGIN{printf "%.0f", (p/100.0)*360.0}')"

  cat <<EOF
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>${title}</title>
    <style>
      :root{
        --bg: #f6f8fb;
        --card: #ffffff;
        --text: #0f172a;
        --muted: #64748b;
        --border: rgba(15,23,42,0.10);
        --shadow: 0 1px 0 rgba(15,23,42,0.04), 0 8px 24px rgba(15,23,42,0.06);
        --green: #16a34a;
        --red: #dc2626;
        --blue: #2563eb;
        --chip: rgba(2,6,23,0.06);
      }
      *{ box-sizing: border-box; }
      body { font-family: system-ui, -apple-system, Segoe UI, Roboto, Ubuntu, Cantarell, Arial, sans-serif; margin: 0; color: var(--text); background: var(--bg); }
      .wrap { width: 100%; max-width: 100%; margin: 0 auto; padding: 22px 18px 40px; }
      header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 16px; }
      h1 { margin: 0; font-size: 34px; letter-spacing: -0.02em; }
      .header-right { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; justify-content: flex-end; }
      .chip { display: inline-flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 10px; background: var(--card); border: 1px solid var(--border); box-shadow: var(--shadow); color: var(--muted); font-size: 13px; }
      .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace; font-size: 0.92em; }

      .topgrid { display: grid; grid-template-columns: 1.2fr 1fr 1.6fr; gap: 14px; margin: 14px 0 14px; }
      .card { background: var(--card); border: 1px solid var(--border); border-radius: 14px; box-shadow: var(--shadow); padding: 14px; }
      .card h2 { margin: 0 0 10px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); }
      .big { font-size: 40px; font-weight: 800; letter-spacing: -0.03em; margin: 2px 0 0; }
      .sub { margin: 6px 0 0; color: var(--muted); font-weight: 600; }
      .row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }

      .donut { width: 86px; height: 86px; border-radius: 999px; background:
        conic-gradient(var(--blue) var(--p), rgba(2,6,23,0.10) 0);
        display:flex; align-items:center; justify-content:center;
      }
      .donut::after{ content:""; width: 62px; height: 62px; background: var(--card); border-radius: 999px; border: 1px solid var(--border); }
      .delta { display: grid; gap: 10px; }
      .delta-item { display:flex; align-items: center; justify-content: space-between; gap: 10px; }
      .delta-item .label { color: var(--muted); font-weight: 700; }
      .delta-item .val { font-weight: 800; font-size: 22px; }
      .down { color: var(--green); }
      .up { color: var(--red); }

      .kv { display: grid; gap: 10px; margin-top: 8px; }
      .kvrow { display:grid; grid-template-columns: 150px 1fr 1fr 1fr; gap: 10px; align-items: baseline; }
      .small { color: var(--muted); font-weight: 700; font-size: 12px; }
      .num { text-align: right; font-weight: 800; }
      .deltaBadge { display:inline-flex; align-items:center; justify-content:center; gap:6px; padding: 2px 8px; border-radius: 999px; font-weight: 900; font-size: 12px; }
      .deltaBadge.dec { background: rgba(22,163,74,0.12); color: #166534; }
      .deltaBadge.inc { background: rgba(220,38,38,0.12); color: #991b1b; }
      .deltaBadge.na { background: rgba(2,6,23,0.06); color: var(--muted); }

      .filters { display:flex; gap: 10px; flex-wrap: wrap; align-items: end; margin: 12px 0 10px; }
      .field { display: grid; gap: 6px; }
      .field label { font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); font-weight: 800; }
      input[type="search"], select {
        background: var(--card);
        border: 1px solid var(--border);
        border-radius: 12px;
        padding: 10px 12px;
        box-shadow: var(--shadow);
        font-size: 14px;
        outline: none;
      }
      input[type="search"] { flex: 1 1 360px; }
      select { flex: 0 0 auto; min-width: 220px; }
      .btn {
        border: 1px solid var(--border);
        background: rgba(2,6,23,0.03);
        color: var(--muted);
        padding: 10px 12px;
        border-radius: 12px;
        cursor: pointer;
        font-weight: 700;
      }

      .tablewrap { background: var(--card); border: 1px solid var(--border); border-radius: 14px; box-shadow: var(--shadow); overflow: visible; }
      table { width: 100%; border-collapse: collapse; table-layout: fixed; }
      thead th { position: sticky; top: 0; z-index: 1; background: rgba(2,6,23,0.03); color: var(--muted); text-transform: uppercase; letter-spacing: 0.08em; font-size: 11px; padding: 12px 12px; border-bottom: 1px solid rgba(2,6,23,0.08); }
      tbody td { padding: 12px 12px; border-bottom: 1px solid rgba(2,6,23,0.06); vertical-align: top; }
      tbody tr:hover { background: rgba(37,99,235,0.04); }
      tbody tr:last-child td { border-bottom: none; }
      .muted { color: var(--muted); font-weight: 600; }
      .pill { display:inline-flex; align-items:center; gap:6px; padding: 3px 9px; border-radius: 999px; font-size: 12px; font-weight: 800; background: var(--chip); color: var(--muted); }
      .pill.dec { background: rgba(22,163,74,0.12); color: #166534; }
      .pill.inc { background: rgba(220,38,38,0.12); color: #991b1b; }
      .pill.na { background: rgba(37,99,235,0.10); color: #1e3a8a; }
      .warn { background: rgba(245,158,11,0.14); color: #92400e; }
      .wbadge { display:inline-flex; align-items:center; justify-content:center; padding: 2px 8px; border-radius: 999px; font-weight: 900; font-size: 12px; background: rgba(245,158,11,0.14); color: #92400e; }
      .wbadge.none { background: rgba(2,6,23,0.06); color: var(--muted); }
      .wbadge[data-tip] { position: relative; cursor: help; }
      .wbadge[data-tip]::after{
        content: attr(data-tip);
        position: absolute;
        left: 50%;
        bottom: calc(100% + 10px);
        transform: translateX(-50%);
        background: rgba(15,23,42,0.92);
        color: #fff;
        padding: 8px 10px;
        border-radius: 10px;
        font-size: 12px;
        font-weight: 700;
        line-height: 1.25;
        white-space: pre-wrap;
        width: max-content;
        max-width: 360px;
        box-shadow: 0 12px 28px rgba(2,6,23,0.22);
        opacity: 0;
        pointer-events: none;
        transition: opacity 120ms ease;
        z-index: 10;
      }
      .wbadge[data-tip]::before{
        content:"";
        position:absolute;
        left:50%;
        bottom: calc(100% + 4px);
        transform: translateX(-50%);
        border: 6px solid transparent;
        border-top-color: rgba(15,23,42,0.92);
        opacity: 0;
        pointer-events: none;
        transition: opacity 120ms ease;
        z-index: 10;
      }
      .wbadge[data-tip]:hover::after,
      .wbadge[data-tip]:hover::before{ opacity: 1; }

      .cell.dec { color: #166534; }
      .cell.inc { color: #991b1b; }
      .cell.na { color: var(--text); }
      .arrow { display:inline-block; min-width: 1ch; text-align: center; font-weight: 900; }
      .unit { color: var(--muted); font-weight: 800; font-size: 12px; margin-left: 6px; }

      .workcell { display:flex; align-items:center; gap: 10px; min-width: 0; }
      .workname { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

      @media (max-width: 980px) {
        .topgrid { grid-template-columns: 1fr; }
        .kvrow { grid-template-columns: 140px 1fr 1fr 1fr; }
      }
    </style>
  </head>
  <body>
    <div class="wrap">
      <header>
        <div>
          <h1>Optimization</h1>
          <div class="muted">OptiPod recommendation summary and workload breakdown.</div>
        </div>
        <div class="header-right">
          <div class="chip">Generated <span class="mono">${generated}</span></div>
EOF
  if [[ -n "$ctx" ]]; then
    echo "          <div class=\"chip\">Context <span class=\"mono\">$(html_escape "$ctx")</span></div>"
  fi
  if [[ -n "$ns" ]]; then
    echo "          <div class=\"chip\">Namespace <span class=\"mono\">$(html_escape "$ns")</span></div>"
  fi
  cat <<EOF
        </div>
      </header>
EOF

  # Summary uses replica-weighted impact totals: delta per-pod * pods
  local cpu_cur cpu_rec cpu_delta
  cpu_cur="$(jq -r '.totals.impact.cpuRequests.currentMilli' <<<"$json")"
  cpu_rec="$(jq -r '.totals.impact.cpuRequests.recommendedMilli' <<<"$json")"
  cpu_delta="$(jq -r '.totals.impact.cpuRequests.deltaMilli' <<<"$json")"

  local mem_cur mem_rec mem_delta
  mem_cur="$(jq -r '.totals.impact.memoryRequests.currentBytes' <<<"$json")"
  mem_rec="$(jq -r '.totals.impact.memoryRequests.recommendedBytes' <<<"$json")"
  mem_delta="$(jq -r '.totals.impact.memoryRequests.deltaBytes' <<<"$json")"

  local cpu_delta_abs_m; cpu_delta_abs_m="$(abs_number "$cpu_delta")"
  local mem_delta_abs_b; mem_delta_abs_b="$(abs_number "$mem_delta")"

  local cpu_delta_class="down"
  local mem_delta_class="down"
  if awk -v v="$cpu_delta" 'BEGIN{exit !(v>0)}'; then cpu_delta_class="up"; fi
  if awk -v v="$mem_delta" 'BEGIN{exit !(v>0)}'; then mem_delta_class="up"; fi

  local cpu_arrow; cpu_arrow="$(arrow_for_dir "$(direction "${cpu_delta:-}")")"
  local mem_arrow; mem_arrow="$(arrow_for_dir "$(direction "${mem_delta:-}")")"

  local cpu_ratio="0"
  local mem_ratio="0"
  if [[ "$cpu_cur" != "0" ]] && [[ "$cpu_cur" != "null" ]]; then
    cpu_ratio="$(awk -v r="$cpu_rec" -v c="$cpu_cur" 'BEGIN{ if (c==0) v=0; else v=(r/c); if (v>1) v=1; if (v<0) v=0; printf "%.4f", v }')"
  fi
  if [[ "$mem_cur" != "0" ]] && [[ "$mem_cur" != "null" ]]; then
    mem_ratio="$(awk -v r="$mem_rec" -v c="$mem_cur" 'BEGIN{ if (c==0) v=0; else v=(r/c); if (v>1) v=1; if (v<0) v=0; printf "%.4f", v }')"
  fi

  local cpu_delta_dir; cpu_delta_dir="$(direction "${cpu_delta:-}")"
  local mem_delta_dir; mem_delta_dir="$(direction "${mem_delta:-}")"
  local cpu_badge_cls="na"
  local mem_badge_cls="na"
  [[ "$cpu_delta_dir" == "increase" ]] && cpu_badge_cls="inc"
  [[ "$cpu_delta_dir" == "decrease" ]] && cpu_badge_cls="dec"
  [[ "$mem_delta_dir" == "increase" ]] && mem_badge_cls="inc"
  [[ "$mem_delta_dir" == "decrease" ]] && mem_badge_cls="dec"

  cat <<EOF
      <div class="topgrid">
        <div class="card">
          <h2>Workloads Optimized</h2>
          <div class="row">
            <div class="donut" style="--p:${donut_deg}deg"></div>
            <div style="flex:1">
              <div class="big">${pct}%</div>
              <div class="sub"><span class="mono">${rec_workloads}</span>/<span class="mono">${scanned_workloads}</span> workloads · <span class="mono">${rec_records}</span> container rows</div>
              <div class="sub">Warnings: <span class="mono">${warn_records}</span></div>
            </div>
          </div>
        </div>

        <div class="card">
          <h2>Difference (Requests)</h2>
          <div class="delta">
            <div class="delta-item">
              <div class="label">CPU</div>
              <div class="val ${cpu_delta_class}"><span class="mono">${cpu_arrow} $(fmt_cpu_cores "$cpu_delta_abs_m")</span> cores</div>
            </div>
            <div class="delta-item">
              <div class="label">Memory</div>
              <div class="val ${mem_delta_class}"><span class="mono">${mem_arrow} $(fmt_mem_gi "$mem_delta_abs_b")</span></div>
            </div>
          </div>
        </div>

        <div class="card">
          <h2>Requests Summary</h2>
          <div class="kv">
            <div class="kvrow">
              <div class="small">CPU (cores)</div>
              <div class="small num">Recommended</div>
              <div class="small num">Current</div>
              <div class="small num">Δ</div>
            </div>
            <div class="kvrow">
              <div class="mono">CPU</div>
              <div class="num mono">$(fmt_cpu_cores "$cpu_rec")</div>
              <div class="num mono">$(fmt_cpu_cores "$cpu_cur")</div>
              <div class="num"><span class="deltaBadge ${cpu_badge_cls}">$(arrow_for_delta "$cpu_delta") $(fmt_cpu_cores "$cpu_delta_abs_m")</span></div>
            </div>
            <div class="kvrow">
              <div class="mono">Memory (GiB)</div>
              <div class="num mono">$(fmt_mem_gi "$mem_rec")</div>
              <div class="num mono">$(fmt_mem_gi "$mem_cur")</div>
              <div class="num"><span class="deltaBadge ${mem_badge_cls}">$(arrow_for_delta "$mem_delta") $(fmt_mem_gi "$mem_delta_abs_b")</span></div>
            </div>
          </div>
        </div>
      </div>

      <div class="filters">
        <div class="field" style="flex:1 1 360px;">
          <label for="q">Search</label>
          <input id="q" type="search" placeholder="Workload, container, policy…" />
        </div>
        <div class="field">
          <label for="nsFilter">Namespace</label>
          <select id="nsFilter"><option value="">All namespaces</option></select>
        </div>
        <div class="field">
          <label for="kindFilter">Type</label>
          <select id="kindFilter"><option value="">All kinds</option></select>
        </div>
        <div class="field">
          <label for="optFilter">Change</label>
          <select id="optFilter">
            <option value="">Any</option>
            <option value="decrease">Decreases</option>
            <option value="increase">Increases</option>
          </select>
        </div>
        <div class="field">
          <label for="warnFilter">Warnings</label>
          <select id="warnFilter">
            <option value="">All</option>
            <option value="warn">Only warnings</option>
          </select>
        </div>
        <div class="field">
          <label for="sortBy">Sort</label>
          <select id="sortBy">
            <option value="savings" selected>Largest total decrease</option>
            <option value="cpu">CPU Δ (total)</option>
            <option value="mem">Memory Δ (total)</option>
            <option value="warn">Warnings</option>
          </select>
        </div>
        <button class="btn" id="clear" style="height:42px;">Clear</button>
      </div>

      <div class="tablewrap">
        <table>
          <colgroup>
            <col style="width: 240px;">
            <col style="width: 120px;">
            <col style="width: 110px;">
            <col style="width: 70px;">
            <col style="width: 120px;">
            <col style="width: 170px;">
            <col style="width: 180px;">
            <col style="width: 130px;">
            <col style="width: 180px;">
            <col style="width: 130px;">
            <col style="width: 120px;">
            <col style="width: 160px;">
          </colgroup>
          <thead>
            <tr>
              <th>Workload</th>
              <th>Namespace</th>
              <th>Type</th>
              <th>Pods</th>
              <th>Container</th>
              <th>Policy</th>
              <th>CPU req</th>
              <th>CPU Δ</th>
              <th>Mem req</th>
              <th>Mem Δ</th>
              <th>Warnings</th>
              <th>Last rec</th>
            </tr>
          </thead>
          <tbody id="rows">
EOF

  jq -r '
    .records
    | sort_by(.namespace, .workload, .kind, .container)
    | .[]
    | [
        (.namespace + "/" + .workload),
        .namespace,
        .kind,
        (.pods | tostring),
        .container,
        ((.policy // "—") + (if .policyUID then " (" + (.policyUID[:8]) + "…)" else "" end)),
        (.current.requests.cpu // "—"), (.recommended.requests.cpu // "—"),
        ((.currentNumeric.cpuReqMilli // "")|tostring), ((.recommendedNumeric.cpuReqMilli // "")|tostring),
        ((.delta.impactCpuReqMilli // "")|tostring),
        (.current.requests.memory // "—"), (.recommended.requests.memory // "—"),
        ((.currentNumeric.memReqBytes // "")|tostring), ((.recommendedNumeric.memReqBytes // "")|tostring),
        ((.delta.impactMemReqBytes // "")|tostring),
        ((.warnings // []) | length | tostring),
        (.warnings // [] | map(
          if . == "CPU_REQ_EXCEEDS_LIMIT" then "CPU req > limit"
          elif . == "MEM_REQ_EXCEEDS_LIMIT" then "Mem req > limit"
          elif . == "UPDATE_REQUESTS_ONLY_CONFLICT" then "updateRequestsOnly conflict"
          else .
          end
        ) | join(", ")),
        (.lastRecommendation // "—")
      ]
    | @tsv
  ' <<<"$json" | while IFS=$'\t' read -r workload ns kind pods container policy ccur crec ccur_m crec_m cimpact mcur mrec mcur_b mrec_b mimpact warn_count warn_text lastRec; do

    local cpu_cur_cores="—"
    local cpu_rec_cores="—"
    local mem_cur_gi="—"
    local mem_rec_gi="—"
    local cpu_delta_m="$cimpact"
    local mem_delta_b="$mimpact"

    [[ -n "$ccur_m" && "$ccur_m" != "null" ]] && cpu_cur_cores="$(fmt_cpu_cores "$ccur_m")"
    [[ -n "$crec_m" && "$crec_m" != "null" ]] && cpu_rec_cores="$(fmt_cpu_cores "$crec_m")"
    [[ -n "$mcur_b" && "$mcur_b" != "null" ]] && mem_cur_gi="$(fmt_mem_gi "$mcur_b")"
    [[ -n "$mrec_b" && "$mrec_b" != "null" ]] && mem_rec_gi="$(fmt_mem_gi "$mrec_b")"

    local cpu_dir; cpu_dir="$(direction "${cpu_delta_m:-}")"
    local mem_dir; mem_dir="$(direction "${mem_delta_b:-}")"
    local cpu_arrow; cpu_arrow="$(arrow_for_dir "$cpu_dir")"
    local mem_arrow; mem_arrow="$(arrow_for_dir "$mem_dir")"
    local cpu_cell_cls="na"
    local mem_cell_cls="na"
    [[ "$cpu_dir" == "decrease" ]] && cpu_cell_cls="dec"
    [[ "$cpu_dir" == "increase" ]] && cpu_cell_cls="inc"
    [[ "$mem_dir" == "decrease" ]] && mem_cell_cls="dec"
    [[ "$mem_dir" == "increase" ]] && mem_cell_cls="inc"

    local cpu_delta_abs_m; cpu_delta_abs_m="$(abs_number "$cpu_delta_m")"
    local mem_delta_abs_b; mem_delta_abs_b="$(abs_number "$mem_delta_b")"
    local cpu_delta_cores="—"
    local mem_delta_gi="—"
    [[ -n "$cpu_delta_abs_m" && "$cpu_delta_abs_m" != "0" ]] && cpu_delta_cores="$(fmt_cpu_cores "$cpu_delta_abs_m")"
    [[ -n "$mem_delta_abs_b" && "$mem_delta_abs_b" != "0" ]] && mem_delta_gi="$(fmt_mem_gi "$mem_delta_abs_b")"

    local warn_cell="—"
    local warn_flag=""
    if [[ "${warn_count:-0}" != "0" ]]; then
      local wlabel="warning"
      [[ "$warn_count" != "1" ]] && wlabel="warnings"
      warn_cell="<span class=\"wbadge\" data-tip=\"$(html_escape "$warn_text")\">$(html_escape "$warn_count") ${wlabel}</span>"
      warn_flag="warn"
    else
      warn_cell="<span class=\"wbadge none\">0</span>"
    fi

    echo "        <tr class=\"main\">"
    echo "          <td>"
    echo "            <div class=\"workcell\">"
    echo "              <span class=\"mono workname\" title=\"$(html_escape "$workload")\">$(html_escape "$workload")</span>"
    echo "            </div>"
    echo "          </td>"
    echo "          <td class=\"mono\">$(html_escape "$ns")</td>"
    echo "          <td class=\"mono\">$(html_escape "$kind")</td>"
    echo "          <td class=\"mono\">$(html_escape "$pods")</td>"
    echo "          <td class=\"mono\">$(html_escape "$container")</td>"
    echo "          <td class=\"mono\">$(html_escape "$policy")</td>"
    echo "          <td class=\"mono cell ${cpu_cell_cls}\" data-text=\"$(html_escape "$workload $ns $kind $pods $container $policy")\" data-ns=\"$(html_escape "$ns")\" data-kind=\"$(html_escape "$kind")\" data-opt=\"$(html_escape "$cpu_dir")\" data-warn=\"$warn_flag\" data-cpu-delta-m=\"$(html_escape "${cpu_delta_m:-0}")\" data-mem-delta-b=\"$(html_escape "${mem_delta_b:-0}")\" data-warn-count=\"$(html_escape "${warn_count:-0}")\">"
    echo "            <span title=\"Current: $(html_escape "$ccur") · Recommended: $(html_escape "$crec")\">$(html_escape "$cpu_cur_cores") → $(html_escape "$cpu_rec_cores")</span> <span class=\"arrow\">$(html_escape "$cpu_arrow")</span>"
    echo "          </td>"
    echo "          <td class=\"mono cell ${cpu_cell_cls}\"><span class=\"arrow\">$(html_escape "$(arrow_for_delta "$cpu_delta_m")")</span> $(html_escape "$cpu_delta_cores")<span class=\"unit\">cores</span></td>"
    echo "          <td class=\"mono cell ${mem_cell_cls}\">"
    echo "            <span title=\"Current: $(html_escape "$mcur") · Recommended: $(html_escape "$mrec")\">$(html_escape "$mem_cur_gi") → $(html_escape "$mem_rec_gi")</span> <span class=\"arrow\">$(html_escape "$mem_arrow")</span>"
    echo "          </td>"
    echo "          <td class=\"mono cell ${mem_cell_cls}\"><span class=\"arrow\">$(html_escape "$(arrow_for_delta "$mem_delta_b")")</span> $(html_escape "$mem_delta_gi")</td>"
    echo "          <td>$warn_cell</td>"
    echo "          <td class=\"mono muted\">$(html_escape "$lastRec")</td>"
    echo "        </tr>"
  done

  cat <<'EOF'
          </tbody>
        </table>
      </div>

      <script>
        (function () {
          const q = document.getElementById('q');
          const nsFilter = document.getElementById('nsFilter');
          const kindFilter = document.getElementById('kindFilter');
          const optFilter = document.getElementById('optFilter');
          const warnFilter = document.getElementById('warnFilter');
          const sortBy = document.getElementById('sortBy');
          const clear = document.getElementById('clear');
          const tbody = document.getElementById('rows');
          const mainRows = Array.from(document.querySelectorAll('#rows tr.main'));
          const pairs = mainRows.map(r => ({ main: r }));

          function uniq(values) { return Array.from(new Set(values)).sort(); }

          const namespaces = uniq(pairs.map(p => p.main.querySelector('[data-ns]')?.getAttribute('data-ns')).filter(Boolean));
          const kinds = uniq(pairs.map(p => p.main.querySelector('[data-kind]')?.getAttribute('data-kind')).filter(Boolean));

          namespaces.forEach(v => { const o = document.createElement('option'); o.value = v; o.textContent = v; nsFilter.appendChild(o); });
          kinds.forEach(v => { const o = document.createElement('option'); o.value = v; o.textContent = v; kindFilter.appendChild(o); });

          function sortPairs(list) {
            const mode = sortBy.value || 'savings';
            const getNum = (el, attr) => Number(el.querySelector('[data-text]')?.getAttribute(attr) || 0);
            const getWarn = (el) => Number(el.querySelector('[data-text]')?.getAttribute('data-warn-count') || 0);

            const cmpNumAsc = (a, b, get) => (get(a) - get(b));
            const cmpNumDesc = (a, b, get) => (get(b) - get(a));

            if (mode === 'cpu') {
              return list.sort((a, b) => cmpNumAsc(a.main, b.main, el => getNum(el, 'data-cpu-delta-m')));
            }
            if (mode === 'mem') {
              return list.sort((a, b) => cmpNumAsc(a.main, b.main, el => getNum(el, 'data-mem-delta-b')));
            }
            if (mode === 'warn') {
              return list.sort((a, b) => cmpNumDesc(a.main, b.main, el => getWarn(el)));
            }

            // Default: "largest total decrease" => most negative memory delta first, then most negative CPU delta.
            return list.sort((a, b) => {
              const am = getNum(a.main, 'data-mem-delta-b');
              const bm = getNum(b.main, 'data-mem-delta-b');
              const ac = getNum(a.main, 'data-cpu-delta-m');
              const bc = getNum(b.main, 'data-cpu-delta-m');
              if (am !== bm) return am - bm;
              if (ac !== bc) return ac - bc;
              return getWarn(b.main) - getWarn(a.main);
            });
          }

          function apply() {
            const query = (q.value || '').toLowerCase().trim();
            const ns = nsFilter.value;
            const kind = kindFilter.value;
            const opt = optFilter.value;
            const warn = warnFilter.value;

            const visible = [];
            pairs.forEach(p => {
              const cell = p.main.querySelector('[data-text]');
              const text = (cell?.getAttribute('data-text') || '').toLowerCase();
              const rns = cell?.getAttribute('data-ns') || '';
              const rkind = cell?.getAttribute('data-kind') || '';
              const ropt = cell?.getAttribute('data-opt') || '';
              const rwarn = cell?.getAttribute('data-warn') || '';
              const ok =
                (!query || text.includes(query)) &&
                (!ns || rns === ns) &&
                (!kind || rkind === kind) &&
                (!opt || ropt === opt) &&
                (!warn || rwarn === 'warn');
              p.main.style.display = ok ? '' : 'none';
              if (ok) visible.push(p);
            });

            const sorted = sortPairs(visible);
            sorted.forEach(p => { tbody.appendChild(p.main); });
          }

          [q, nsFilter, kindFilter, optFilter, warnFilter, sortBy].forEach(el => el.addEventListener('input', apply));
          clear.addEventListener('click', () => {
            q.value = '';
            nsFilter.value = '';
            kindFilter.value = '';
            optFilter.value = '';
            warnFilter.value = '';
            sortBy.value = 'savings';
            apply();
          });

          apply();
        })();
      </script>
    </div>
  </body>
</html>
EOF
}

OUTPUT_DATA=""
case "$OUTPUT" in
  json) OUTPUT_DATA="$REPORT_JSON";;
  html) OUTPUT_DATA="$(render_html "$REPORT_JSON")";;
esac

if [[ -n "$OUT_FILE" ]]; then
  printf '%s\n' "$OUTPUT_DATA" >"$OUT_FILE"
else
  printf '%s\n' "$OUTPUT_DATA"
fi

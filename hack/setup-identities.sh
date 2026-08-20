#!/usr/bin/env bash
# Mirrors SafeLane's two production identities on the local kind cluster.
#
# Safe to run twice: RBAC and token Secrets are declarative, the preserved
# admin context is reused, and both generated ServiceAccount kubeconfigs are
# replaced atomically. The caller becomes the default context; the controller
# context is written only to project.yml's controller_kubeconfig path.
set -euo pipefail

NAMESPACE=podinfo
CALLER=safelane-caller
CONTROLLER=safelane-controller
ADMIN_CONTEXT=safelane-admin
DEFAULT_KUBECONFIG="${HOME:?HOME is required}/.kube/config"
SAFELANE_CONFIG_HOME="${SAFELANE_HOME:-${HOME}/.safelane}"
PROJECT_FILE="${SAFELANE_PROJECT_FILE:-${SAFELANE_CONFIG_HOME}/apps/podinfo/project.yml}"

yaml_scalar() {
  local key="$1"
  awk -v key="${key}" '
    $0 ~ "^[[:space:]]*" key ":[[:space:]]*" {
      sub("^[[:space:]]*" key ":[[:space:]]*", "")
      sub(/[[:space:]]*#.*/, "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      quote = sprintf("%c", 39)
      first = substr($0, 1, 1)
      last = substr($0, length($0), 1)
      if ((first == "\"" && last == "\"") || (first == quote && last == quote)) {
        $0 = substr($0, 2, length($0) - 2)
      }
      print
      exit
    }
  ' "${PROJECT_FILE}"
}

context_exists() {
  kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config get-contexts -o name |
    grep -Fxq -- "$1"
}

if [[ ! -f "${DEFAULT_KUBECONFIG}" ]]; then
  echo "Refusing to continue: ${DEFAULT_KUBECONFIG} is not a regular file." >&2
  exit 1
fi
if [[ -L "${DEFAULT_KUBECONFIG}" ]]; then
  echo "Refusing to mutate symlinked kubeconfig ${DEFAULT_KUBECONFIG}." >&2
  exit 1
fi
if [[ ! -f "${PROJECT_FILE}" ]]; then
  echo "Refusing to continue: project config not found at ${PROJECT_FILE}." >&2
  exit 1
fi

CONTROLLER_KUBECONFIG_VALUE="$(yaml_scalar controller_kubeconfig)"
CONTROLLER_CONTEXT="$(yaml_scalar controller_context)"
if [[ -z "${CONTROLLER_KUBECONFIG_VALUE}" || -z "${CONTROLLER_CONTEXT}" ]]; then
  echo "project.yml must set controller_kubeconfig and controller_context." >&2
  exit 1
fi
if [[ "${CONTROLLER_CONTEXT}" != "${CONTROLLER}" ]]; then
  echo "Refusing unexpected controller context ${CONTROLLER_CONTEXT}; want ${CONTROLLER}." >&2
  exit 1
fi

PROJECT_DIR="$(cd "$(dirname "${PROJECT_FILE}")" && pwd -P)"
if [[ "${CONTROLLER_KUBECONFIG_VALUE}" = /* ]]; then
  CONTROLLER_KUBECONFIG="${CONTROLLER_KUBECONFIG_VALUE}"
else
  CONTROLLER_KUBECONFIG="${PROJECT_DIR}/${CONTROLLER_KUBECONFIG_VALUE}"
fi
mkdir -p "$(dirname "${CONTROLLER_KUBECONFIG}")"

BACKUP="${DEFAULT_KUBECONFIG}.safelane-backup.$(date -u +%Y%m%dT%H%M%SZ)"
suffix=0
while [[ -e "${BACKUP}" ]]; do
  suffix=$((suffix + 1))
  BACKUP="${DEFAULT_KUBECONFIG}.safelane-backup.$(date -u +%Y%m%dT%H%M%SZ).${suffix}"
done
cp -p "${DEFAULT_KUBECONFIG}" "${BACKUP}"
echo "Backed up kubeconfig to ${BACKUP}"

if context_exists "${ADMIN_CONTEXT}"; then
  : # Re-run: the original kind administrator was already preserved.
else
  CURRENT_CONTEXT="$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config current-context)"
  case "${CURRENT_CONTEXT}" in
    ""|"${CALLER}"|"${CONTROLLER}")
      echo "No recoverable administrator context exists; restore ${BACKUP} and retry." >&2
      exit 1
      ;;
  esac
  kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config rename-context \
    "${CURRENT_CONTEXT}" "${ADMIN_CONTEXT}" >/dev/null
fi

CLUSTER_NAME="$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config view \
  -o "jsonpath={.contexts[?(@.name=='${ADMIN_CONTEXT}')].context.cluster}")"
if [[ "${CLUSTER_NAME}" != kind-* ]]; then
  echo "Refusing non-kind admin context ${ADMIN_CONTEXT} (cluster ${CLUSTER_NAME:-<empty>})." >&2
  exit 1
fi
if [[ "$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" --context "${ADMIN_CONTEXT}" auth can-i '*' '*')" != "yes" ]]; then
  echo "Context ${ADMIN_CONTEXT} is not cluster-admin; restore ${BACKUP} and retry." >&2
  exit 1
fi

kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" --context "${ADMIN_CONTEXT}" apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${CALLER}
  namespace: ${NAMESPACE}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${CALLER}-token
  namespace: ${NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: ${CALLER}
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: ${CALLER}
  namespace: ${NAMESPACE}
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["rollouts"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ${CALLER}
  namespace: ${NAMESPACE}
subjects:
  - kind: ServiceAccount
    name: ${CALLER}
    namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: ${CALLER}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${CONTROLLER}
  namespace: ${NAMESPACE}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${CONTROLLER}-token
  namespace: ${NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: ${CONTROLLER}
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: ${CONTROLLER}
  namespace: ${NAMESPACE}
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["rollouts", "rollouts/status"]
    verbs: ["get", "patch"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "create", "update", "patch"]
  - apiGroups: ["argoproj.io"]
    resources: ["analysistemplates"]
    verbs: ["get", "create", "update", "patch"]
  - apiGroups: ["argoproj.io"]
    resources: ["analysisruns"]
    verbs: ["get"]
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "create", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ${CONTROLLER}
  namespace: ${NAMESPACE}
subjects:
  - kind: ServiceAccount
    name: ${CONTROLLER}
    namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: ${CONTROLLER}
EOF

read_token() {
  local secret="$1"
  local encoded=""
  local attempt
  for attempt in $(seq 1 30); do
    encoded="$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" --context "${ADMIN_CONTEXT}" \
      get secret "${secret}" -n "${NAMESPACE}" -o jsonpath='{.data.token}')"
    if [[ -n "${encoded}" ]]; then
      printf '%s' "${encoded}" | base64 --decode
      return 0
    fi
    sleep 1
  done
  echo "Timed out waiting for token Secret ${secret}." >&2
  return 1
}

CALLER_TOKEN="$(read_token "${CALLER}-token")"
CONTROLLER_TOKEN="$(read_token "${CONTROLLER}-token")"
SERVER="$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" --context "${ADMIN_CONTEXT}" \
  config view --raw --flatten --minify -o jsonpath='{.clusters[0].cluster.server}')"
CA_DATA="$(kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" --context "${ADMIN_CONTEXT}" \
  config view --raw --flatten --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
if [[ -z "${SERVER}" || -z "${CA_DATA}" ]]; then
  echo "Could not read the kind cluster server and embedded CA data." >&2
  exit 1
fi

TEMP_DIR="$(mktemp -d)"
cleanup() {
  if [[ -n "${TEMP_DIR:-}" && -d "${TEMP_DIR}" ]]; then
    rm -f -- "${TEMP_DIR}/ca.crt"
    rmdir -- "${TEMP_DIR}"
  fi
}
trap cleanup EXIT
printf '%s' "${CA_DATA}" | base64 --decode >"${TEMP_DIR}/ca.crt"
TEMP_CONTROLLER="$(mktemp "$(dirname "${CONTROLLER_KUBECONFIG}")/.controller.kubeconfig.XXXXXX")"

kubectl --kubeconfig "${TEMP_CONTROLLER}" config set-cluster "${CLUSTER_NAME}" \
  --server="${SERVER}" --certificate-authority="${TEMP_DIR}/ca.crt" --embed-certs=true >/dev/null
kubectl --kubeconfig "${TEMP_CONTROLLER}" config set-credentials "${CONTROLLER}" \
  --token="${CONTROLLER_TOKEN}" >/dev/null
kubectl --kubeconfig "${TEMP_CONTROLLER}" config set-context "${CONTROLLER}" \
  --cluster="${CLUSTER_NAME}" --user="${CONTROLLER}" --namespace="${NAMESPACE}" >/dev/null
kubectl --kubeconfig "${TEMP_CONTROLLER}" config use-context "${CONTROLLER}" >/dev/null
chmod 600 "${TEMP_CONTROLLER}"
mv -f "${TEMP_CONTROLLER}" "${CONTROLLER_KUBECONFIG}"

# The privileged controller context must never be available through the
# ambient kubeconfig. The timestamped backup above keeps any prior entry
# recoverable while the active file is made safe for the caller.
if context_exists "${CONTROLLER}"; then
  kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config delete-context "${CONTROLLER}" >/dev/null
fi
kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config unset "users.${CONTROLLER}" >/dev/null 2>&1 || true
kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config set-credentials "${CALLER}" \
  --token="${CALLER_TOKEN}" >/dev/null
kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config set-context "${CALLER}" \
  --cluster="${CLUSTER_NAME}" --user="${CALLER}" --namespace="${NAMESPACE}" >/dev/null
kubectl --kubeconfig "${DEFAULT_KUBECONFIG}" config use-context "${CALLER}" >/dev/null
chmod 600 "${DEFAULT_KUBECONFIG}"

if context_exists "${CONTROLLER}"; then
  echo "Controller context unexpectedly remains in ${DEFAULT_KUBECONFIG}." >&2
  exit 1
fi

echo "Caller context ${CALLER} is now active in ${DEFAULT_KUBECONFIG}."
echo "Admin context remains recoverable as ${ADMIN_CONTEXT}."
echo "Controller kubeconfig written to ${CONTROLLER_KUBECONFIG}."

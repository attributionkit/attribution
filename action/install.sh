#!/usr/bin/env bash
set -euo pipefail

: "${ATTRIBUTION_VERSION:?ATTRIBUTION_VERSION is required}"
: "${RUNNER_OS:?RUNNER_OS is required}"
: "${RUNNER_ARCH:?RUNNER_ARCH is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_PATH:?GITHUB_PATH is required}"
: "${GH_TOKEN:?GH_TOKEN is required for release provenance verification}"

if [[ ! "${ATTRIBUTION_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Invalid AttributionKit release tag: ${ATTRIBUTION_VERSION}" >&2
  exit 2
fi

case "${RUNNER_OS}-${RUNNER_ARCH}" in
  Linux-X64) artifact="attribution-linux-amd64.tar.gz" ;;
  Linux-ARM64) artifact="attribution-linux-arm64.tar.gz" ;;
  macOS-X64) artifact="attribution-darwin-amd64.tar.gz" ;;
  macOS-ARM64) artifact="attribution-darwin-arm64.tar.gz" ;;
  *) echo "Unsupported runner ${RUNNER_OS}-${RUNNER_ARCH}" >&2; exit 2 ;;
esac

base="https://github.com/attributionkit/attribution/releases/download/${ATTRIBUTION_VERSION}"
download_dir="${RUNNER_TEMP}/attributionkit-${ATTRIBUTION_VERSION}"
install_dir="${RUNNER_TEMP}/attributionkit/bin"
mkdir -p "${download_dir}" "${install_dir}"

curl --fail --location --silent --show-error --proto '=https' \
  "${base}/${artifact}" -o "${download_dir}/${artifact}"
curl --fail --location --silent --show-error --proto '=https' \
  "${base}/checksums.txt" -o "${download_dir}/checksums.txt"

expected_checksum="$(awk -v artifact="${artifact}" '$2 == artifact { print $1 }' "${download_dir}/checksums.txt")"
if [[ ! "${expected_checksum}" =~ ^[0-9a-fA-F]{64}$ ]]; then
  echo "No valid SHA-256 checksum found for ${artifact}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "${download_dir}/${artifact}" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "${download_dir}/${artifact}" | awk '{ print $1 }')"
else
  echo "A SHA-256 implementation (sha256sum or shasum) is required" >&2
  exit 1
fi

if [[ "${actual_checksum}" != "${expected_checksum}" ]]; then
  echo "SHA-256 checksum mismatch for ${artifact}" >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "GitHub CLI is required for release provenance verification" >&2
  exit 1
fi
gh attestation verify "${download_dir}/${artifact}" \
  --repo attributionkit/attribution \
  --signer-workflow github.com/attributionkit/attribution/.github/workflows/release.yml \
  --source-ref "refs/tags/${ATTRIBUTION_VERSION}" \
  --deny-self-hosted-runners >/dev/null

archive_entries="$(tar -tzf "${download_dir}/${artifact}" | sed 's#^\./##' | LC_ALL=C sort)"
expected_entries=$'LICENSE\nTHIRD_PARTY_NOTICES\nattribution'
if [[ "${archive_entries}" != "${expected_entries}" ]]; then
  echo "Unexpected files in ${artifact}; refusing to extract" >&2
  exit 1
fi

archive_types="$(tar -tvzf "${download_dir}/${artifact}" | awk '{ print substr($1, 1, 1), $NF }' | LC_ALL=C sort)"
expected_types=$'- LICENSE\n- THIRD_PARTY_NOTICES\n- attribution'
if [[ "${archive_types}" != "${expected_types}" ]]; then
  echo "Non-regular files in ${artifact}; refusing to extract" >&2
  exit 1
fi

tar -xzf "${download_dir}/${artifact}" -C "${install_dir}"
chmod +x "${install_dir}/attribution"
printf '%s\n' "${install_dir}" >> "${GITHUB_PATH}"

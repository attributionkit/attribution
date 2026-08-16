#!/usr/bin/env bash
set -euo pipefail

: "${RUNNER_OS:?RUNNER_OS is required}"
: "${RUNNER_ARCH:?RUNNER_ARCH is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"

case "${RUNNER_OS}-${RUNNER_ARCH}" in
  Linux-X64) artifact="attribution-linux-amd64.tar.gz" ;;
  Linux-ARM64) artifact="attribution-linux-arm64.tar.gz" ;;
  macOS-X64) artifact="attribution-darwin-amd64.tar.gz" ;;
  macOS-ARM64) artifact="attribution-darwin-arm64.tar.gz" ;;
  *) echo "Unsupported runner ${RUNNER_OS}-${RUNNER_ARCH}" >&2; exit 2 ;;
esac

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "${script_dir}/.." && pwd)"
fixture_root="$(mktemp -d "${RUNNER_TEMP}/attributionkit-install-test.XXXXXX")"
release_dir="${fixture_root}/release"
stage_dir="${fixture_root}/stage"
fake_bin="${fixture_root}/fake-bin"
mkdir -p "${release_dir}" "${stage_dir}" "${fake_bin}"

cp "${repository_root}/LICENSE" "${stage_dir}/LICENSE"
cp "${repository_root}/THIRD_PARTY_NOTICES" "${stage_dir}/THIRD_PARTY_NOTICES"
printf '#!/usr/bin/env bash\nprintf "fixture-cli\\n"\n' > "${stage_dir}/attribution"
chmod +x "${stage_dir}/attribution"
tar -czf "${release_dir}/${artifact}" -C "${stage_dir}" LICENSE THIRD_PARTY_NOTICES attribution

if command -v sha256sum >/dev/null 2>&1; then
  checksum="$(sha256sum "${release_dir}/${artifact}" | awk '{ print $1 }')"
else
  checksum="$(shasum -a 256 "${release_dir}/${artifact}" | awk '{ print $1 }')"
fi
printf '%s  %s\n' "${checksum}" "${artifact}" > "${release_dir}/checksums.txt"

# The single-quoted lines below intentionally defer expansion to the fake curl process.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'url=""' \
  'output=""' \
  'while [[ $# -gt 0 ]]; do' \
  '  case "$1" in' \
  '    -o) output="$2"; shift 2 ;;' \
  '    --proto) shift 2 ;;' \
  '    http*) url="$1"; shift ;;' \
  '    *) shift ;;' \
  '  esac' \
  'done' \
  'cp "${FAKE_RELEASE_DIR}/$(basename "${url}")" "${output}"' \
  > "${fake_bin}/curl"
chmod +x "${fake_bin}/curl"

# The single-quoted lines below intentionally defer expansion to the fake gh process.
# shellcheck disable=SC2016
printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'test "$1" = "attestation"' \
  'test "$2" = "verify"' \
  '[[ "$*" == *"--repo attributionkit/attribution"* ]]' \
  '[[ "$*" == *"--signer-workflow github.com/attributionkit/attribution/.github/workflows/release.yml"* ]]' \
  '[[ "$*" == *"--source-ref refs/tags/${ATTRIBUTION_VERSION}"* ]]' \
  '[[ "$*" == *"--deny-self-hosted-runners"* ]]' \
  '[[ "$*" != *"--no-public-good"* ]]' \
  'if [[ "${FAKE_GH_EXIT:-0}" != "0" ]]; then echo "fixture provenance failure" >&2; exit "${FAKE_GH_EXIT}"; fi' \
  > "${fake_bin}/gh"
chmod +x "${fake_bin}/gh"

export PATH="${fake_bin}:${PATH}"
export FAKE_RELEASE_DIR="${release_dir}"
export ATTRIBUTION_VERSION="v0.1.0-preview.4"
export GH_TOKEN="fixture-token"
export GITHUB_PATH="${fixture_root}/github-path"
"${script_dir}/install.sh"

test -x "${RUNNER_TEMP}/attributionkit/bin/attribution"
test "$("${RUNNER_TEMP}/attributionkit/bin/attribution")" = "fixture-cli"
grep -Fx "${RUNNER_TEMP}/attributionkit/bin" "${GITHUB_PATH}"

attestation_runner_temp="${fixture_root}/bad-attestation-runner"
mkdir -p "${attestation_runner_temp}"
if attestation_output="$(
  FAKE_GH_EXIT=1 \
    RUNNER_TEMP="${attestation_runner_temp}" \
    GITHUB_PATH="${fixture_root}/bad-attestation-github-path" \
    "${script_dir}/install.sh" 2>&1
)"; then
  echo "Installer accepted an artifact with failed provenance verification" >&2
  exit 1
fi
test -n "${attestation_output}"

printf '%064d  %s\n' 0 "${artifact}" > "${release_dir}/checksums.txt"
bad_runner_temp="${fixture_root}/bad-runner"
mkdir -p "${bad_runner_temp}"
if bad_output="$(
  RUNNER_TEMP="${bad_runner_temp}" \
    GITHUB_PATH="${fixture_root}/bad-github-path" \
    "${script_dir}/install.sh" 2>&1
)"; then
  echo "Installer accepted an invalid checksum" >&2
  exit 1
fi
grep -F "SHA-256 checksum mismatch for ${artifact}" <<< "${bad_output}"

symlink_stage="${fixture_root}/symlink-stage"
mkdir -p "${symlink_stage}"
cp "${repository_root}/LICENSE" "${symlink_stage}/LICENSE"
cp "${repository_root}/THIRD_PARTY_NOTICES" "${symlink_stage}/THIRD_PARTY_NOTICES"
ln -s LICENSE "${symlink_stage}/attribution"
tar -czf "${release_dir}/${artifact}" -C "${symlink_stage}" LICENSE THIRD_PARTY_NOTICES attribution
if command -v sha256sum >/dev/null 2>&1; then
  checksum="$(sha256sum "${release_dir}/${artifact}" | awk '{ print $1 }')"
else
  checksum="$(shasum -a 256 "${release_dir}/${artifact}" | awk '{ print $1 }')"
fi
printf '%s  %s\n' "${checksum}" "${artifact}" > "${release_dir}/checksums.txt"
unsafe_runner_temp="${fixture_root}/unsafe-runner"
mkdir -p "${unsafe_runner_temp}"
if unsafe_output="$(
  RUNNER_TEMP="${unsafe_runner_temp}" \
    GITHUB_PATH="${fixture_root}/unsafe-github-path" \
    "${script_dir}/install.sh" 2>&1
)"; then
  echo "Installer accepted a non-regular executable" >&2
  exit 1
fi
grep -F "Non-regular files in ${artifact}" <<< "${unsafe_output}"

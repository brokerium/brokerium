#!/usr/bin/env bash
# SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0
#
# Builds the site of the Helm repository https://charts.brokerium.kaufen: index.yaml lists the
# chart of every GitHub release that has one, with download URLs pointing at the release assets.
#
#   deploy/helm/repo/build-index.sh <output dir>
#
# Needs gh (GH_TOKEN) and helm. ARTIFACTHUB_REPOSITORY_ID, when set, goes into
# artifacthub-repo.yml for the verified publisher badge on Artifact Hub.
set -euo pipefail

out=${1:?usage: build-index.sh <output dir>}
repo=${GITHUB_REPOSITORY:-brokerium/brokerium}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$out" "$work/empty"
helm repo index "$work/empty"
mv "$work/empty/index.yaml" "$out/index.yaml"

for tag in $(gh release list --repo "$repo" --exclude-drafts --limit 1000 --json tagName --jq '.[].tagName'); do
  dir="$work/$tag"
  mkdir -p "$dir"
  if ! gh release download "$tag" --repo "$repo" --pattern 'brokerium-*.tgz' --dir "$dir" 2>/dev/null; then
    continue
  fi
  helm repo index "$dir" --url "https://github.com/$repo/releases/download/$tag" --merge "$out/index.yaml"
  mv "$dir/index.yaml" "$out/index.yaml"
  echo "indexed the chart of $tag"
done

cp "$(dirname "$0")/index.html" "$out/index.html"
if [ -n "${ARTIFACTHUB_REPOSITORY_ID:-}" ]; then
  printf 'repositoryID: %s\n' "$ARTIFACTHUB_REPOSITORY_ID" > "$out/artifacthub-repo.yml"
fi

#!/usr/bin/env bash
# Build the Encore image locally (same flags as .github/workflows/encore-image-build.yml).
#
# Why a copy: Encore CLI v1.58.x `encore build docker` hangs forever (100% CPU, no output)
# when run inside a git worktree or submodule, because cmdutil.WorkspaceRoot() walks up
# looking for a `.git` *directory* and loops without moving up when `.git` is a *file*.
# (encore-api has a local `replace` on ../pkg/relational, so the whole tree is exported.)
# So we build from a clean export (git archive HEAD) that is its own git repo with a real `.git` directory.
# Details: docs/tasks/R2E-ENCORE-BUILD-DONE.md
#
# Usage: scripts/build-encore-image.sh [IMAGE_TAG] [ARCH]
#   IMAGE_TAG default r2e-encore:dev   ARCH default: host arch (arm64 on Apple silicon)
set -euo pipefail
TAG="${1:-r2e-encore:dev}"
case "$(uname -m)" in arm64|aarch64) DEF=arm64 ;; *) DEF=amd64 ;; esac
ARCH="${2:-$DEF}"
REPO="$(git rev-parse --show-toplevel)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/r2e-encore-build.XXXXXX")"
WORK="$(cd "$WORK" && pwd -P)"   # Encore fails on symlinked paths (macOS /var -> /private/var)
trap 'rm -rf "$WORK"' EXIT
git -C "$REPO" archive --format=tar HEAD | tar -xf - -C "$WORK"
# A real throwaway repo: a `.git` dir makes WorkspaceRoot() terminate, and a valid repo
# keeps `go build` VCS stamping happy (Encore does not forward GOFLAGS=-buildvcs=false).
git -C "$WORK" init -q
git -C "$WORK" add -A
git -C "$WORK" -c user.name=r2e -c user.email=r2e@localhost commit -qm export
cd "$WORK/encore-api"
encore build docker "$TAG" --arch="$ARCH" \
  --services=authn,thingmodel,devicemetadata,fileassets,connectorcatalog,automation,notification,marketinstall,northbound,platformconfig,brokerscope,system \
  --skip-config

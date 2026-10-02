#!/usr/bin/env bash
set -euo pipefail

# Build from the same committed source as the binary; never archive runtime data.
source_root="$(cd "$(git rev-parse --show-toplevel)" && pwd -P)"
cd "$source_root"
git diff --quiet HEAD -- . || {
    echo "源码分发包必须从已提交且无修改的工作区生成。" >&2
    exit 1
}
test -d static/out || { echo "请先构建并嵌入前端。" >&2; exit 1; }
mkdir -p build/source
source_stage="$(mktemp -d "$source_root/build/source-stage.XXXXXX")"
cleanup_source_stage() {
    # Resolve and verify the target before recursive cleanup.
    local resolved
    resolved="$(cd "$source_stage" && pwd -P)"
    case "$resolved" in
        "$source_root"/build/source-stage.*) rm -rf -- "$resolved" ;;
        *) echo "拒绝清理不属于构建目录的路径。" >&2 ;;
    esac
}
trap cleanup_source_stage EXIT
git archive --format=tar HEAD | tar -xf - -C "$source_stage"
mkdir -p "$source_stage/static"
cp -R static/out "$source_stage/static/"
(
    cd "$source_stage"
    go mod vendor
    go version > BUILD_TOOLCHAIN.txt
    git -C "$source_root" rev-parse HEAD > SOURCE_REVISION.txt
)
tar -czf "$source_root/build/source/octopus-source.tar.gz" -C "$source_stage" .

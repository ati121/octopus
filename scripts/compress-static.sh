#!/usr/bin/env bash

# 为嵌入前端的（static/out 下的）文本类静态资源生成 .gz 预压缩变体，保留原文件。
# 由 scripts/build.sh 与 CI 的前端嵌入步骤在 mv web/out -> static/out 之后调用；
# 服务端 internal/server/middleware/static.go 按 Accept-Encoding 选择返回预压缩内容。
# 这些扩展名同时已从 gin gzip 中间件排除，避免二次压缩。

set -euo pipefail

STATIC_DIR="${1:-static/out}"

if [ ! -d "$STATIC_DIR" ]; then
    echo "error: static directory not found: $STATIC_DIR" >&2
    exit 1
fi

find "$STATIC_DIR" -type f \
    \( -name '*.js' -o -name '*.mjs' -o -name '*.css' -o -name '*.html' \
       -o -name '*.svg' -o -name '*.json' -o -name '*.txt' -o -name '*.xml' \
       -o -name '*.webmanifest' -o -name '*.map' \) \
    ! -name '*.gz' -print0 |
    xargs -0 -r gzip -9 -k

COUNT=$(find "$STATIC_DIR" -type f -name '*.gz' | wc -l)
echo "precompressed static assets under $STATIC_DIR ($COUNT .gz files)"

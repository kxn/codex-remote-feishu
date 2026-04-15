#!/usr/bin/env bash
set -euo pipefail

version="${VERSION:-}"
commit="${COMMIT:-}"
built_at="${BUILT_AT:-}"

cat <<NOTES
滚动开发构建（不是正式版）。

- version: ${version}
- commit: ${commit}
- built_at: ${built_at}
- 已安装实例可直接使用：
  - /upgrade dev
- 稳定用户请继续使用 production / beta。
NOTES

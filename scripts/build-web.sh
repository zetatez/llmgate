#!/usr/bin/env sh
# 构建前端产物并拷贝到 internal/web/dist（供 go:embed 使用）。
set -e
cd "$(dirname "$0")/.."

echo ">> building web ..."
(cd web && npm install --silent && npm run build)

echo ">> copying dist to internal/web/dist ..."
rm -rf internal/web/dist
mkdir -p internal/web/dist
cp -r web/dist/. internal/web/dist/

echo ">> done. dist ready for go:embed."
#!/bin/sh
# 以 root 初始化数据目录权限后，降权为 llmgate 用户运行服务。
set -e

if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  chown -R llmgate:llmgate /data
  exec su-exec llmgate /app/llmgate "$@"
fi

exec /app/llmgate "$@"
#!/bin/sh
# 生成 serve 容器运行时 env(敏感值只进 ~/.wbot,不进仓库)。
# 数据源: ~/.wbot/inspect.sh 第 13-17 行 export 块(网关地址/交易凭证/LLM/DSN)。
# 用法: umask 077 && tools/serve-env.sh        # 产出 ~/.wbot/serve.env
# 注意: 修改 inspect.sh export 块后重新生成。
set -eu
inspect=~/.wbot/inspect.sh
out=~/.wbot/serve.env
# export 块一行可含多个 KEY=VALUE(值均无空格,见 inspect.sh 第 13 行双变量);
# tr 拆成逐行 env-file 格式;引号值(DSN)由 compose env-file 解析器剥除。
sed -n '13,17p' "$inspect" | grep -v '^  *#' | sed 's/^  export //' | tr ' ' '\n' | sed '/^$/d' > "$out"
chmod 600 "$out"
echo "serve-env: wrote $out ($(wc -l < "$out") lines, 0600)"

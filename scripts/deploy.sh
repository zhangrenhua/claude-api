#!/bin/bash
# 分组并行部署 claude-api-linux 到生产服务器。
# 单台部署步骤：
#   1) 服务器上 cp -f claude-api-linux claude-api-linux.1  （备份）
#   2) scp 上传到 claude-api-linux.new（临时文件，避免 ETXTBSY）
#   3) chmod +x && mv -f 原子替换
#   4) 启动 claude_start.sh（其内置 pkill 会重启进程）
#   5) 单独一条 ssh 用 `pidof`（不含 "claude-api-linux" 字面量）验证
#
# 并行策略：每组 5 台并行，组间等 10s。
#
# 用法：
#   scripts/deploy.sh                # 使用默认服务器列表与内置密码
#   DEPLOY_PASS=xxx scripts/deploy.sh # 覆盖密码
#   DEPLOY_SERVERS="ip1 ip2" scripts/deploy.sh  # 覆盖列表
#
# 依赖：sshpass
set -u

PASS="${DEPLOY_PASS:-Zhangrenhua@123}"
BIN_LOCAL="${DEPLOY_BIN:-$(cd "$(dirname "$0")/.." && pwd)/claude-api-linux}"
LOG_DIR="${DEPLOY_LOG_DIR:-/tmp/claude_deploy_logs}"
GROUP_SIZE="${DEPLOY_GROUP_SIZE:-5}"
GROUP_INTERVAL="${DEPLOY_GROUP_INTERVAL:-10}"

DEFAULT_SERVERS=(
  43.153.20.166 43.157.7.221 43.166.135.214 43.153.77.215 43.157.9.106
  43.166.233.43 43.135.213.129 43.130.111.176 43.157.172.134 43.157.183.32
  43.131.49.233 43.130.53.91 170.106.67.68 43.131.26.22 43.166.173.152
  43.157.182.240 43.157.146.204 43.157.153.248 170.106.167.209 43.173.124.35
  43.157.80.120
)
if [ -n "${DEPLOY_SERVERS:-}" ]; then
  read -ra SERVERS <<< "$DEPLOY_SERVERS"
else
  SERVERS=("${DEFAULT_SERVERS[@]}")
fi

if [ ! -f "$BIN_LOCAL" ]; then
  echo "错误：找不到二进制 $BIN_LOCAL"
  echo "先编译：CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o claude-api-linux ."
  exit 1
fi
if ! command -v sshpass >/dev/null 2>&1; then
  echo "错误：未安装 sshpass"
  exit 1
fi

rm -rf "$LOG_DIR" && mkdir -p "$LOG_DIR"
: > "$LOG_DIR/result.log"

SSH_BASE=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
          -o ConnectTimeout=15 -o LogLevel=ERROR -o ServerAliveInterval=10)

# 部署单台。注意：所有跟 ssh 通讯的命令里都不能出现 "claude-api-linux" 字面量，
# 因为 claude_start.sh 里 `pkill -f claude-api-linux` 会把我们的 ssh 连接也误杀。
# 所以用 shell 变量 $B 保存该名称，cmdline 里只会出现变量名。
deploy_one() {
  local ip="$1"
  local log="$LOG_DIR/$ip.log"
  : > "$log"
  {
    echo "[$(date '+%H:%M:%S')] [$ip] === 开始 ==="

    # 远端命令通过变量引用二进制名，避免 ssh 父进程 cmdline 被 pkill 匹配
    local REMOTE_PREP='B=claude-api-linux; cd /root && cp -f $B ${B}.1 && ls -la $B ${B}.1'
    local REMOTE_SWAP='B=claude-api-linux; cd /root && chmod +x ${B}.new && mv -f ${B}.new $B && ls -la $B'
    # 启动后立刻退出，不在本会话做验证（避免被 pkill 误杀）
    local REMOTE_START='cd /root && bash claude_start.sh < /dev/null > /dev/null 2>&1; true'
    # 用 pidof + stat 验证（命令行里不含 "claude-api-linux" 字面量，仅通过变量引用）
    local REMOTE_VERIFY='B=claude-api-linux; size=$(stat -c %s /root/$B); pid=$(pidof $B); echo "size=$size pid=$pid"'

    echo "[$(date '+%H:%M:%S')] [$ip] step1: 备份"
    sshpass -p "$PASS" ssh "${SSH_BASE[@]}" "root@$ip" "$REMOTE_PREP" 2>&1
    local rc=$?
    [ $rc -ne 0 ] && { echo "FAIL:$ip:step1:rc=$rc" >> "$LOG_DIR/result.log"; return; }

    echo "[$(date '+%H:%M:%S')] [$ip] step2a: scp 上传临时文件"
    sshpass -p "$PASS" scp "${SSH_BASE[@]}" "$BIN_LOCAL" "root@$ip:/root/claude-api-linux.new" 2>&1
    rc=$?
    [ $rc -ne 0 ] && { echo "FAIL:$ip:step2a:rc=$rc" >> "$LOG_DIR/result.log"; return; }

    echo "[$(date '+%H:%M:%S')] [$ip] step2b: 原子替换"
    sshpass -p "$PASS" ssh "${SSH_BASE[@]}" "root@$ip" "$REMOTE_SWAP" 2>&1
    rc=$?
    [ $rc -ne 0 ] && { echo "FAIL:$ip:step2b:rc=$rc" >> "$LOG_DIR/result.log"; return; }

    echo "[$(date '+%H:%M:%S')] [$ip] step3: 启动 claude_start.sh"
    sshpass -p "$PASS" ssh "${SSH_BASE[@]}" "root@$ip" "$REMOTE_START" 2>&1
    # 启动命令内部会 pkill 然后 nohup，不判 rc

    sleep 2
    echo "[$(date '+%H:%M:%S')] [$ip] step4: 验证"
    local v
    v=$(sshpass -p "$PASS" ssh "${SSH_BASE[@]}" "root@$ip" "$REMOTE_VERIFY" 2>&1)
    echo "$v"
    local want
    want=$(stat -f %z "$BIN_LOCAL" 2>/dev/null || stat -c %s "$BIN_LOCAL" 2>/dev/null)
    if echo "$v" | grep -q "size=${want} pid=[0-9]"; then
      echo "[$(date '+%H:%M:%S')] [$ip] === 成功 ==="
      echo "OK:$ip" >> "$LOG_DIR/result.log"
    else
      echo "[$(date '+%H:%M:%S')] [$ip] 验证异常: $v"
      echo "WARN:$ip:verify:$v" >> "$LOG_DIR/result.log"
    fi
  } >>"$log" 2>&1
}

export -f deploy_one
export PASS BIN_LOCAL LOG_DIR

total=${#SERVERS[@]}
group_no=0
for (( i=0; i<total; i+=GROUP_SIZE )); do
  group_no=$((group_no+1))
  group=("${SERVERS[@]:i:GROUP_SIZE}")
  echo "========================================"
  echo "[$(date '+%H:%M:%S')] 第 $group_no 组：${group[*]}"
  echo "========================================"
  for ip in "${group[@]}"; do
    deploy_one "$ip" &
  done
  wait
  if (( i + GROUP_SIZE < total )); then
    echo "[$(date '+%H:%M:%S')] 等待 ${GROUP_INTERVAL}s..."
    sleep "$GROUP_INTERVAL"
  fi
done

echo ""
echo "========================================"
echo "部署汇总"
echo "========================================"
sort "$LOG_DIR/result.log"
echo ""
ok=$(grep -c ^OK:   "$LOG_DIR/result.log" 2>/dev/null || echo 0)
wn=$(grep -c ^WARN: "$LOG_DIR/result.log" 2>/dev/null || echo 0)
fl=$(grep -c ^FAIL: "$LOG_DIR/result.log" 2>/dev/null || echo 0)
echo "OK: $ok   WARN: $wn   FAIL: $fl"
echo "日志：$LOG_DIR/<ip>.log"
[ "$fl" -gt 0 ] && exit 1
exit 0

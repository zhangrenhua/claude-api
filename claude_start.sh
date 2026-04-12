#!/bin/bash

# 停止已有进程
pkill -f claude-api-linux 2>/dev/null && sleep 1

chmod +x claude-api-linux 

nohup ./claude-api-linux > /dev/null 2>&1 &

echo "claude api started, pid: $!"

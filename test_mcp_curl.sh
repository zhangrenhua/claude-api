#!/bin/bash
# ============================================================
# MCP 工具请求 curl 测试样例
# 用法: 修改 BASE_URL 和 API_KEY 后执行
# ============================================================

BASE_URL="https://ccvibe.cc"
API_KEY="sk-17427412f279e57994d829c139655a5b61f66a57038bbfebeff96a8a3542dd58"

# 通用 header
H_AUTH="x-api-key: ${API_KEY}"
H_CT="Content-Type: application/json"

# ============================================================
# 1. GitHub MCP — 单工具调用 (search_repositories)
# ============================================================
echo "===== 1. GitHub MCP: search_repositories ====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__github__search_repositories",
      "description": "Search for GitHub repositories matching a query",
      "input_schema": {
        "type": "object",
        "properties": {
          "query": {"type": "string", "description": "Search query"},
          "page": {"type": "integer", "description": "Page number (default 1)"},
          "per_page": {"type": "integer", "description": "Results per page (max 100)"}
        },
        "required": ["query"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "帮我搜一下 GitHub 上关于 claude api proxy 的 Go 语言项目"}
  ]
}'
echo -e "\n"

# ============================================================
# 2. GitHub MCP — 多工具定义 + 多轮工具调用
# ============================================================
echo "===== 2. GitHub MCP: 多轮 tool_use/tool_result ====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__github__search_repositories",
      "description": "Search for GitHub repositories",
      "input_schema": {
        "type": "object",
        "properties": {
          "query": {"type": "string"}
        },
        "required": ["query"]
      }
    },
    {
      "name": "mcp__github__get_file_contents",
      "description": "Get file contents from a GitHub repository",
      "input_schema": {
        "type": "object",
        "properties": {
          "owner": {"type": "string"},
          "repo": {"type": "string"},
          "path": {"type": "string"},
          "branch": {"type": "string"}
        },
        "required": ["owner", "repo", "path"]
      }
    },
    {
      "name": "mcp__github__create_issue",
      "description": "Create a new issue in a GitHub repository",
      "input_schema": {
        "type": "object",
        "properties": {
          "owner": {"type": "string"},
          "repo": {"type": "string"},
          "title": {"type": "string"},
          "body": {"type": "string"},
          "labels": {"type": "array", "items": {"type": "string"}}
        },
        "required": ["owner", "repo", "title"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "看一下 anthropics/claude-code 仓库的 README"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "我来获取这个仓库的 README 文件。"},
        {
          "type": "tool_use",
          "id": "mcp_gh_001",
          "name": "mcp__github__get_file_contents",
          "input": {"owner": "anthropics", "repo": "claude-code", "path": "README.md"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "mcp_gh_001",
          "content": "# Claude Code\n\nClaude Code is an agentic coding tool...\n\n## Installation\n\nnpm install -g @anthropic-ai/claude-code\n\n## Usage\n\nRun `claude` in your terminal."
        }
      ]
    },
    {"role": "user", "content": "内容挺简洁的，帮我总结一下这个项目"}
  ]
}'
echo -e "\n"

# ============================================================
# 3. Chrome DevTools MCP — navigate + screenshot + evaluate
# ============================================================
echo "===== 3. Chrome DevTools MCP: 多工具链式调用 ====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__chrome__navigate",
      "description": "Navigate to a URL in the browser",
      "input_schema": {
        "type": "object",
        "properties": {
          "url": {"type": "string", "format": "uri"}
        },
        "required": ["url"]
      }
    },
    {
      "name": "mcp__chrome__screenshot",
      "description": "Take a screenshot of the current page or a specific element",
      "input_schema": {
        "type": "object",
        "properties": {
          "selector": {"type": "string", "description": "CSS selector"},
          "width": {"type": "integer"},
          "height": {"type": "integer"}
        }
      }
    },
    {
      "name": "mcp__chrome__evaluate",
      "description": "Execute JavaScript in the browser console and return the result",
      "input_schema": {
        "type": "object",
        "properties": {
          "expression": {"type": "string", "description": "JavaScript expression to evaluate"}
        },
        "required": ["expression"]
      }
    },
    {
      "name": "mcp__chrome__click",
      "description": "Click an element on the page",
      "input_schema": {
        "type": "object",
        "properties": {
          "selector": {"type": "string", "description": "CSS selector of the element to click"}
        },
        "required": ["selector"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "打开百度首页，截个图看看"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "好的，我先导航到百度首页。"},
        {
          "type": "tool_use",
          "id": "chrome_001",
          "name": "mcp__chrome__navigate",
          "input": {"url": "https://www.baidu.com"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "chrome_001",
          "content": "Successfully navigated to https://www.baidu.com"
        }
      ]
    },
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "页面已打开，现在截图。"},
        {
          "type": "tool_use",
          "id": "chrome_002",
          "name": "mcp__chrome__screenshot",
          "input": {}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "chrome_002",
          "content": "[Screenshot: 1920x1080, shows Baidu search homepage with search bar and logo]"
        },
        {
          "type": "text",
          "text": "截图完成了，帮我用 JS 获取一下页面标题"
        }
      ]
    }
  ]
}'
echo -e "\n"

# ============================================================
# 4. SSH MCP — 远程命令执行
# ============================================================
echo "===== 4. SSH MCP: 远程命令执行 ====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__ssh__execute_command",
      "description": "Execute a command on a remote server via SSH",
      "input_schema": {
        "type": "object",
        "properties": {
          "host": {"type": "string", "description": "SSH host (user@host or configured alias)"},
          "command": {"type": "string", "description": "Shell command to execute"},
          "timeout": {"type": "integer", "description": "Command timeout in seconds", "default": 30}
        },
        "required": ["host", "command"]
      }
    },
    {
      "name": "mcp__ssh__list_connections",
      "description": "List available SSH connections from config",
      "input_schema": {
        "type": "object",
        "properties": {}
      }
    },
    {
      "name": "mcp__ssh__scp_upload",
      "description": "Upload a file to a remote server via SCP",
      "input_schema": {
        "type": "object",
        "properties": {
          "host": {"type": "string"},
          "local_path": {"type": "string"},
          "remote_path": {"type": "string"}
        },
        "required": ["host", "local_path", "remote_path"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "帮我看一下 35.236.134.32 服务器的磁盘和内存使用情况"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "好的，我来检查服务器状态。"},
        {
          "type": "tool_use",
          "id": "ssh_001",
          "name": "mcp__ssh__execute_command",
          "input": {"host": "gavin@35.236.134.32", "command": "df -h && echo '---' && free -m", "timeout": 10}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "ssh_001",
          "content": "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   23G   25G  48% /\ntmpfs           3.9G     0  3.9G   0% /dev/shm\n---\n              total        used        free\nMem:           7982        3214        2567\nSwap:          2047         128        1919"
        }
      ]
    },
    {"role": "user", "content": "磁盘用了快一半了，帮我看下哪些目录占用最多"}
  ]
}'
echo -e "\n"

# ============================================================
# 5. 混合 MCP — GitHub + SSH + Chrome 并行调用
# ============================================================
echo "===== 5. 混合: GitHub + SSH 并行 tool_use ====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__github__search_repositories",
      "description": "Search GitHub repositories",
      "input_schema": {
        "type": "object",
        "properties": {
          "query": {"type": "string"}
        },
        "required": ["query"]
      }
    },
    {
      "name": "mcp__ssh__execute_command",
      "description": "Execute a command on a remote server via SSH",
      "input_schema": {
        "type": "object",
        "properties": {
          "host": {"type": "string"},
          "command": {"type": "string"},
          "timeout": {"type": "integer"}
        },
        "required": ["host", "command"]
      }
    },
    {
      "name": "mcp__chrome__navigate",
      "description": "Navigate to a URL",
      "input_schema": {
        "type": "object",
        "properties": {
          "url": {"type": "string"}
        },
        "required": ["url"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "同时帮我做两件事：1) 在 GitHub 搜 MCP server 相关项目 2) 看下 34.80.42.77 服务器的 uptime"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "好的，我同时执行这两个任务。"},
        {
          "type": "tool_use",
          "id": "mix_001",
          "name": "mcp__github__search_repositories",
          "input": {"query": "mcp server model context protocol"}
        },
        {
          "type": "tool_use",
          "id": "mix_002",
          "name": "mcp__ssh__execute_command",
          "input": {"host": "gavin@34.80.42.77", "command": "uptime", "timeout": 5}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "mix_001",
          "content": "{\"total_count\":156,\"items\":[{\"full_name\":\"modelcontextprotocol/servers\",\"stars\":15200},{\"full_name\":\"anthropics/mcp-typescript-sdk\",\"stars\":3400}]}"
        },
        {
          "type": "tool_result",
          "tool_use_id": "mix_002",
          "content": "15:42:01 up 89 days, 3:21, 2 users, load average: 0.23, 0.18, 0.12"
        }
      ]
    },
    {"role": "user", "content": "不错，服务器运行稳定。MCP server 那个项目 star 挺多的，帮我打开看看"}
  ]
}'
echo -e "\n"

# ============================================================
# 6. 边界场景 — 悬空 tool_use（用户跳过工具执行）
# ============================================================
echo "===== 6. 边界: 悬空 tool_use（测试 fixDanglingToolUses）====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-sonnet-4-5-20250929",
  "max_tokens": 4096,
  "stream": true,
  "tools": [
    {
      "name": "mcp__ssh__execute_command",
      "description": "Execute a command on a remote server via SSH",
      "input_schema": {
        "type": "object",
        "properties": {
          "host": {"type": "string"},
          "command": {"type": "string"}
        },
        "required": ["host", "command"]
      }
    }
  ],
  "messages": [
    {"role": "user", "content": "帮我重启一下服务器上的 nginx"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "好的，我来重启 nginx。"},
        {
          "type": "tool_use",
          "id": "dangling_001",
          "name": "mcp__ssh__execute_command",
          "input": {"host": "root@43.130.43.252", "command": "systemctl restart nginx"}
        }
      ]
    },
    {"role": "user", "content": "等等，先别重启！我还在调试呢，先看下 nginx 的状态就行"}
  ]
}'
echo -e "\n"

# ============================================================
# 7. 边界场景 — 无工具定义但历史有工具调用（Zed 标题生成模式）
# ============================================================
echo "===== 7. 边界: 无 tools 字段 + 历史含工具（测试 flattenToolRefs）====="
curl -s -X POST "${BASE_URL}/v1/messages" \
  -H "${H_AUTH}" -H "${H_CT}" \
  -d '{
  "model": "claude-haiku-4-5-20251001",
  "max_tokens": 64000,
  "temperature": 1.0,
  "stream": true,
  "messages": [
    {"role": "user", "content": "帮我看下服务器状态"},
    {
      "role": "assistant",
      "content": [
        {"type": "text", "text": "我来检查一下。"},
        {
          "type": "tool_use",
          "id": "hist_tool_001",
          "name": "mcp__ssh__execute_command",
          "input": {"host": "server1", "command": "uptime"}
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "tool_result",
          "tool_use_id": "hist_tool_001",
          "content": "up 42 days, load average: 0.1"
        },
        {
          "type": "text",
          "text": "Generate a concise 3-7 word title for this conversation."
        }
      ]
    }
  ]
}'
echo -e "\n"

echo "===== All tests done ====="

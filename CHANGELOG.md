# 更新日志 (Changelog)

## [2026-03-09] - 日志管理增强版

### 🎉 新增功能

#### 1. 用户使用统计功能
- **用户筛选器**: 在请求日志页面新增"用户"下拉选择器，可按用户筛选日志
- **用户统计面板**: 新增"用户统计"按钮，展开后显示每个用户的详细使用情况
  - 请求次数（总数、成功、失败）
  - Token 消耗（输入、输出、总计）
  - 总消费金额（美元）
  - 平均响应时间
  - 最后请求时间
- **用户使用分析**: 支持按时间范围、账号、端点类型等条件查看用户统计

#### 2. 日志保留期限优化
- **默认保留期**: 从 7 天延长至 30 天
- **更长查询范围**: 新增"最近30天"快捷查询按钮

#### 3. 性能优化
- **并行加载**: 日志页面切换时并行加载账号、用户、日志数据，提升加载速度
- **智能缓存**: 优化数据加载逻辑，避免重复请求

### 📝 修改的文件

#### 后端文件

1. **internal/models/request_log.go**
   - 新增 `UserStat` 结构体，用于用户统计数据
   - 在 `LogFilters` 中添加 `UserID` 字段支持用户筛选

2. **internal/database/logs.go**
   - 新增 `GetUserUsageStats()` 方法，按用户分组统计使用情况
   - 在 `applyLogFiltersGorm()` 中添加用户ID筛选逻辑

3. **internal/database/settings.go**
   - 修改 `LogRetentionDays` 默认值从 7 改为 30

4. **internal/api/handlers.go**
   - 新增 `handleGetUserUsageStats()` API 处理函数
   - 在 `handleGetLogs()` 中添加 `user_id` 参数支持
   - 在 `handleGetStats()` 中添加 `user_id` 参数支持

5. **internal/api/routes.go**
   - 新增路由 `GET /v2/logs/user-stats` 用于获取用户统计

#### 前端文件

1. **frontend/js/logs.js**
   - 新增 `userStats` 数据字段存储用户统计
   - 新增 `showUserStatsPanel` 控制用户统计面板显示
   - 新增 `userSelectOpen` 控制用户选择器状态
   - 在 `logsFilters` 中添加 `userID` 字段
   - 新增 `handleToggleUserStatsPanel()` 方法切换统计面板
   - 新增 `handleLoadUserStats()` 方法加载用户统计数据
   - 在 `handleLoadLogs()` 和 `handleLoadLogsStats()` 中添加 `user_id` 参数传递
   - 在 `handleSetLogsTimeRange()` 中添加 30 天选项支持
   - 在时间范围切换和筛选重置时同步刷新用户统计

2. **frontend/index.html**
   - 在日志页面头部添加"用户统计"按钮
   - 在筛选器中添加"用户"下拉选择器（位于账号和客户端IP之间）
   - 新增用户统计表格，显示详细的用户使用数据
   - 在时间范围快捷按钮中添加"最近30天"选项
   - 修改查询按钮逻辑，同时刷新日志和用户统计

3. **frontend/js/app.js**
   - 优化 `loadTabData()` 方法，在加载日志页面时并行加载账号、用户、日志数据
   - 使用 `Promise.all()` 实现并行加载，提升页面切换速度

### 🔧 技术细节

- **数据库查询优化**: 使用批量查询和 JOIN 优化用户统计性能
- **前端性能**: 使用 Vue 响应式数据和条件渲染优化界面更新
- **API 设计**: RESTful 风格，支持完整的筛选条件传递

### 📊 使用方法

1. **查看用户统计**:
   - 进入"请求日志"页面
   - 点击"用户统计"按钮展开统计面板
   - 可配合时间范围、账号等筛选条件查看特定时段的用户统计

2. **按用户筛选日志**:
   - 在筛选器中选择"用户"下拉框
   - 选择特定用户查看其日志记录

3. **查询30天日志**:
   - 点击"最近30天"按钮快速设置时间范围
   - 或手动设置开始/结束时间

### 🐛 已知问题

- 无

### 📌 注意事项

- 首次使用需要重新编译项目：`go build -o claude-server main.go`
- 浏览器需要强制刷新（Ctrl+Shift+R 或 Cmd+Shift+R）以加载新的前端文件
- 日志保留天数的修改只影响新安装，已有配置不会自动更新

---

## 历史版本

查看 Git 提交历史获取更多信息。

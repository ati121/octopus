# Octopus 修改版

复刻自上游项目 [bestruirui/octopus](https://github.com/bestruirui/octopus)。

本仓库在上游基础上进行了修改，目前包括：

- Chat 流正常结束时不再误记为 `context canceled`。
- 请求进入后立即在日志中显示为**响应中**，完成后原位更新同一条记录。

## Docker 使用

```bash
git clone -b dev https://github.com/tianxia3111/octopus.git
cd octopus
docker compose up -d --build
```

访问 <http://localhost:8080>。默认用户名和密码均为 `admin`，首次登录后请立即修改。

数据默认保存在 `./data`，需要改到其他位置时可修改 `docker-compose.yml` 中的挂载路径。

English: [README.md](README.md)

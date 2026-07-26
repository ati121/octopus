# Octopus 修改版

复刻自上游项目 [bestruirui/octopus](https://github.com/bestruirui/octopus)。

本仓库在上游基础上进行了修改，目前包括：

- Chat 流正常结束时不再误记为 `context canceled`。
- 请求进入后立即在日志中显示为**响应中**，完成后原位更新同一条记录。

## Docker 使用

直接运行：

```bash
docker run -d --name octopus -v /path/to/data:/app/data -p 8080:8080 ghcr.io/ati121/octopus:latest
```

Docker Compose：

```yaml
services:
  octopus:
    image: ghcr.io/ati121/octopus:latest
    ports:
      - "8080:8080"
    volumes:
      - "/path/to/data:/app/data"
    container_name: octopus
    restart: unless-stopped
```

```bash
docker compose up -d
```

访问 <http://localhost:8080>。默认用户名和密码均为 `admin`，首次登录后请立即修改。

English: [README.md](README.md)

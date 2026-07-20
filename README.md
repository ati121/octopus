# Octopus Fork

Forked from [bestruirui/octopus](https://github.com/bestruirui/octopus).

This fork contains local changes built on top of upstream, including:

- Completed Chat streams are no longer incorrectly recorded as `context canceled`.
- Relay requests appear in logs immediately as **Responding** and update in place when complete.

## Docker

Run directly:

```bash
docker run -d --name octopus -v /path/to/data:/app/data -p 8080:8080 ghcr.io/tianxia3111/octopus:latest
```

Docker Compose:

```yaml
services:
  octopus:
    image: ghcr.io/tianxia3111/octopus:latest
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

Open <http://localhost:8080>. The default username and password are both `admin`; change them after the first login.

See [README_zh.md](README_zh.md) for Chinese.

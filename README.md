# Octopus Fork

Forked from [bestruirui/octopus](https://github.com/bestruirui/octopus).

This fork contains local changes built on top of upstream, including:

- Completed Chat streams are no longer incorrectly recorded as `context canceled`.
- Relay requests appear in logs immediately as **Responding** and update in place when complete.

## Docker

```bash
git clone -b dev https://github.com/tianxia3111/octopus.git
cd octopus
docker compose up -d --build
```

Open <http://localhost:8080>. The default username and password are both `admin`; change them after the first login.

Data is stored in `./data` by default. Edit the volume in `docker-compose.yml` if you need another location.

See [README_zh.md](README_zh.md) for Chinese.

# 第三方组件与重新构建

Octopus 使用 AxonHub 的 `github.com/looplj/axonhub/llm` 库处理协议。
锁定版本为 `v0.0.0-20261001173204-2bf8e2c91641`，版权归 AxonHub 贡献者所有，
该库及其使用适用 LGPL-3.0；本项目未修改库源码。

- 上游及对应源码：[looplj/axonhub，提交 2bf8e2c91641](https://github.com/looplj/axonhub/tree/2bf8e2c91641/llm)。
- 许可证全文：[LGPL-3.0](licenses/LGPL-3.0.txt) 与其引用的 [GPL-3.0](licenses/GPL-3.0.txt)。
- Octopus 自有代码继续遵循根目录 [LICENSE](LICENSE) 的 AGPL-3.0。
- 历史派生代码的 MIT 声明保留在 `internal/transformer/NOTICE`，它不代表当前 AxonHub 库的许可证。

构建工作流为二进制和镜像附带 `octopus-source.tar.gz`，包含对应 Octopus 源码、
嵌入的前端文件、Go vendor 依赖及构建工具链版本。
镜像内文件位于 `/usr/share/doc/octopus/`。源包通过
`bash scripts/package-source.sh` 从已提交的干净 checkout 生成，不包括运行数据或密钥。

解包后，使用 `BUILD_TOOLCHAIN.txt` 记录的 Go 版本（至少 1.26）重新构建：

```sh
CGO_ENABLED=0 go build -mod=vendor -tags=jsoniter -o octopus .
```

可以修改 `vendor/github.com/looplj/axonhub/llm/` 下的库源码，再执行相同命令重新链接。
如需更换完整库版本，也可以修改 `go.mod` 并重新生成 vendor。
本项目不限制为修改该库或调试这些修改所需的逆向工程。
源码包中的 `SOURCE_REVISION.txt` 标识对应提交；镜像构建参数与前端构建方法见
`.github/workflows/publish-image.yml`。重新构建后可替换原可执行文件并使用原配置启动，
无需厂商签名或解锁步骤。

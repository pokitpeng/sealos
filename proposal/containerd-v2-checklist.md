# containerd 2.x 适配待办清单

对应设计：[containerd-v2-support.md](containerd-v2-support.md)。标记规则：`[ ]` 未完成；每项需记录 PR、镜像 digest 或测试日志才能勾选。**当前只有设计，无已验证的 2.x 支持。**

## P0：确认范围与供应链（开始编码前）

- [ ] 重新核实 v2.4.1 的 stable/维护状态、安全公告、amd64/arm64 官方包、对应 SHA256 和 Kubernetes 兼容范围；批准或调整候选版本，记录决策。
- [ ] 定义首批目标 Kubernetes 版本与主机 OS/CPU、默认 runc（crun 是否纳入）、新建集群支持范围，明确存量升级暂不支持。目标 1.34+，先验收 1.34.x，再按 minor 扩至 1.35/1.36/1.37；未来 minor 须新验收，不能无限承诺。
- [ ] 确认 **第四仓库** `labring-actions/cache`：现有 `.github/scripts/cri` 固定选择 v1.7.x，`runtime/.github/scripts/kubernetes.sh` 依赖其 `cri-$ARCH`；决定 fork/cache 分版本制品或在 runtime 直接下载/校验，记录 owner 和 PR。
- [ ] 确认三个 fork 的上游和源码同步方向；检查 `sealos/.github/workflows/sync_code.yml` 的目标仓库/凭据，避免 fork 自动推错库。

## P1：构建指定版本运行时（cache 或 runtime；负责人待定）

- [ ] 固定 containerd 2.x 官方下载地址/校验值及 runc/crun/libseccomp 依赖版本；严格校验每个架构的 SHA256，失败即中止。
- [ ] 按 `runtime/containerd/scripts/init-containerd.sh` 的 `--strip-components 2` 要求打包 `cri/cri-containerd.tar.gz`；测试两架构解压后的文件名及执行权限。
- [ ] 发布不可变版本标识（tag + digest），改造 `runtime/.github/scripts/kubernetes.sh` 明确选择 2.x 输入；不覆盖旧 `cri-$ARCH`；记录 `containerd --version`。
- [ ] 校正 `runtime/version.yaml` 等可能误导的版本信息，增加构建版本清单；确认该文件是否参与流水线，再决定如何维护。

## P1：配置、安装与 shim（runtime + sealos）

- [ ] `runtime/containerd/etc/` 增加独立的 v4 模板（v2.4.1 实测默认配置格式），按 images/runtime 插件分别放置 pause/snapshotter/registry 与 cgroup/runc/crun 等配置；原 v2 模板保持可用。
- [ ] 检查并更新 `runtime/containerd/scripts/check.sh`、`init-containerd.sh`、`clean-containerd.sh`：按镜像选择正确模板、验证插件/运行时已加载及移除新包的全部安装文件；禁止误删现有生产数据。
- [ ] 审核私有仓库 `hosts.toml`、镜像认证、TLS/HTTP 与离线场景；已加入运行时生成、root-only 的 auth header 并在模拟仓库通过 CRI PullImage 正反例；仍须验证真实 htpasswd 仓库、证书/重定向、密码轮换、冷启动 RunPodSandbox，确保公开镜像层和日志不泄露凭据。
- [ ] 在 `sealos/lifecycle/staging/src/github.com/labring/image-cri-shim/pkg/cri/cri.go` 同时解析旧和新插件的默认运行时 cgroup 配置；未知/损坏配置不能静默回退成错误驱动。
- [ ] 增加单元测试：旧 v2 配置、新 v3/v4 配置、runc/crun 默认、SystemdCgroup true/false、无文件/错误 TOML/缺失 runtime；验证 kubeadm 与 kubelet 的 cgroup driver 一致。
- [ ] 运行 staging shim 单测并检查本地 `image-cri-shim/` 源码同步结果；不要在两份源码中独立提交不同实现。

- [ ] 已为 1.34–1.36 的 containerd 2.4.1 实验路径实现 .0 base + 官方 SHA256 校验的目标补丁版三件套和新 pause 清单（v1.34.12/amd64 在隔离测试机校验通过）；仍须构建集群镜像确认最终 `FROM` 层没有保留错误的二进制/registry 镜像。arm64 尚未测试；1.37 缺基础缓存与 CRI 工具缓存，已显式拒绝构建。固定两处缓存 digest 后才可发布。

## P2：端到端与发布（通过前不标注“已支持”）

- [ ] 用指定 containerd 版本、指定 Sealos/shim 提交构建并发布测试专用 Kubernetes 集群镜像；保存构建输入、两架构 digest、SBOM/校验记录。
- [ ] 在 amd64 与 arm64 分别执行单机安装：**amd64 / 1.34.12** 已验证镜像内二进制版本、CRI v1、插件、单节点 Node Ready、systemd cgroup 一致、Pod Running 与 DNS；arm64、Pod/服务重启和真实无缓存断网冷启动仍未测。
- [ ] 多节点扩容、离线镜像/认证/私有仓库、重启 containerd/kubelet、清理脚本；runc 与 crun（若支持）均覆盖。单节点测试已验证节点生成的 auth header 存在及本地 registry pause 已缓存，但不能代替无缓存断网测试。
- [ ] 在兼容性矩阵的 Kubernetes 下限/主流/上限做真实部署；另用旧 1.x 镜像回归，确认新模板不会污染旧构建路径。
- [ ] 独立验证已有 1.x 集群升级到 2.x 的实验流程与数据安全；若未完成，发布说明明确禁止将其视为受支持的原地升级。
- [ ] 完成 `sealos` → `image-cri-shim` 同步/发布、CI 门禁、镜像版本映射和用户文档；记录哪些组合已测/未测以及失败回滚手册。`runtime/.github/workflows/build-kubernetes-134-containerd2.yml` 已添加按 amd64/arm64 分别构建检查的候选 CI，需推送两个 fork 分支、配置大磁盘自托管 runner 后执行；arm64 仍须真机验收。

## 二次评审新增：必须关闭的缺口

- [ ] P0：批准具体 OS/内核/glibc/cgroup、Kubernetes/Sealos/containerd 版本交集；明确供应链路线，第四个 fork 不是强制要求。
- [ ] P1：同时构建修复后的 **sealctl 和 image-cri-shim**，检查镜像内与节点上的版本/来源；直接验证 `sealctl cri cgroup-driver --short` 的输出和非零错误退出。
- [ ] P1：完善 pause 镜像绕过 shim 时的认证方案：已实现节点运行时写入 root-only hosts.toml 并用临时模拟仓库验证，真实 registry 及无缓存 Pod 启动仍待验证。
- [ ] P1：增加冷启动用例：新节点无 pause 缓存、无外网、registry 启用认证时 RunPodSandbox 成功；错误凭据明确失败，不回退到公网。
- [ ] P1：定义配置版本/插件优先级、默认字段语义与 imports 支持边界，补充相应单测，避免未知结构静默返回 cgroupfs。
- [ ] P1：明确 `CONTAINERD_VERSION` 到包、模板、标签的统一选择；最终镜像只含一个选中的 `etc/config.toml.tmpl`（v2.4.1 的 v4 模板还须验证 `io.containerd.server.v1.grpc`）；同步修改 `manifest.sh`，避免覆盖原 tag。
- [ ] P1：解决 `crun_` 与 `crun` 命名差异；首批 runc 固定默认，crun 未完成实测前不声明支持。
- [ ] P2：修正 shim E2E 的路径过滤（补 `labring/`）并覆盖 sealctl 变更；验证 PR 确实触发测试。
- [ ] P2：锁定所有实际构建输入的 digest，不只 containerd；记录实际镜像拉取模式及对应并发配置生效情况。

- [ ] 修正/文档化 CNI PodCIDR：`labring/flannel:0.24.0` 默认 `172.31.0.0/17` 与本次集群 `100.64.0.0/10` 不匹配，当前仅测试机手工 patch ConfigMap 后 CoreDNS 和业务 Pod 正常；正式发布需自动化保证一致。

## 完成判据

- 构建可重复：固定版本和 digest，CI 中实测二进制版本为目标 2.x，两个架构通过。
- 新建集群矩阵通过，旧 1.x 回归通过；错误配置有明确报错，无隐性 cgroupfs 回退。
- shim 版本、集群镜像与 Sealos 版本的兼容关系及存量升级限制均有发布记录。

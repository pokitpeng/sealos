# Sealos 支持 containerd 2.x：设计方案（草案）

状态：实现中（**amd64 / Kubernetes v1.34.12 单节点测试已通过**；未发布、其他版本未验收）｜目标版本：**containerd v2.4.1（待多架构/多版本验收）**｜范围：Sealos lifecycle / Kubernetes 集群镜像，不包含 K3s 自带 containerd 或云平台所有工作负载。

> 版本选择说明：查询时 containerd GitHub `releases/latest` 返回 v2.4.1（非 prerelease，2026-09-24 发布）。正式开发前重新核实其维护状态、安全公告、Kubernetes/CRI 兼容范围及 amd64/arm64 发布资产；固定精确补丁版本和 SHA256，不使用 `latest`。升级到更新补丁版本须重新运行兼容性矩阵。**文档不是支持声明；通过验收后再对外声明。**

## 1. 背景与目标

当前 Kubernetes 集群镜像使用的配置模板 `runtime/containerd/etc/config.toml.tmpl` 为 `version = 2` 且使用 `io.containerd.grpc.v1.cri`；此处 `version = 2` 指 TOML **配置格式 v2**，不是 containerd **二进制 2.x**。`runtime/.github/scripts/kubernetes.sh` 从 `ghcr.io/labring-actions/cache:cri-$ARCH` 拷贝 `cri/cri-containerd.tar.gz`；上游 `labring-actions/cache/.github/scripts/cri` 当前强制选择 containerd v1.7.x。`runtime/version.yaml` 仍写 1.6.16，不能把它当作实际集群镜像中的版本：需修正构建元数据和最终制品的可追溯性。

Sealos 内嵌的 `image-cri-shim` 读取 `/etc/containerd/config.toml` 时只识别旧插件路径，未识别 containerd 2.x 配置格式 v3/v4 的新插件路径；若未读到默认运行时，会静默返回 `cgroupfs`，可能导致 kubelet 与 containerd cgroup driver 不一致。目标是新构建的 Kubernetes 集群镜像**明确使用且验证指定的 containerd 2.x**，保留现有 1.x 集群镜像功能，不偷偷替换已有镜像标签。

## 2. 仓库/制品边界与顺序

| 位置 | 责任 | 交付 |
| --- | --- | --- |
| `sealos/` | lifecycle、内嵌 `image-cri-shim`、测试/发布 | 兼容旧 v2 与新 v3/v4 配置的 shim；端到端验证 |
| `image-cri-shim/` | `sealos/lifecycle/staging/src/github.com/labring/image-cri-shim/` 的同步仓库 | 与上游源码一致的同步提交/发布，不在两边分别开发 |
| `runtime/` | 集群镜像的模板、安装/清理脚本、构建流水线 | 目标 v2.4.1 的 v4 配置及新版本镜像；明确依赖指定的缓存制品 |
| **额外依赖：[`labring-actions/cache`](https://github.com/labring-actions/cache)**（当前未在本地三仓库中） | 构建 `ghcr.io/labring-actions/cache:cri-$ARCH`，打包 `cri-containerd.tar.gz` | 生产环境可重复构建的 2.x amd64/arm64 缓存镜像；或改造 `runtime/` 直接引入已校验官方包 |

**关键发现**：仅修改模板/shim、继续使用旧缓存不能完成适配；但第四个 fork 并非必需，修改当前 `runtime` 的二进制获取流程也可解决。缓存镜像供应链必须解决。建议额外 fork/cache，建立独立版本标签（如 `cri-containerd-v2.4.1-$ARCH`），避免直接覆盖共用的 `cri-$ARCH` 影响 1.x 构建；另一条路线是在 `runtime` 构建阶段直接下载并校验官方压缩包，再按 `cri-containerd.tar.gz` 原有布局打包。两方案择一，经评审确定；不能把旧缓存包标成 2.x。

建议依赖顺序：锁定版本/资产 → 供应链（cache 或 runtime 直接下载）→ shim（先在 sealos staging 开发）→ runtime 模板 → 集成镜像 → 同步独立 shim 仓库/发布。独立 shim 仓库的推送需要检查 `sealos/.github/sync_code.yml` 和其工作流的实际权限及目标 fork；切勿默认 fork 会自动同步到 `pokitpeng/image-cri-shim`。

## 3. 技术设计

### 3.1 版本与二进制来源

- 以 containerd **v2.4.1** 为首个候选，仅使用其正式发布的 linux/amd64、linux/arm64 包及对应 SHA256；固定 runc、crun、libseccomp、crictl 的兼容版本/校验值，核对 OCI 二进制、CNI 等依赖。构建时输出版本清单（containerd 镜像 tag + digest、sealos/shim commit、Kubernetes 版本及配置版本）；CI 运行 `containerd --version` 和 `crictl version` 实测制品版本。
- 不使用可漂移的 `cri-$ARCH`、`latest` 或按时间自动选最高 tag 作为正式构建输入；构建镜像名称须含 containerd 版本或有不可变 digest，使用独立测试 tag，验证后再决定常规发布标签策略。
- 检查缓存包结构：当前 `runtime/containerd/scripts/init-containerd.sh` 用 `--strip-components 2` 解包 `usr/bin/*`，新包或自制包必须满足该布局；安装后校验二进制、服务状态、CRI v1 与 systemd cgroup 配置。

### 3.2 生成 containerd 2.x 配置

为 v2.4.1 新建**独立**的 v4 源模板（不就地覆盖 1.x 模板），构建阶段按锁定的 `CONTAINERD_VERSION` 选择且只将选中模板输出为镜像内的 `etc/config.toml.tmpl`，渲染后仍为 `etc/config.toml`。未知主版本直接拒绝构建，禁止依赖文件复制顺序覆盖模板；版本选择必须同时控制二进制、模板、镜像标签和 manifest 发布脚本。检查 `runtime/containerd/scripts/check.sh`、`init-containerd.sh` 对字段和文件名的操作。

字段映射（以锁定版本的 `containerd config default` 和官方文档为准）：**在测试机上实测 v2.4.1 的 `config default` 输出 `version = 4`，且 gRPC socket 配置位于 `[plugins."io.containerd.server.v1.grpc"]`；先前只考虑 v3 是错误的。**

| 原配置（v2） | 新配置（v4；部分路径也适用于 v3） |
| --- | --- |
| `[plugins."io.containerd.grpc.v1.cri".containerd]` 下 `snapshotter` | `[plugins."io.containerd.cri.v1.images"]` 下 `snapshotter` |
| `sandbox_image` | `[plugins."io.containerd.cri.v1.images".pinned_images]` 下 `sandbox` |
| `registry.config_path` | `[plugins."io.containerd.cri.v1.images".registry]` 下 `config_path` |
| `max_concurrent_downloads` | images 插件；确认新版本 transfer/local pull 实际生效的配置路径 |
| `max_container_log_line_size`、`disable_apparmor`、`default_runtime_name`、`runtimes.*.options.SystemdCgroup` | `[plugins."io.containerd.cri.v1.runtime"]` 及其 `.containerd.runtimes.*.options` |

沿用 `/run/containerd/containerd.sock`、`/var/lib/containerd` 与 `certs.d/hosts.toml`；检查 pause 镜像可达且实际使用。旧模板内的 `registry.configs.*.auth` 不应原样搬到 v4：该写法已弃用，transfer pull 不支持其中部分配置。业务镜像可使用现有 `image-cri-shim` 的 CRI 拉取凭据及 `certs.d` host 配置，但 **RunPodSandbox 触发的 pause 镜像拉取由 containerd 内部完成，不经过 image-cri-shim**。`runtime/registry/etc/registry_config.yml.tmpl` 启用了 htpasswd，现有 `hosts.toml.tmpl` 没有认证信息，因此直接删除旧 auth 字段会留下冷启动风险。首批选用只写入节点 root 权限 `hosts.toml` 的、限定 Sealos 内部 registry host 的 Basic 认证 header（`runtime/containerd/scripts/init-containerd.sh`）；通过隔离模拟仓库验证了 containerd v2.4.1 的 CRI 直连 `PullImage`：无 header 返回 `no basic auth credentials`，有 header 则拉取成功。但仍须在真实 Sealos htpasswd 仓库、无缓存节点和 `RunPodSandbox` 中验证；检查凭据更新和意外重定向行为。hosts.toml 在节点运行时生成，权限限定 root，日志脱敏，禁止把生成后的 header 文件固化到公共镜像层；但现有 Kubefile 的 `registryUsername=admin`/`registryPassword=passw0rd` 本身就是公开默认值，生产环境必须换成独立密钥并限制节点间网络。Basic 编码不是加密，HTTP 仅用于明确隔离的网络。验收必须在无 pause 缓存的新节点断网执行 RunPodSandbox，不能以经过 shim 的 `crictl pull` 成功替代。`use_local_image_pull` 的取值应以实际所需功能/压测结果为准，不假设 transfer 的功能等价。

`crun` 默认运行时分支仍由脚本选择；确保新配置中 `default_runtime_name` 与对应 runtimes 节点一致且两种运行时的 `SystemdCgroup` 一致。运行 `containerd --config ... config dump`、`ctr plugins ls` 与 `crictl info` 检查插件已加载，禁止仅靠 TOML 可解析就判定通过。1.x 镜像继续使用旧模板；v2.4.1 虽可能转换较早配置版本，但正式交付采用在目标二进制上实测的 v4 以降低废弃项风险。

### 3.3 Sealos / image-cri-shim

- 修改 `sealos/lifecycle/staging/src/github.com/labring/image-cri-shim/pkg/cri/cri.go` 中 `ContainerdRuntime.processConfigFile()`：同时识别旧 `io.containerd.grpc.v1.cri` 与新 `io.containerd.cri.v1.runtime`（v3 与 v4），从有效 `default_runtime_name` 对应运行时读取 `SystemdCgroup`；兼容指定路径不存在、默认运行时为空、选中 runtime 缺失、坏 TOML。**不能把解析失败/未知配置静默判作 cgroupfs**；记录明确错误或采用经过验证的 CRI RuntimeConfig 回退。保留已有 v1 行为；检查 `config` 路径的来源与优先级。
- 检查 `sealctl` cgroup 检测到 `kubeadm` 的调用链（`sealos/lifecycle/pkg/ssh/remote.go` → `sealos/lifecycle/pkg/runtime/kubernetes/kubeadm.go`），确保 kubelet 配置与 containerd 实际驱动一致；确认目标 Kubernetes 版本支持 CRI v1 和所选自动检测机制。当前 `sealos/lifecycle/go.mod` 中的 containerd v1.7.12 是 **Go 构建依赖**，不是打包进集群的 containerd 版本；仅在发现 SDK API 依赖问题时单独升级，避免无谓扩大范围。
- **必须同时重编译并交付 `sealctl` 与 `image-cri-shim`**。准确调用链是 kubeadm → SSH Remote.CGroup → `sealctl cri cgroup-driver --short` → 内嵌 `pkg/cri.CGroupDriver()`（见 `sealos/lifecycle/cmd/sealctl/cmd/cri.go`），并非由 image-cri-shim 服务直接回传驱动。`runtime/.github/scripts/kubernetes.sh` 分别拷贝 `cri/image-cri-shim` 和 `opt/sealctl`，二者都必须来自修复后的同一源码基线；仅替换 shim 服务不能解决此问题。
- 同步到独立 `image-cri-shim/` 后比较源码差异；按其 go.mod 的本地 replace/工作区依赖配置运行测试，不能假设独立仓库无需 Sealos 依赖即可测试。集成验收额外检查节点上 `sealctl` 的实际输出和退出码。

### 3.4 回滚与升级边界

- **第一阶段仅支持新建集群使用新的 2.x 镜像**；存量集群从 1.x 原地升到 2.x、跨大版本镜像替换、失败后降回 1.x 不在首批支持承诺内，需单独设计数据备份/恢复、pause 镜像、容器状态、配置迁移与清理路径并验证。不得建议直接覆盖 `/var/lib/containerd` 或在生产上盲目降级。
- 安装失败回滚仅针对测试环境/新集群，保留原镜像和旧模板，不改变旧集群既有状态；要验证卸载脚本与清理流程在新包布局下正常工作。

### 3.5 二次评审补充：阻塞项与明确约束

1. **有效配置解析**：不要只解析主 TOML 然后忽略 `imports` 或默认值。首批由 Sealos 生成完整、无 imports 的 v2/v4 配置；用户自定义 imports 若无法正确合并则明确报错。明确配置版本/插件选择规则；v3/v4 只读新 runtime 插件，旧字段冲突不能静默覆盖。合法缺省与损坏配置分开处理：SystemdCgroup 明确 false 可返回 cgroupfs；runtime/驱动无法确定时必须失败。优先实施版本化配置解析，不将升级 CRI API 并接入 RuntimeConfig 作为未经评估的隐式回退。
2. **crun 分支**：安装脚本探测的是 `crun_`，而当前缓存脚本下载名是 `crun`。不能假设打包 crun 就会切换默认运行时；首批固定 runc，crun 只有在显式选择、包名/路径检查和对应测试完成后才列入支持范围。
3. **认证与拉取模式**：必须先关闭 pause 冷启动认证问题，再决定 transfer 或显式 local pull。不要依靠废弃字段触发隐式回退；记录最终模式、并发参数实际生效位置和日志，首批只承诺实测模式。
4. **供应链锁定范围**：除 containerd 外，`kubernetes.sh` 还依赖 tools、kubernetes、sealos、CRI 工具、lvscare 等镜像。若宣称可重复构建，就必须记录并锁定这些输入及构建器；tag 可变，正式消费使用 digest。修改 `manifest.sh` 防止 2.x 测试制品覆盖原 Kubernetes tag。
5. **支持矩阵尚未批准**：v2.4.1 为正式发布候选不等于选定 LTS 或已具备目标主机兼容性；需核对 containerd 维护策略、目标 OS 内核/glibc/cgroup 模式和 Kubernetes/Sealos kubeadm API 支持交集。批准具体版本前不得进入发布阶段。
6. **CI 触发**：`sealos/.github/workflows/e2e_test_image_cri_shim.yml` 当前路径过滤省略了 `labring/`，应修正并覆盖 sealctl、runtime 模板变更；只有实际触发且测试通过才算门禁有效。

## 4. 验收与风险

最低验收矩阵：amd64 与 arm64；1.x 旧模板与 2.4.1 v4 模板；目标 Kubernetes 版本（先明确版本区间，再按下限/主流/上限选择，**不要仅用 K8s 版本号推断 containerd 兼容**）；runc 默认、crun 可选；单机/多节点；在线/离线私有仓库及认证。每格至少检查 `containerd --version`、`containerd config dump`、CRI v1 `crictl info`/拉镜像、Pod 创建和重启、`kubectl get nodes` Ready、kubelet/systemd cgroup driver 一致、扩容和卸载；升级实验单独记录，不计入首批支持。

门禁：单元测试覆盖 v2/v3/v4 TOML 和错误配置；CI 构建双架构产物并记录固定 digest；至少完成选定 Kubernetes 版本的实际端到端测试；发布说明写明版本矩阵和“不支持存量原地升级”的限制。若新插件未加载、错误检测成 cgroupfs、离线拉取失败或镜像中的 containerd 版本与预期不符则阻止发布。

风险：上游缓存共用标签漂移；containerd 2.x 的 CRI 插件拆分及 registry transfer/local 差异；凭据泄露；不同 Kubernetes/CRI 与 cgroup 自动检测行为；旧节点升级时数据布局/回滚不可逆。所有风险以测试结果和固定制品版本关闭，不能靠配置模板推断通过。

## 5. 依据

- [containerd v2.4.1 正式 release](https://github.com/containerd/containerd/releases/tag/v2.4.1)
- [containerd CRI 配置文档（固定 v2.4.1）](https://github.com/containerd/containerd/blob/v2.4.1/docs/cri/config.md)
- [cache 现有 CRI 包构建脚本](https://github.com/labring-actions/cache/blob/main/.github/scripts/cri)
- [runtime 集群镜像构建脚本](https://github.com/labring-actions/runtime/blob/main/.github/scripts/kubernetes.sh)（本地副本：`runtime/.github/scripts/kubernetes.sh`，相对于工作区根目录）

### 临时测试机验证记录（非集群验收）

2026-09-28 在 `117.50.193.80`（Ubuntu 22.04 x86_64，原机未安装系统级 containerd/Kubernetes）用固定 SHA256 校验官方 v2.4.1 amd64 压缩包；在 `/tmp/sealos-containerd-v2/` 完成 `containerd --version`、自建 v4 模板 `config dump`、隔离 socket 的临时 daemon 启动与 `ctr plugins ls`（`io.containerd.cri.v1` images/runtime 均 `ok`）；打包脚本在模拟基础包中保留 `runc` 并覆盖 containerd 后验证版本。没有配置系统服务、没有创建 Kubernetes 集群，**未验证真实仓库认证的 pause 冷启动、shim+sealctl 的实际交付、arm64 或 Pod 运行**。旧模板 v2 和新 v4 cgroup 解析本地单测已通过。另用官方签名校验文件对应的 SHA256 核对 crictl v1.34.0，在隔离 daemon 和 Python 模拟 htpasswd 仓库下用 CRI `crictl pull 127.0.0.1:55055/pause:3.10.1` 做了认证正反例：未配置 header 时失败 `no basic auth credentials`，配置只在本机生成的 host header 后返回成功。模拟 registry 的 Basic 账户仅供测试，实际仓库待验证。

### Kubernetes 1.34+ 版本矩阵与 pause 原始镜像（2026-09-28 查询）

`dl.k8s.io/release/stable-1.xx.txt` 与对应版本 kubeadm `config images list` 实测：

| Kubernetes | 当前补丁版 | 官方 pause | 当前缓存制品状态 |
| --- | --- | --- | --- |
| 1.34 | v1.34.12 | registry.k8s.io/pause:3.10.1 | 有 `kubernetes-v1.34.0-amd64`，无 `kubernetes-v1.34.12-amd64` |
| 1.35 | v1.35.9 | registry.k8s.io/pause:3.10.1 | 有 `kubernetes-v1.35.0-amd64`，无 `kubernetes-v1.35.9-amd64` |
| 1.36 | v1.36.5 | registry.k8s.io/pause:3.10.2 | 有 `kubernetes-v1.36.0-amd64`，无 `kubernetes-v1.36.5-amd64` |
| 1.37 | v1.37.1 | registry.k8s.io/pause:3.10.2 | `kubernetes-v1.37.0-amd64` 和 `cri-v1.37-amd64` 均不存在 |

这些版本的 kubeadm 当前输出 `kubeadm.k8s.io/v1beta4`，但不能据此推断 Sealos 全流程兼容。首批优先验证 1.34.x；1.35+ 各 minor 增加正式构建和 CI 验收后再声明支持；未来 minor 不自动视为已支持。`runtime/.github/scripts/kubernetes.sh` 现已为 2.4.1 实验路径选择已存在的 `kubernetes-v1.xx.0` base，再由 `prepare-kubernetes-binaries.sh` 下载并校验**精确补丁版本**的 kubeadm/kubelet/kubectl，覆盖 `bin/` 与 `images/shim/DefaultImageList`；2026-09-28 在测试机确认 v1.34.12 三个二进制的官方 SHA256 校验与 pause 清单成功。基础镜像仍可能包含旧版镜像层，需验证 `sealos build` 的最终镜像和内部 registry 确实使用新补丁版本，不能仅看文件覆盖。1.37 缺 .0 base 和 cri-v1.37，构建明确拒绝并要求补缓存；不要宣称已支持。

已在临时测试机 `/tmp/sealos-containerd-v2/` 的**隔离** containerd 内容库从官方仓库直接拉取 `pause:3.10.1` 和 `pause:3.10.2` 的 amd64 镜像并导出 OCI tar（文件名 `pause-3.10.{1,2}-amd64.oci.tar`）；这不等于 Sealos 集群镜像已经打包、`sealos.hub:5000` 已可用或 Pod 已验证。公开多架构 digest：3.10.1 = `sha256:278fb9dbcca9518083ad1e11276933a2e96f23de604a3a08cc3c80002767d24c`；3.10.2 = `sha256:f548e0e8e3dc1896ca956272154dde3314e8cc4fde0a57577ee9fa1c63f5baf4`。本地 OCI tar（单架构导出）具有单独的文件 SHA256，不要当成上游多架构 digest。

### amd64 集群验证记录（2026-09-28，测试机 `117.50.193.80`）

- 在测试机上安装了构建依赖，从本分支源码编译 `sealos`、`sealctl`、`image-cri-shim`，本地构建 `localhost/sealos-test/kubernetes:build-1790586198`（**未推送仓库**）。镜像内容检查：kubeadm/kubelet/kubectl 均 v1.34.12；内置 containerd v2.4.1；`etc/config.toml.tmpl` 为配置格式 v4；镜像清单中的 pause 为 3.10.1；两个 Sealos 工具包含在镜像内。该临时源码编译的 Sealos 版本元数据是 `latest`，正式发布需版本信息/commit 可追溯。
- 执行 `sealos run <本地镜像> --single` 成功初始化；`containerd`、kubelet、registry、image-cri-shim 服务均 active；CRI v1；`sealctl cri cgroup-driver --short` 与 kubelet 配置均为 systemd；本地 registry pause:3.10.1 已缓存、hosts.toml 权限 `0600` 且含节点生成的认证 header（仅校验存在，不公开内容）。
- 安装 `labring/flannel:0.24.0` 后发现其**默认**网络 `172.31.0.0/17` 与 Sealos 的 `podSubnet: 100.64.0.0/10` 冲突；仅在测试机修改 flannel ConfigMap `net-conf.json` 为 `100.64.0.0/10` 并重启 DaemonSet。之后节点 `Ready`、CoreDNS 两副本 Running；创建临时 busybox Pod Running，解析 `kubernetes.default.svc.cluster.local` 成功，随后删除临时命名空间。**若不配置 CNI 匹配的 PodCIDR，默认 flannel 部署仍失败。**
- 留有单节点集群用于后续测试；未进行 HA、多节点、断网冷启动、重启回归、1.35+、arm64 或旧集群原地升级测试。此记录不是生产支持声明，切勿用于判断其他版本已通过。

实施步骤见 [containerd-v2-checklist.md](containerd-v2-checklist.md)。

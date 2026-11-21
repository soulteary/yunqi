# 验证发布产物

当前自动发布暂停，见[发布状态](release.md)。本页保留发布恢复后的核验步骤；只有对应 Release 提供校验和与签名文件后，才适用以下命令。签名身份和 Windows 签名要求应以该版本的发布说明为准。

Sigstore 验证用于确认校验和清单由预期的工作流身份签名，再通过 SHA256 检查二进制是否与清单一致。以下示例假设发布工作流恢复为 `soulteary/yunqi` 的 `.github/workflows/release.yml`；实际使用时需核对对应 Release 的身份。

## 工作原理

1. CI 构建时，GitHub Actions 通过 OIDC 协议向 Sigstore 的 Fulcio CA 申请**短期签名证书**，证书中绑定了 GitHub Actions 身份信息。
2. `cosign` 使用该证书对 `checksums.txt` 文件签名，生成 Sigstore bundle（`.sigstore.json`）。
3. 签名记录写入 Rekor 公开透明度日志，任何人都可审计。
4. 用户验证时，cosign 从 bundle 中恢复证书信息，校验签名是否来自预期的 GitHub Actions 身份。

keyless 签名不要求维护者长期保存签名私钥，但仍需维护工作流权限和发布身份。

## 安装 cosign

macOS：

```bash
brew install cosign
```

Linux（二进制）：

```bash
# 参见 https://docs.sigstore.dev/cosign/installation/
COSIGN_VERSION="v3.8.0"
curl -LO "https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-amd64"
chmod +x cosign-linux-amd64
sudo mv cosign-linux-amd64 /usr/local/bin/cosign
```

Windows：

```powershell
choco install cosign
```

## 验证步骤

### 1. 下载 checksums 和签名文件

从 [GitHub Releases](https://github.com/soulteary/yunqi/releases) 下载：

- `checksums.txt` — 全平台 SHA256 清单
- `checksums.txt.sigstore.json` — 对应的 Sigstore 签名 bundle

### 2. 验证 checksums 清单签名

```bash
VERSION=vX.Y.Z  # 替换为要验证的版本

cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/soulteary/yunqi/.github/workflows/release.yml@refs/tags/${VERSION}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

验证成功时 cosign 输出 `Verified checksums.txt` 并返回 exit code 0。

优先按实际标签核验。仅检查发布身份时，也可使用以下仓库限定的正则；它不会核对你选择的具体版本：

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp "^https://github\.com/soulteary/yunqi/\.github/workflows/release\.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

### 3. 验证具体文件的 SHA256

签名验证通过后，用 checksums 清单校验下载的具体文件：

Linux：

```bash
sha256sum -c checksums.txt --ignore-missing
```

macOS（部分版本 `shasum` 不支持 `--ignore-missing`，可手动比对）：

```bash
shasum -a 256 yunqi-darwin-arm64
# 将输出与 checksums.txt 中对应行比较
```

### 4. 验证 Windows Authenticode 签名

若该 Release 声明提供 Authenticode 签名，可用 PowerShell 检查 Windows 二进制：

```powershell
$signature = Get-AuthenticodeSignature ".\yunqi-windows-amd64.exe"
$signature.Status
$signature.SignerCertificate.Subject
```

`Status` 应为 `Valid`。如果显示 `NotSigned`、`UnknownError` 或证书信息不符合预期，不要运行该安装包，重新从官方 Release 下载并重新验证 SHA256。

## 验证失败的常见原因

| 现象 | 原因 |
|------|------|
| `error: verifying bundle: mismatched identities` | 签名不是由本项目 CI 产生，或使用了错误的版本号 |
| `error: matching certificate identity failed` | `--certificate-identity` 与实际签名身份不匹配，检查版本号是否正确 |
| `error: tlog entry is not trusted` | Rekor 透明度日志验证失败，可能是 bundle 文件损坏 |
| `error: no matching signatures` | `checksums.txt` 文件内容与签名时不一致，可能下载过程中损坏 |
| 签名验证通过但 SHA256 不匹配 | 下载的二进制文件本身损坏，重新下载即可 |

## 产物说明

| 文件 | 内容 |
|------|------|
| `checksums.txt` | 全平台所有产物的 SHA256 哈希列表（`sha256sum` 格式） |
| `checksums.txt.sigstore.json` | Sigstore bundle：包含签名、OIDC 证书、Rekor 透明度日志条目 |
| `{artifact}.sha256` | 单个产物的 SHA256（仅含 hex hash） |

## 了解更多

- [Sigstore 官网](https://www.sigstore.dev/)
- [cosign 文档](https://docs.sigstore.dev/cosign/)
- [Rekor 透明度日志](https://docs.sigstore.dev/logging/overview/)
- [SLSA 供应链安全框架](https://slsa.dev/)

归档发布流程规划的产物为 `yunqi-{linux,darwin,windows}-{amd64,arm64}`（Windows 后缀 `.exe`）以及对应校验文件。旧桌面安装包及 MSIX 不再生成。

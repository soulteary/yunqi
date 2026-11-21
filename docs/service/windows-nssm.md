# Windows NSSM 自启动

NSSM 可以把 Yunqi 可执行文件注册为 Windows 服务，适合非 Docker 部署。

## 准备

1. 从 Release 下载 Windows 可执行文件，例如 `yunqi-windows-amd64.exe`。
2. 创建目录 `C:\yunqi`。
3. 将可执行文件放到 `C:\yunqi\yunqi-windows-amd64.exe`。
4. 下载并安装 NSSM。

## 安装服务

以管理员身份打开 PowerShell：

```powershell
nssm install yunqi C:\yunqi\yunqi-windows-amd64.exe
nssm set yunqi AppDirectory C:\yunqi
nssm set yunqi AppEnvironmentExtra PROXY_ACCESS_KEY=your-proxy-access-key PORT=3688 ENABLE_WEB_UI=true APP_UI_LANGUAGE=zh-CN
nssm set yunqi Start SERVICE_AUTO_START
nssm start yunqi
```

## 常用命令

```powershell
nssm status yunqi
nssm restart yunqi
nssm stop yunqi
nssm remove yunqi confirm
```

## 日志

如需输出日志，可以配置 NSSM stdout/stderr：

```powershell
mkdir C:\yunqi\logs
nssm set yunqi AppStdout C:\yunqi\logs\stdout.log
nssm set yunqi AppStderr C:\yunqi\logs\stderr.log
nssm set yunqi AppRotateFiles 1
nssm set yunqi AppRotateOnline 1
nssm set yunqi AppRotateBytes 10485760
```

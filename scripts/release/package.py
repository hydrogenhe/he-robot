#!/usr/bin/env python3
"""Build portable release ZIPs from an explicit file allowlist; never copy user data."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[2]
TARGETS = (("darwin", "arm64"), ("darwin", "amd64"), ("windows", "amd64"))


def module_licenses(destination):
    raw = subprocess.check_output(["go", "list", "-deps", "-json", "./cmd/gpt-robot"], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    modules = {}
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        module = package.get("Module")
        if module and not module.get("Main"):
            modules[module["Path"]] = module
    entries = []
    for module in modules.values():
        source = Path(module["Dir"])
        folder = destination / (module["Path"].replace("/", "__") + "@" + module["Version"])
        files = [p for p in source.iterdir() if p.is_file() and p.name.upper().startswith(("LICENSE", "NOTICE", "COPYING", "AUTHORS", "PATENTS"))]
        if not any(p.name.upper().startswith(("LICENSE", "COPYING")) for p in files):
            raise RuntimeError("Missing license: " + module["Path"])
        folder.mkdir(parents=True)
        for path in files:
            shutil.copy2(path, folder / path.name)
        entries.append(module["Path"] + " " + module["Version"])
    toolchain = Path(subprocess.check_output(["go", "env", "GOROOT"], cwd=ROOT, text=True).strip())
    go_licenses = destination / "Go-runtime"
    go_licenses.mkdir()
    for name in ("LICENSE", "PATENTS"):
        candidates = (toolchain / name, toolchain.parent / name)
        source = next((path for path in candidates if path.is_file()), None)
        if source:
            shutil.copy2(source, go_licenses / name)
        elif name == "LICENSE":
            raise RuntimeError("Missing Go runtime LICENSE")
    (destination / "MODULES.txt").write_text("\n".join(entries) + "\n")


def package(version, output):
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", version):
        raise ValueError("Use a version such as v0.2.0")
    output.mkdir(parents=True, exist_ok=True)
    sums = []
    with tempfile.TemporaryDirectory(prefix="gpt-robot-release-") as temporary:
        temporary = Path(temporary)
        licenses = temporary / "third-party-licenses"
        licenses.mkdir()
        module_licenses(licenses)
        for system, arch in TARGETS:
            name = f"gpt-robot-{version}-{system}-{arch}"
            folder = temporary / name
            (folder / "bin").mkdir(parents=True)
            (folder / "scripts").mkdir()
            executable = folder / "bin" / ("gpt-robot.exe" if system == "windows" else "gpt-robot")
            environment = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED="0")
            subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(executable), "./cmd/gpt-robot"], cwd=ROOT, env=environment, check=True)
            for file in ("README.md", "LICENSE", "THIRD_PARTY_NOTICES.md"):
                shutil.copy2(ROOT / file, folder / file)
            shutil.copytree(licenses, folder / "third-party-licenses")
            launcher = "start.cmd" if system == "windows" else "start.command"
            shutil.copy2(ROOT / "scripts" / launcher, folder / "scripts" / launcher)
            if system != "windows":
                # A binary download must never attempt to install Go or compile sources.
                (folder / "scripts" / launcher).write_text('''#!/bin/zsh
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")/.." || exit 1
bin/gpt-robot serve --root "$PWD" --open
read -r '?服务已停止，按回车关闭'
''')
                (folder / "scripts" / launcher).chmod(0o755)
            (folder / "START-HERE.txt").write_text(f'''GPT Robot {version} / {system}-{arch}

先完整解压 ZIP，再双击 scripts/{launcher}。
不需要 Go、Node.js 或另外安装数据库服务。
启动后打开 http://127.0.0.1:3217/；保持终端窗口运行。
在网页创建你自己的角色，安装并登录自己的 Codex / DeepSeek Harness 后配置模型。
此包不安装模型、不包含模型账号，模型请求使用你自己的权限与额度。
个人资料首次启动后保存在本目录的 data/、config/、.private/，请勿分享。
升级时先退出旧程序，备份这三个目录；新包不会包含或覆盖它们。

macOS Apple Silicon 包在 ARM64 Mac 验证；Intel 和 Windows 包仅交叉编译，未实机验收。
Windows 暂无 DeepSeek Harness 隔离适配；Codex 需可执行原生 CLI，链路仍待实机验证。
Windows 的额外 Codex 历史索引用量采集暂不支持；正常生成的原生回执计量不依赖该采集器。
当前包未使用开发者分发证书签名或公证，系统可能要求核实来源。
请核对官方仓库的 SHA256SUMS.txt；不要关闭系统全局安全设置。

许可证：MIT。第三方许可证见 third-party-licenses/。
''', encoding="utf-8")
            archive = output / (name + ".zip")
            with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as handle:
                for file in sorted(folder.rglob("*")):
                    if file.is_file():
                        handle.write(file, file.relative_to(temporary))
            sums.append(hashlib.sha256(archive.read_bytes()).hexdigest() + "  " + archive.name)
            print(archive.name, flush=True)
    (output / "SHA256SUMS.txt").write_text("\n".join(sums) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version")
    parser.add_argument("--output", type=Path, default=ROOT / "dist")
    options = parser.parse_args()
    package(options.version, options.output.resolve())

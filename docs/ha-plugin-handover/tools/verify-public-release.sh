#!/usr/bin/env bash
# 校验公开发布的 HA 插件制品：目录签名 + Agent/镜像摘要 + 篡改必须被拒绝。
#
# 只读：只下载到临时目录，不安装、不改任何容器。
#   用法： verify-public-release.sh [amd64|arm64|both] [--skip-assets]
#     --skip-assets  只验目录签名，不下载 15MB 左右的 Agent 和镜像包
#   依赖： curl openssl python3，以及 sha256sum 或 shasum
#
# 信任根写死在本脚本里（和安装器 config/native/update.py 的 PLUGIN_TRUST_PEM 相同），
# 并核对公钥指纹。这样即使下载来的目录被换掉，也不会被当成可信。
set -euo pipefail

SITE="${TP_INSTALL_SITE:-https://install.thingspanel.io}"
RELEASE_BASE="${TP_PLUGIN_RELEASE_BASE:-https://github.com/ThingsPanel/thingspanel-installer/releases/download}"
EXPECT_FINGERPRINT="6f6e902e807b86949e9dc3090de82eb932c27c6bd7f95987c8f44de00cca4fe8"

ARCHS="both"
SKIP_ASSETS=0
for a in "$@"; do
  case "$a" in
    amd64|arm64|both) ARCHS="$a" ;;
    --skip-assets) SKIP_ASSETS=1 ;;
    *) echo "未知参数：$a" >&2; exit 2 ;;
  esac
done
[ "$ARCHS" = both ] && ARCHS="amd64 arm64"

for c in curl openssl python3; do
  command -v "$c" >/dev/null || { echo "缺少命令：$c" >&2; exit 2; }
done
if command -v sha256sum >/dev/null; then SHA() { sha256sum "$1" | cut -d' ' -f1; }
else SHA() { shasum -a 256 "$1" | cut -d' ' -f1; }; fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat > "$WORK/trust.pem" <<'PEM'
-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEx5sN5wE3uKMeQNg/GfwdgdECr3Dl
HwERLCSb0Ng2n3l4djp5nlaxGQdYZFcwGZJ0UhdPyohfgnwpGnu6eZKicw==
-----END PUBLIC KEY-----
PEM

GOT_FINGERPRINT="$(openssl pkey -pubin -in "$WORK/trust.pem" -outform DER | openssl dgst -sha256 -r | cut -d' ' -f1)"
if [ "$GOT_FINGERPRINT" != "$EXPECT_FINGERPRINT" ]; then
  echo "FAIL 内置公钥指纹不对：$GOT_FINGERPRINT" >&2; exit 1
fi
echo "信任根指纹 $GOT_FINGERPRINT"

fail=0
verify_sig() { # catalog sig
  openssl dgst -sha256 -verify "$WORK/trust.pem" -signature "$2" "$1" >/dev/null 2>&1
}

for arch in $ARCHS; do
  echo
  echo "== linux/$arch"
  cat="$WORK/catalog-$arch.json"; sig="$WORK/catalog-$arch.json.sig"
  curl -fsSL --max-time 60 -o "$cat" "$SITE/plugins/catalog-linux-$arch.json"
  curl -fsSL --max-time 60 -o "$sig" "$SITE/plugins/catalog-linux-$arch.json.sig"

  if verify_sig "$cat" "$sig"; then echo "PASS 目录签名有效"; else echo "FAIL 目录签名无效"; fail=1; continue; fi

  # 篡改必须被拒绝：改一个字节，签名必须失败。
  python3 - "$cat" "$WORK/tampered-$arch.json" <<'PY'
import sys
b = bytearray(open(sys.argv[1], 'rb').read())
i = b.index(b'"version"')
b[i + 1] ^= 0x01
open(sys.argv[2], 'wb').write(b)
PY
  if verify_sig "$WORK/tampered-$arch.json" "$sig"; then echo "FAIL 篡改后的目录居然通过了验签"; fail=1
  else echo "PASS 篡改后的目录被拒绝"; fi

  eval "$(python3 - "$cat" <<'PY'
import json, sys, shlex, time
c = json.load(open(sys.argv[1]))
p = c["plugins"][0]
exp = c.get("expires_at", 0)
for k, v in {
  "TARGET": c.get("target_platform", ""), "TAG": c["release_tag"], "VER": c["version"],
  "AGENT_SHA": c["agent_sha256"], "IMG_SHA": p["image_archive_sha256"], "IMG_ID": p["image_id"],
  "PLUGIN_ID": p["id"], "SRC": p.get("source_commit", ""), "SDK": p.get("sdk_commit", ""),
  "EXPIRED": "yes" if exp and exp < time.time() else "no",
}.items():
    print("%s=%s" % (k, shlex.quote(str(v))))
PY
)"
  echo "插件 $PLUGIN_ID  版本 $VER  平台 $TARGET  Release $TAG"
  echo "HA 源码 $SRC  SDK $SDK"
  echo "镜像 ID $IMG_ID"
  [ "$TARGET" = "linux/$arch" ] && echo "PASS 目录平台与架构一致" || { echo "FAIL 目录平台 $TARGET 与 $arch 不一致"; fail=1; }
  [ "$EXPIRED" = no ] && echo "PASS 目录未过期" || { echo "FAIL 目录已过期"; fail=1; }

  if [ "$SKIP_ASSETS" = 1 ]; then echo "SKIP 未下载 Agent 和镜像（--skip-assets）"; continue; fi
  for pair in "tp-agent-linux-$arch:$AGENT_SHA" "ha-image-linux-$arch.tar:$IMG_SHA"; do
    name="${pair%%:*}"; want="${pair##*:}"
    curl -fsSL --max-time 600 -o "$WORK/$name" "$RELEASE_BASE/$TAG/$name"
    got="$(SHA "$WORK/$name")"
    if [ "$got" = "$want" ]; then echo "PASS $name 摘要与目录一致"; else echo "FAIL $name 摘要不一致 目录=$want 实际=$got"; fail=1; fi
    rm -f "$WORK/$name"
  done
done

echo
if [ "$fail" = 0 ]; then echo "全部通过"; else echo "有失败项"; exit 1; fi

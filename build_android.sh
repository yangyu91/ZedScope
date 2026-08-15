#!/usr/bin/env bash
# =============================================================================
# Local build of the ZedScope APK (Go core -> AAR -> signed APK).
#
# Prerequisites (on a machine WITH internet access):
#   - Go 1.22+
#   - JDK 17 (set JAVA_HOME)
#   - Android SDK (cmdline-tools) + NDK r25 ($ANDROID_HOME / $ANDROID_NDK_HOME)
#   - Gradle 8.9 on PATH  (run `gradle wrapper` once if you prefer ./gradlew)
#   - the gomobile toolchain (installed below on first run)
#
# The script:
#   1. installs gomobile / gobind (cached after first run)
#   2. compiles the Go capture engine into android/app/libs/yami.aar
#   3. makes sure a debug keystore exists for signing
#   4. builds a SIGNED APK (release by default; set BUILD_TYPE=debug for debug)
#
# Signing:   release is signed with $KEYSTORE_FILE if provided, otherwise the
#            Android debug keystore (~/.android/debug.keystore). Either way the
#            resulting APK is installable — no manual keystore required.
#
# Optional restricted-network mirror:  YAMI_MAVEN_MIRROR=1  (Tencent maven)
# =============================================================================
set -euo pipefail

# ---- environment ----
export ANDROID_HOME="${ANDROID_HOME:-$HOME/Android/Sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/25.2.9519653}"
: "${JAVA_HOME:?Please set JAVA_HOME to a JDK 17 installation}"

# release (signed, installable) by default; BUILD_TYPE=debug for a debug build.
BUILD_TYPE="${BUILD_TYPE:-release}"
GRADLE_TASK="assemble$(printf '%s' "$BUILD_TYPE" | sed 's/./\U&/')"   # assembleRelease / assembleDebug

# ---- ensure gradle is available ----
if ! command -v gradle >/dev/null 2>&1; then
  if [ -x ./android/gradlew ]; then
    GRADLE_CMD=(./android/gradlew)
  else
    echo "ERROR: 'gradle' not found on PATH and ./android/gradlew is missing." >&2
    echo "       Install Gradle 8.9 (https://gradle.org) or run 'gradle wrapper'." >&2
    exit 1
  fi
else
  GRADLE_CMD=(gradle)
fi

echo "==> [1/4] installing gomobile (skipped if already present)"
go install golang.org/x/mobile/cmd/gomobile@latest 2>/dev/null || true
go install golang.org/x/mobile/cmd/gobind@latest    2>/dev/null || true
command -v gomobile >/dev/null 2>&1 && gomobile init || echo "(gomobile init skipped)"

echo "==> [2/4] building yami.aar (Go core -> Android)"
mkdir -p android/app/libs
cd go
gomobile bind -target=android -androidapi 24 -o ../android/app/libs/yami.aar ./yami
cd ..

echo "==> [3/4] ensuring an Android signing keystore exists"
DBG_KEYSTORE="$HOME/.android/debug.keystore"
if [ ! -f "$DBG_KEYSTORE" ]; then
  echo "    generating $DBG_KEYSTORE (debug keystore, password 'android')"
  mkdir -p "$HOME/.android"
  keytool -genkeypair -v \
    -keystore "$DBG_KEYSTORE" -storepass android -keypass android \
    -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 \
    -dname "CN=ZedScope Debug,O=ZedScope,C=US"
fi

if [ -n "${KEYSTORE_FILE:-}" ]; then
  echo "    using provided release keystore: $KEYSTORE_FILE"
else
  echo "    using debug keystore fallback for signing"
fi

echo "==> [4/4] building $BUILD_TYPE APK (task: $GRADLE_TASK)"
cd android
"${GRADLE_CMD[@]}" "$GRADLE_TASK"
cd ..

APK="android/app/build/outputs/apk/$BUILD_TYPE/app-$BUILD_TYPE.apk"
echo
echo "=========================================================="
echo " Done. APK: $APK"
echo " Install:  adb install -r \"$APK\""
echo "=========================================================="

# Patchbay for Android

A native Android wrapper for Patchbay using **Gomobile (AAR)** + **Kotlin** + **Android Foreground Service** + **Embedded WebView**.

---

## Architecture Overview

1. **Go Core (`portforward` package)**:
   - Compiled to Android AAR (`patchbay.aar`) via `gomobile bind`.
   - Exposes `Portforward.startMobile(dataDir, host, port)` and `Portforward.stopMobile()`.
   - Embeds the exact same Vanilla JS web dashboard, Canvas chart, and SSE live telemetry.
2. **Android Foreground Service (`PatchbayService.kt`)**:
   - Manages Go runtime lifecycle.
   - Shows persistent status bar notification (`Patchbay is Active`) with quick **Stop** action.
   - Holds a `PARTIAL_WAKE_LOCK` so TCP/UDP port forwarding persists when the device screen is turned off.
3. **Android UI (`MainActivity.kt`)**:
   - Fullscreen Material 3 container hosting a modern `WebView`.
   - Connects directly to local Go engine at `http://127.0.0.1:8787/`.
   - Supports Swipe-to-Refresh and back button navigation.

---

## How to Build

### 1. Prerequisites
- **Go 1.22+**
- **Android SDK & NDK** (API 21+)
- **gomobile**:
  ```bash
  go install golang.org/x/mobile/cmd/gomobile@latest
  gomobile init
  ```
- **Android Studio** (or Gradle 8+)

### 2. Step 1: Build the Go AAR library
From the project root:
```bash
gomobile bind -target=android -androidapi 21 -o android/app/libs/patchbay.aar .
```
*(Or run `task build-android-aar` if using Taskfile)*

### 3. Step 2: Build the Android APK
Open the `android/` directory in **Android Studio** or run via CLI:
```bash
cd android
./gradlew assembleDebug
```
Output APK will be generated at:
`android/app/build/outputs/apk/debug/app-debug.apk`

---

## Permissions & Features
- `FOREGROUND_SERVICE` & `FOREGROUND_SERVICE_SPECIAL_USE`: Ensures TCP/UDP forwarder survives Android process killing / Doze mode.
- `RECEIVE_BOOT_COMPLETED`: Optional automatic background launch on device restart.
- `POST_NOTIFICATIONS`: Android 13+ status bar service control.

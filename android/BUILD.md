# Building & installing the Android client

**Prerequisites:** JDK 17, Android SDK with `compileSdk = 35` (Platform 35 + Build Tools 35.x).

## Build

```sh
git clone https://github.com/rotemmiz/opcode42
cd opcode42/android
./gradlew assembleDebug          # outputs app/build/outputs/apk/debug/app-debug.apk
```

Release:

```sh
./gradlew assembleRelease        # needs signing config; minified + resource-shrunk
```

## Install on a device or emulator

```sh
adb install app/build/outputs/apk/debug/app-debug.apk
```

The debug `applicationId` is `dev.opcode42.app.debug` (release is `dev.opcode42.app`), so
debug and release can coexist on the same device.

## Build variants

No product flavors. Two build types (`app/build.gradle.kts`):

| | Debug | Release |
|---|---|---|
| `applicationId` | `dev.opcode42.app.debug` | `dev.opcode42.app` |
| `versionName` suffix | `-debug` | — |
| Minify / shrink resources | off | on (R8 + `proguard-rules.pro`) |

`versionName = "0.1.0"`, `versionCode = 1`. Java/Kotlin target 17.

## Running tests

```sh
./gradlew test
./gradlew connectedAndroidTest     # needs a device/emulator
```

## Push notifications (opt-in)

The `com.google.gms:google-services` Gradle plugin is **deliberately not applied** — the
project does not commit a `google-services.json`. Firebase is initialized at runtime from
optional string resources (`PushConfig.kt`):

```
firebase_project_id
firebase_application_id
firebase_api_key
firebase_messaging_sender_id
```

When any are absent the app builds and runs normally with push disabled (the CI path). To
enable push in a private build, either drop a `google-services.json` into `:app` and apply
the gms plugin, or add a private `firebase_config.xml` defining the four string resources
above. The `Opcode42MessagingService` is `enabled="false"` in the manifest and toggled on at
runtime only when `PushConfig.isConfigured` is true.